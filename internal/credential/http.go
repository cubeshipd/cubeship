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
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	// Name is the provider as a person calls it, so a client does not
	// keep its own table of labels that drifts out of step.
	ProviderName string `json:"provider_name"`
	Label        string `json:"label"`
	// Username is the first half where the provider has one — an access
	// key id. Not a secret: the secret is the other half.
	Username string `json:"username,omitempty"`
	// Capabilities are what this credential may be used for. Derived
	// from the provider, so a client can show it without knowing the
	// rules.
	Capabilities []string `json:"capabilities"`
	// InUseBy is what is currently depending on it — what a delete
	// would refuse over, said before somebody tries.
	InUseBy   []string  `json:"in_use_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProviderResponse is one provider a credential can be created for, and
// what to ask for. It exists so a form is built from what this release
// actually supports rather than from a copy of the list that drifts.
type ProviderResponse struct {
	Provider     string   `json:"provider"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	// UsernameLabel is what to call the first field, and absent for a
	// provider whose secret is a single value — then there is no first
	// field.
	UsernameLabel string `json:"username_label,omitempty"`
	PasswordLabel string `json:"password_label"`
	Hint          string `json:"hint"`
}

func toResponse(c *Credential, uses []Use) Response {
	r := Response{
		ID: c.ID, Provider: string(c.Provider), ProviderName: c.Provider.Name(),
		Label: c.Label, Username: c.Username,
		Capabilities: capabilityNames(c.Provider),
		CreatedAt:    c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	for _, u := range uses {
		r.InUseBy = append(r.InUseBy, u.String())
	}
	return r
}

func capabilityNames(p Provider) []string {
	out := make([]string, 0, len(p.Capabilities()))
	for _, c := range p.Capabilities() {
		out = append(out, string(c))
	}
	return out
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

const credentialPath = "/credentials/{id}"

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("POST /credentials", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /credentials", auth(http.HandlerFunc(h.list)))
	// Two segments rather than one under {id}, so the mux tells the
	// list of providers from a credential without either avoiding the
	// other — and an id is a number, so they could not collide anyway.
	r.Handle("GET /credentials/providers", auth(http.HandlerFunc(h.providers)))
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
	case errors.Is(err, ErrUnknownProvider), errors.Is(err, ErrUnknownCapability),
		errors.Is(err, ErrLabelRequired), errors.Is(err, ErrUsernameRequired),
		errors.Is(err, ErrPasswordRequired), errors.Is(err, ErrUsernameNotAllowed),
		errors.Is(err, ErrCannot):
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
		Provider string `json:"provider"`
		Label    string `json:"label"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	created, err := h.svc.Create(r.Context(), user.FromContext(r.Context()), Credential{
		Provider: Provider(req.Provider), Label: req.Label,
		Username: req.Username, Password: req.Password,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created, nil))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	caller := user.FromContext(r.Context())
	all, err := h.svc.List(r.Context(), caller, Capability(r.URL.Query().Get("capability")))
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

func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	if err := user.Require(user.FromContext(r.Context()), manageRole); err != nil {
		WriteError(w, err)
		return
	}
	out := make([]ProviderResponse, 0, len(Providers()))
	for _, p := range Providers() {
		out = append(out, ProviderResponse{
			Provider: string(p), Name: p.Name(),
			Capabilities:  capabilityNames(p),
			UsernameLabel: p.UsernameLabel(),
			PasswordLabel: p.PasswordLabel(),
			Hint:          p.Hint(),
		})
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
