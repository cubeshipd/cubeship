package certificates

import (
	"errors"
	"net/http"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /certificates", auth(http.HandlerFunc(h.report)))
}

func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	report, err := h.svc.Report(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, report)
}
