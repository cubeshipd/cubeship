package dns

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"cubeship/internal/credential"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	// No credential CRUD here any more. An account that manages DNS is
	// a credential, and it is created, renamed and deleted at
	// /credentials like every other account this instance holds. What
	// is left is what is actually about DNS.
	r.Handle("GET /dns/{id}/status", auth(http.HandlerFunc(h.status)))
	r.Handle("GET /dns/{id}/zones", auth(http.HandlerFunc(h.zones)))
	r.Handle("GET /dns/{id}/records", auth(http.HandlerFunc(h.records)))
	r.Handle("PUT /dns/{id}/records", auth(http.HandlerFunc(h.putRecord)))
	r.Handle("DELETE /dns/{id}/records", auth(http.HandlerFunc(h.deleteRecord)))
}

// WriteError maps this module's domain errors onto status codes.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, ErrNotFound),
		errors.Is(err, ErrRecordNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrRecordTypeUnknown):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		// Everything about the account itself — no such credential, one
		// that cannot do DNS — is the credential module's to phrase, so
		// its mapping answers rather than this one guessing at a status
		// for an error it did not raise.
		credential.WriteError(w, err)
	}
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	status, err := h.svc.Probe(ctx, user.FromContext(ctx), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (h *Handler) zones(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	zones, err := h.svc.Zones(ctx, user.FromContext(ctx), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, zones)
}

func (h *Handler) records(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	records, err := h.svc.Records(ctx, user.FromContext(ctx),
		id, r.URL.Query().Get("zone"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, records)
}

func (h *Handler) putRecord(w http.ResponseWriter, r *http.Request) {
	var req Record
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	if err := h.svc.PutRecord(ctx, user.FromContext(ctx),
		id, r.URL.Query().Get("zone"), req); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	query := r.URL.Query()
	if err := h.svc.DeleteRecord(ctx, user.FromContext(ctx),
		id, query.Get("zone"), query.Get("name"), query.Get("type")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathID reads the credential's id, answering 404 for anything that is
// not one: a path segment that is not a number names nothing, and that
// is the same answer as naming something that does not exist.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return 0, false
	}
	return id, true
}
