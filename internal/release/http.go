package release

import (
	"errors"
	"net/http"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Handler is the HTTP surface: what this instance is running, and what
// changed in it.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /releases", auth(http.HandlerFunc(h.get)))
	r.Handle("POST /releases/seen", auth(http.HandlerFunc(h.seen)))
}

// Response is every release this build knows about, and which of them
// the caller has not been shown.
//
// One request rather than two, because the screen that asks has one
// question: is there something to say, and what is the history behind
// it. `unseen` is a list of the same shape rather than a list of
// version strings — a dialog would otherwise have to join the two
// itself for no gain.
type Response struct {
	// Version is what this instance is running, without a leading v.
	// Empty on a build with nothing stamped on it, which is a
	// developer's — and then there is nothing to show.
	Version string `json:"version,omitempty"`
	// Notes is every release up to that version, newest first. Never
	// one this instance is not on: a build carrying notes for a version
	// ahead of it would otherwise advertise something nobody can use.
	Notes []NoteResponse `json:"notes"`
	// Unseen is the ones this caller has not been shown. Empty is the
	// ordinary answer, and it is what a dialog reads to decide not to
	// appear.
	Unseen []NoteResponse `json:"unseen"`
}

// NoteResponse is one release.
type NoteResponse struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	// Summary is one sentence, for a list where the body is too much.
	Summary string `json:"summary"`
	// Body is Markdown.
	Body string `json:"body"`
	// Prerelease says this is not a stable version.
	Prerelease bool `json:"prerelease,omitempty"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	state, err := h.svc.For(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Response{
		Version: state.Version,
		Notes:   toNotes(state.Notes),
		Unseen:  toNotes(state.Unseen),
	})
}

func (h *Handler) seen(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.MarkSeen(r.Context(), user.FromContext(r.Context())); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toNotes(in []Note) []NoteResponse {
	out := make([]NoteResponse, 0, len(in))
	for _, n := range in {
		out = append(out, NoteResponse{
			Version: n.Version, Date: n.Date, Summary: n.Summary,
			Body: n.Body, Prerelease: n.Prerelease,
		})
	}
	return out
}

// WriteError maps this module's refusals. There is one kind: who is
// asking.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
