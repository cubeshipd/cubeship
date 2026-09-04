package setup

import (
	"errors"
	"net/http"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Response is what claiming an instance hands back: enough for the
// dashboard to render the state it just created, without a second round
// trip.
type Response struct {
	Username string `json:"username"`
	Org      string `json:"org"`
	Project  string `json:"project"`
}

// Handler serves the two setup endpoints. Both are unauthenticated, and
// have to be: there is nobody to authenticate as yet.
type Handler struct {
	svc *Service

	// startSession signs the new account in, so whoever set the instance
	// up lands in the dashboard rather than at a login form they would
	// fill in with what they just typed.
	startSession func(w http.ResponseWriter, r *http.Request, u *user.User) error
}

func NewHandler(svc *Service, startSession func(http.ResponseWriter, *http.Request, *user.User) error) *Handler {
	return &Handler{svc: svc, startSession: startSession}
}

func (h *Handler) Routes(r *httpx.Router) {
	r.HandleInternalFunc("GET /setup", h.status)
	r.HandleInternalFunc("POST /setup", h.claim)
}

// status says whether the instance is still unclaimed. A browser asks
// this first: it decides between showing a sign-in form and showing the
// one-time setup form.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	needed, err := h.svc.Needed(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Status{Needed: needed})
}

func (h *Handler) claim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	result, err := h.svc.Claim(r.Context(), req.Username, req.Password)
	switch {
	case err == nil:
	case errors.Is(err, ErrAlreadySetUp):
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case errors.Is(err, ErrUsernameRequired), errors.Is(err, ErrPasswordRequired),
		errors.Is(err, user.ErrPasswordTooShort), errors.Is(err, slug.ErrInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.startSession(w, r, result.User); err != nil {
		// The account exists and is usable; only the convenience of
		// arriving signed in was lost. Say so rather than implying the
		// setup failed, which would invite a retry that now conflicts.
		http.Error(w, "the instance was set up, but signing you in failed — sign in with the account you just created", http.StatusInternalServerError)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, Response{
		Username: result.User.Username,
		Org:      result.Org.Slug,
		Project:  result.Project.Slug,
	})
}
