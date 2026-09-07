package machine

import (
	"errors"
	"net/http"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// One route. There is one machine and nothing on it to configure from
// here — this module reads what the kernel already knows, the way
// certificates reads Traefik's store.
func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /instance/metrics", auth(http.HandlerFunc(h.metrics)))
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	series, err := h.svc.Series(ctx, user.FromContext(ctx), r.URL.Query().Get("window"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, series)
}

// WriteError maps this module's refusals.
//
// **A measurement this daemon cannot take is not one of them.** It
// comes back inside a 200 with the reason beside the numbers that did
// read, because "no network figures on this instance, and here is what
// to do about it" is an answer — and an error status would throw away
// the three charts that work to report the one that does not.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, metrics.ErrUnknownWindow):
		http.Error(w, err.Error()+": try "+metrics.WindowNames(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
