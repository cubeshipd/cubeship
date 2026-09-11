package backup

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is one backup, as a table shows it.
type Response struct {
	ID int64 `json:"id"`
	// Database is the name it was taken from, and it survives that
	// database being deleted — which is when a backup matters most.
	Database string `json:"database"`
	// Exists says the database is still here, which is what decides
	// whether this can be restored at all.
	Exists  bool   `json:"database_exists"`
	Engine  string `json:"engine"`
	Version string `json:"version"`
	// Store is where it landed, empty for this machine's own disk.
	Store  string `json:"store,omitempty"`
	Bucket string `json:"bucket,omitempty"`
	Key    string `json:"key"`
	// OffMachine is the one fact worth reading at a glance: a dump on
	// the same disk as the database survives a dropped table and
	// nothing else.
	OffMachine bool   `json:"off_machine"`
	Size       int64  `json:"size_bytes"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Scheduled  bool   `json:"scheduled"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// ScheduleResponse is when a database is backed up without anybody
// asking. Absent entirely when it is not — the row existing is what
// scheduled means.
type ScheduleResponse struct {
	At       string `json:"at"`
	Timezone string `json:"timezone"`
	Keep     int    `json:"keep"`
	Store    string `json:"store,omitempty"`
	Bucket   string `json:"bucket,omitempty"`
	LastRun  string `json:"last_run_at,omitempty"`
}

// CoverageResponse is one database's backup situation.
//
// **It is the answer a list of backups cannot give**, because it is
// built from the databases: a database that has never been dumped is
// the row somebody most needs and it appears in no list of dumps.
type CoverageResponse struct {
	Database string `json:"database"`
	Engine   string `json:"engine"`
	Version  string `json:"version"`
	// CanBackUp is false for an engine this instance does not dump.
	// Reported rather than left out — a database missing from a
	// coverage report reads as one nobody checked.
	CanBackUp bool `json:"can_back_up"`

	// Schedule is absent when there is none, which is what "not
	// scheduled" is.
	Schedule *ScheduleResponse `json:"schedule,omitempty"`

	// Protected is the whole report in one field: there is something to
	// put back, and it is not on this machine's own disk. Both halves,
	// because either alone is a lie somebody acts on.
	Protected bool `json:"protected"`
	// Failing says the most recent attempt did not succeed. Not the
	// opposite of Protected: last week's dump may be sitting safely in
	// a bucket while every night since has failed.
	Failing bool `json:"failing"`

	// LastGood is the newest backup that can actually be restored, and
	// Last the newest attempt whatever it did. Both, because one says
	// what you could put back and the other says whether backups are
	// working.
	LastGood *Response `json:"last_good,omitempty"`
	Last     *Response `json:"last,omitempty"`
	Count    int       `json:"count"`
}

type Handler struct {
	svc *Service
	// stores answers what a store is called, so a listing can say where
	// a backup is without the dashboard joining it itself.
	names func(id int64) string
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// SetStoreNames wires what an object store's id is called. Wired by
// `server`, like every other seam between two modules.
func (h *Handler) SetStoreNames(names func(id int64) string) { h.names = names }

const databasePath = "/datastores/{name}/backups"

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /backups", auth(http.HandlerFunc(h.list)))
	// Before the {id} routes in the file and irrelevant to the mux,
	// which prefers a literal segment over a wildcard whatever the
	// order — but a reader should not have to know that.
	r.Handle("GET /backups/coverage", auth(http.HandlerFunc(h.coverage)))
	r.Handle("GET /backups/orphans", auth(http.HandlerFunc(h.orphans)))
	r.Handle("GET "+databasePath, auth(http.HandlerFunc(h.forDatabase)))
	r.Handle("POST "+databasePath, auth(http.HandlerFunc(h.take)))
	r.Handle("GET "+databasePath+"/schedule", auth(http.HandlerFunc(h.schedule)))
	r.Handle("PUT "+databasePath+"/schedule", auth(http.HandlerFunc(h.setSchedule)))
	r.Handle("DELETE "+databasePath+"/schedule", auth(http.HandlerFunc(h.unsetSchedule)))
	r.Handle("POST /backups/{id}/restore", auth(http.HandlerFunc(h.restore)))
	r.Handle("GET /backups/{id}/download", auth(http.HandlerFunc(h.download)))
	r.Handle("DELETE /backups/{id}", auth(http.HandlerFunc(h.delete)))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.svc.List(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toResponses(rows))
}

// coverage is one row per database rather than one per dump.
func (h *Handler) coverage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.svc.Coverage(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]CoverageResponse, 0, len(rows))
	for _, c := range rows {
		item := CoverageResponse{
			Database: c.Database, Engine: c.Engine, Version: c.Version,
			CanBackUp: c.CanBackUp,
			Protected: c.Protected(), Failing: c.Failing(),
			Count: c.Count,
		}
		if c.Schedule != nil {
			sched := h.toScheduleResponse(c.Schedule)
			item.Schedule = &sched
		}
		if c.LastGood != nil {
			good := h.toResponse(c.LastGood)
			item.LastGood = &good
		}
		if c.Last != nil {
			last := h.toResponse(c.Last)
			item.Last = &last
		}
		out = append(out, item)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// orphans is the backups whose database has been deleted — the rows
// nothing else on the instance can reach.
func (h *Handler) orphans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.svc.Orphans(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toResponses(rows))
}

