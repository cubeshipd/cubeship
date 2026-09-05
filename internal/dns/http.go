package dns

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is a credential as it leaves the daemon.
//
// The secret is not on it, and cannot be: a provider takes the secret
// itself, so it is stored as given, and an endpoint that returned it
// would turn every read of the list into a way out for it. Route 53's
// key id is here, because it is not a secret — it is the half you can
// read off the console.
type Response struct {
	ID       int64    `json:"id"`
	Provider Provider `json:"provider"`
	Label    string   `json:"label"`
	// Username is Route 53's access key id, and absent for Cloudflare.
	// It is not a secret — it is the half you can read off the console.
	Username  string    `json:"username,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toResponse(c *Credential) Response {
	return Response{
		ID: c.ID, Provider: c.Provider, Label: c.Label, Username: c.Username,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func toResponses(creds []*Credential) []Response {
	out := make([]Response, 0, len(creds))
	for _, c := range creds {
		out = append(out, toResponse(c))
	}
	return out
}

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /dns", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /dns", auth(http.HandlerFunc(h.create)))
	r.Handle("PATCH /dns/{id}", auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE /dns/{id}", auth(http.HandlerFunc(h.delete)))
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
	case errors.Is(err, ErrLabelTaken):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrProviderRequired), errors.Is(err, ErrLabelRequired),
		errors.Is(err, ErrPasswordRequired), errors.Is(err, ErrUsernameRequired),
		errors.Is(err, ErrRecordTypeUnknown):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	creds, err := h.svc.List(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(creds))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider Provider `json:"provider"`
		Label    string   `json:"label"`
		Username string   `json:"username"`
		Password string   `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	created, err := h.svc.Create(ctx, user.FromContext(ctx), Credential{
		Provider: req.Provider, Label: req.Label,
		Username: req.Username, Password: req.Password,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	// Pointers, because omitting a field means "leave it alone" and
	// sending an empty one means "you gave me nothing" — two different
	// requests a plain string cannot tell apart.
	var req struct {
		Label    *string `json:"label"`
		Username *string `json:"username"`
		Password *string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	updated, err := h.svc.Update(ctx, user.FromContext(ctx),
		id, req.Label, req.Username, req.Password)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
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
