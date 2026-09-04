package extregistry

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/org"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is one credential as the API returns it.
//
// There is no password field, and that is not an oversight: a stored
// password exists to be sent to a registry, and an endpoint that hands
// it back turns every read of this list into a way to exfiltrate it.
// Replace it if you need to change it.
type Response struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toResponse(c *Credential) Response {
	return Response{
		ID: c.ID, Name: c.Name, Host: c.Host, Username: c.Username,
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
	r.Handle("GET /orgs/{orgSlug}/registries", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /orgs/{orgSlug}/registries", auth(http.HandlerFunc(h.create)))
	r.Handle("PUT /orgs/{orgSlug}/registries/{id}", auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE /orgs/{orgSlug}/registries/{id}", auth(http.HandlerFunc(h.delete)))
}

// WriteError maps this module's domain errors onto status codes. The
// 404/403 split is org.Service.Authorize's, unchanged.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, org.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, org.ErrNotFound), errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrHostTaken), errors.Is(err, ErrNameTaken):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNameRequired), errors.Is(err, ErrHostRequired),
		errors.Is(err, ErrUsernameRequired), errors.Is(err, ErrPasswordRequired):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	creds, err := h.svc.List(ctx, user.FromContext(ctx), r.PathValue("orgSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(creds))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Host     string `json:"host"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	created, err := h.svc.Create(ctx, user.FromContext(ctx), r.PathValue("orgSlug"),
		req.Name, req.Host, req.Username, req.Password)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	updated, err := h.svc.Update(ctx, user.FromContext(ctx), r.PathValue("orgSlug"),
		id, req.Username, req.Password)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if err := h.svc.Delete(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