func (h *Handler) forDatabase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.svc.ForDatabase(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toResponses(rows))
}

// take answers 202: the dump is detached, so what comes back is the row
// it will report into rather than the outcome.
func (h *Handler) take(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	row, err := h.svc.Take(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, h.toResponse(row))
}

func (h *Handler) schedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s, err := h.svc.Schedule(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toScheduleResponse(s))
}

func (h *Handler) setSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		At       string `json:"at"`
		Timezone string `json:"timezone"`
		// Keep is how many to hold on to. Zero keeps every one, which
		// is a decision rather than a gap — so a body that leaves it
		// out is asking for that, and the screen says what it costs.
		Keep int `json:"keep"`
		// Store is the object store by name, the way every surface in
		// this product addresses one. Empty is this machine's own disk.
		Store  string `json:"store"`
		Bucket string `json:"bucket"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ctx := r.Context()
	s, err := h.svc.SetSchedule(ctx, user.FromContext(ctx), r.PathValue("name"), req.Store, Schedule{
		At: req.At, Timezone: req.Timezone, Keep: req.Keep, Bucket: req.Bucket,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toScheduleResponse(s))
}

func (h *Handler) unsetSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.svc.UnsetSchedule(ctx, user.FromContext(ctx), r.PathValue("name")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	id, ok := idOf(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := h.svc.Restore(ctx, user.FromContext(ctx), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// download streams the dump through the daemon, the way a file in a
// bucket already is: a presigned URL would name an endpoint only this
// daemon can resolve, and a local backup has no URL at all.
func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	id, ok := idOf(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	body, row, err := h.svc.Download(ctx, user.FromContext(ctx), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	// Always an attachment. A dump is somebody's data and rendering one
	// inline would run whatever is in it on this daemon's own origin,
	// beside the session cookie — the same rule a bucket's files keep.
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", path.Base(row.Key)))
	if row.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(row.Size, 10))
	}
	if _, err := io.Copy(w, body); err != nil {
		// The status is long gone; there is nothing to tell the client
		// that it has half a file but a truncated download.
		return
	}
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := idOf(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := h.svc.Delete(ctx, user.FromContext(ctx), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func idOf(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return 0, false
	}
	return id, true
}

func (h *Handler) toResponse(b *Backup) Response {
	out := Response{
		ID: b.ID, Database: b.DatastoreName, Exists: b.DatastoreID != 0,
		Engine: b.Engine, Version: b.Version,
		Bucket: b.Bucket, Key: b.Key, OffMachine: b.OffMachine(),
		Size: b.Size, Status: b.Status, Error: b.Error,
		Scheduled: b.Scheduled,
		StartedAt: b.StartedAt.UTC().Format(time.RFC3339),
	}
	if b.StoreID != 0 && h.names != nil {
		out.Store = h.names(b.StoreID)
	}
	if b.FinishedAt != nil {
		out.FinishedAt = b.FinishedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (h *Handler) toResponses(rows []*Backup) []Response {
	out := make([]Response, 0, len(rows))
	for _, b := range rows {
		out = append(out, h.toResponse(b))
	}
	return out
}

func (h *Handler) toScheduleResponse(s *Schedule) ScheduleResponse {
	out := ScheduleResponse{
		At: s.At, Timezone: s.Timezone, Keep: s.Keep, Bucket: s.Bucket,
	}
	if s.StoreID != 0 && h.names != nil {
		out.Store = h.names(s.StoreID)
	}
	if s.LastRunAt != nil {
		out.LastRun = s.LastRunAt.UTC().Format(time.RFC3339)
	}
	return out
}

// WriteError maps this module's refusals onto status codes.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)

	case errors.Is(err, ErrStillRunning), errors.Is(err, ErrNotDone),
		errors.Is(err, ErrEngineMismatch), errors.Is(err, ErrNoDump):
		// Nothing about the request is malformed: the backup, or the
		// database, is in a state that does not allow it.
		http.Error(w, err.Error(), http.StatusConflict)

	case errors.Is(err, ErrBadTime), errors.Is(err, ErrUnknownTimezone),
		errors.Is(err, ErrBadKeep), errors.Is(err, ErrNoBucket),
		errors.Is(err, httpx.ErrNotJSON):
		http.Error(w, err.Error(), http.StatusBadRequest)

	default:
		// A refusal raised by the database or the store it reached for
		// is that module's to phrase, rather than this one guessing a
		// status for an error it did not raise.
		user.WriteError(w, err)
	}
}
