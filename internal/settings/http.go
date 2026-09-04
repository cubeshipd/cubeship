package settings

import (
	"errors"
	"net/http"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is the instance's configuration as the API reports it, plus
// what the daemon can actually do with it — so a dashboard does not have
// to re-derive the rules.
type Response struct {
	Domain    string `json:"domain"`
	ACMEEmail string `json:"acme_email"`

	// RegistryHost is where a `docker push` goes, or empty while no
	// domain is set.
	RegistryHost string `json:"registry_host,omitempty"`

	// TLSEnabled is false until both a domain and a contact address
	// exist. While it is false, apps are served over plain HTTP.
	TLSEnabled bool `json:"tls_enabled"`
}

func toResponse(v Values) Response {
	r := Response{
		Domain:     v.Get(Domain),
		ACMEEmail:  v.Get(ACMEEmail),
		TLSEnabled: v.HasTLS(),
	}
	if v.HasDomain() {
		r.RegistryHost = RegistryHostFor(v.Get(Domain))
	}
	return r
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /settings", auth(http.HandlerFunc(h.get)))
	r.Handle("PUT /settings", auth(http.HandlerFunc(h.set)))
}

func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSuperAdminOnly):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrUnknownKey):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	values, err := h.svc.All(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(values))
}

// set applies the settings given and leaves the rest alone, the same way
// PATCH on environment variables does — for the same reason: a dashboard
// saving one field must not clear another.
func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain    *string `json:"domain"`
		ACMEEmail *string `json:"acme_email"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	values := map[string]string{}
	if req.Domain != nil {
		values[Domain] = *req.Domain
	}
	if req.ACMEEmail != nil {
		values[ACMEEmail] = *req.ACMEEmail
	}
	if len(values) == 0 {
		http.Error(w, "give domain, acme_email, or both", http.StatusBadRequest)
		return
	}

	current, err := h.svc.Set(r.Context(), user.FromContext(r.Context()), values)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(current))
}
