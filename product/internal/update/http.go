package update

import (
	"errors"
	"net/http"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Handler is the HTTP surface: what this instance is on, what it could
// move to, and moving it.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /updates", auth(http.HandlerFunc(h.get)))
	r.Handle("POST /updates", auth(http.HandlerFunc(h.start)))
}

// Response is what a screen shows.
type Response struct {
	// Version is what this instance is running. Absent on a build with
	// nothing stamped on it, which is a developer's.
	Version string `json:"version,omitempty"`
	// Available is a newer stable release, or absent.
	Available *Available `json:"available,omitempty"`
	// Checked says the lookup happened. **False with no `available` is
	// "could not ask"**, which is a different thing to show than "you
	// are up to date" — an instance behind a firewall is a normal
	// instance and should not be told it is current.
	Checked bool `json:"checked"`
	// Run is the update in progress, or the last one. While its status
	// is `running` this instance refuses every write, including from a
	// browser that has just reloaded.
	Run *Run `json:"run,omitempty"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	state, err := h.svc.Check(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Response{
		Version: state.Version, Available: state.Available,
		Checked: state.Checked, Run: state.Run,
	})
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// Version is the release to move to. Required rather than
		// defaulted to "the newest": a button that says what it will
		// install and a request that decides for itself are two
		// different promises, and the second one changes under
		// somebody between the screen rendering and the click.
		Version string `json:"version"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	run, err := h.svc.Start(r.Context(), user.FromContext(r.Context()), req.Version)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, Response{Version: h.svc.Version(), Run: run})
}

// WriteError maps this module's refusals.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnknownVersion):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrInProgress), errors.Is(err, ErrNotNewer), errors.Is(err, ErrNotAContainer):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Guard refuses to change anything while an update is running.
//
// **The lock has to be the server's**, not the screen's. A browser that
// reloads forgets everything it knew, and the moment worth protecting
// is exactly the one where the daemon has restarted under it: somebody
// pressing deploy while their containers are being replaced is the
// failure this exists to stop, and a disabled button in a page that is
// no longer loaded stops nothing.
//
// Reads go through. What somebody does during an update is watch it,
// and refusing the request that shows the progress would be refusing
// the only useful thing left to do.
//
// `POST /updates` is refused too, by the service, with a message about
// one already running — so there is no hole here for a second update.
func Guard(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if run := svc.Current(); run.Running() {
				step := run.Step
				if step == "" {
					step = "starting"
				}
				http.Error(w,
					"this instance is updating to "+run.Version+" ("+step+"). Nothing can be changed until it finishes.",
					http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
