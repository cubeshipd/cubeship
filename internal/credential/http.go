package credential

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is one credential as the API reports it.
//
// No password, ever, and no way to ask for one. Unlike a database's
// login — which a person has to be able to paste into psql — nothing
// outside this daemon needs to see these: the daemon is what talks to
// the provider. A credential you cannot read back is a credential that
// cannot leak through a screen somebody left open.
type Response struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
	// Username is the first half where the secret has one — an access
	// key id, a registry login. Not a secret: the secret is the other
	// half.
	Username string `json:"username,omitempty"`
	// InUseBy is what is currently depending on it — what a delete
	// would refuse over, said before somebody tries.
	InUseBy   []string  `json:"in_use_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toResponse(c *Credential, uses []Use) Response {
	r := Response{
		ID: c.ID, Label: c.Label, Username: c.Username,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	for _, u := range uses {
		r.InUseBy = append(r.InUseBy, u.String())
	}
	return r
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

const credentialPath = "/credentials/{id}"

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("POST /credentials", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /credentials", auth(http.HandlerFunc(h.list)))
	r.Handle("PATCH "+credentialPath, auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE "+credentialPath, auth(http.HandlerFunc(h.delete)))
}

// WriteError maps this module's domain errors onto status codes.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrLabelTaken), errors.Is(err, ErrInUse):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrLabelRequired), errors.Is(err, ErrPasswordRequired):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		user.WriteError(w, err)
	}
}

func idFrom(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label    string `json:"label"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	created, err := h.svc.Create(r.Context(), user.FromContext(r.Context()), Credential{
		Label: req.Label, Username: req.Username, Password: req.Password,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created, nil))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	caller := user.FromContext(r.Context())
	all, err := h.svc.List(r.Context(), caller)
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]Response, 0, len(all))
	for _, c := range all {
		// What is using each, so the list can say why one cannot be
		// deleted before somebody tries. One query per credential, on a
		// page that holds a handful of them.
		uses, err := h.svc.Uses(r.Context(), c.ID)
		if err != nil {
			WriteError(w, err)
			return
		}
		out = append(out, toResponse(c, uses))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		http.Error(w, "invalid credential id", http.StatusBadRequest)
		return
	}
	var req struct {
		Label    *string `json:"label"`
		Username *string `json:"username"`
		Password *string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	updated, err := h.svc.Update(r.Context(), user.FromContext(r.Context()),
		id, req.Label, req.Username, req.Password)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, nil))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		http.Error(w, "invalid credential id", http.StatusBadRequest)
		return
	}
	if err := h.svc.Delete(r.Context(), user.FromContext(r.Context()), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
