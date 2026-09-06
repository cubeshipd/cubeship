package firewall

import (
	"errors"
	"net/http"
	"strconv"

	"cubeship/internal/platform/hostexec"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /firewall", auth(http.HandlerFunc(h.status)))
	r.Handle("POST /firewall/enable", auth(http.HandlerFunc(h.enable)))
	r.Handle("POST /firewall/disable", auth(http.HandlerFunc(h.disable)))
	r.Handle("POST /firewall/rules", auth(http.HandlerFunc(h.addRule)))
	r.Handle("DELETE /firewall/rules/{index}", auth(http.HandlerFunc(h.deleteRule)))
	r.Handle("POST /firewall/docker", auth(http.HandlerFunc(h.adoptDocker)))
	r.Handle("DELETE /firewall/docker", auth(http.HandlerFunc(h.releaseDocker)))
}

// WriteError maps this module's domain errors onto status codes.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, ErrNoSuchRule):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrBadRule):
		http.Error(w, err.Error(), http.StatusBadRequest)
	// 409 for the three refusals that are about the state of the host
	// rather than about the request: it is a correct request that this
	// machine is not in a position to carry out, and the difference is
	// what tells somebody whether to fix the form or fix the instance.
	case errors.Is(err, ErrWouldLockYouOut), errors.Is(err, ErrRuleChanged),
		errors.Is(err, ErrDockerNotAdopted), errors.Is(err, ErrNotInstalled),
		errors.Is(err, hostexec.ErrUnavailable):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		// Whatever ufw itself said, which is usually a sentence worth
		// reading. 500 because the daemon asked for something it
		// believed was valid and the host disagreed.
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status, err := h.svc.Status(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

// write is what every mutating handler ends in: they all answer with the
// firewall as it now stands, so a client never has to follow one call
// with a read to find out what happened.
func write(w http.ResponseWriter, result func() (*Status, error)) {
	status, err := result()
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (h *Handler) enable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	write(w, func() (*Status, error) { return h.svc.Enable(ctx, user.FromContext(ctx)) })
}

func (h *Handler) disable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	write(w, func() (*Status, error) { return h.svc.Disable(ctx, user.FromContext(ctx)) })
}

func (h *Handler) addRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope    string `json:"scope"`
		Action   string `json:"action"`
		Protocol string `json:"protocol"`
		Port     string `json:"port"`
		From     string `json:"from"`
		Comment  string `json:"comment"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	write(w, func() (*Status, error) {
		return h.svc.AddRule(ctx, user.FromContext(ctx), Spec{
			Scope:    Scope(req.Scope),
			Action:   Action(req.Action),
			Protocol: Protocol(req.Protocol),
			Port:     req.Port,
			From:     req.From,
			Comment:  req.Comment,
		})
	})
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// What the caller believes is at that position. Optional, and the
	// dashboard always sends it: ufw deletes by position, and a screen
	// acting on a listing that has moved would delete somebody else's
	// rule with no sign but a port that stopped answering.
	ctx := r.Context()
	write(w, func() (*Status, error) {
		return h.svc.DeleteRule(ctx, user.FromContext(ctx), index, r.URL.Query().Get("expect"))
	})
}

func (h *Handler) adoptDocker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// AllowPorts are the published ports to keep open. 80 and 443
		// are added whatever this says.
		AllowPorts []int `json:"allow_ports"`
	}
	// An empty body is a valid request here — it means "only what
	// Cubeship itself needs" — so a decode failure is not fatal.
	_ = httpx.DecodeJSON(r, &req)

	ctx := r.Context()
	write(w, func() (*Status, error) { return h.svc.AdoptDocker(ctx, user.FromContext(ctx), req.AllowPorts) })
}

func (h *Handler) releaseDocker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	write(w, func() (*Status, error) { return h.svc.ReleaseDocker(ctx, user.FromContext(ctx)) })
}
