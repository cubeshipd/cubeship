package components

import (
	"cubeship/internal/node"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
	"errors"
	"net/http"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }
func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /nodes/{name}/components", auth(http.HandlerFunc(h.list)))
	r.Handle("GET /nodes/{name}/components/{component}/logs", auth(http.HandlerFunc(h.logs)))
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.List(r.Context(), user.FromContext(r.Context()), r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}
func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Logs(r.Context(), user.FromContext(r.Context()), r.PathValue("name"), r.PathValue("component"), r.URL.Query().Get("tail"))
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(out)
}
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, node.ErrNotFound), errors.Is(err, ErrUnknown), errors.Is(err, database.ErrNotFound), errors.Is(err, dockerx.ErrContainerNotFound):
		http.Error(w, "component or machine not found", http.StatusNotFound)
	case errors.Is(err, ErrTail):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, user.ErrUnauthenticated), errors.Is(err, user.ErrForbidden):
		user.WriteError(w, err)
	default:
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
}
