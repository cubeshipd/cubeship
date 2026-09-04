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

	// PublicIP is what this instance's DNS records should point at —
	// what the operator configured, or what the daemon reads off its own
	// interface. A browser cannot work this out, which is why it is
	// here: the screen that offers to write the records needs it.
	PublicIP string `json:"public_ip,omitempty"`
	// PublicIPConfigured says the address above was typed rather than
	// detected, so a screen can offer to correct a detected one without
	// implying the operator's answer was a guess.
	PublicIPConfigured bool `json:"public_ip_configured"`

	// DNSProviderID is the stored DNS credential that writes this
	// instance's own records, or empty while its DNS is kept elsewhere.
	DNSProviderID string `json:"dns_provider_id,omitempty"`

	// GitHubAppSlug names the App this instance acts as, which is what
	// its install page is addressed by. Empty until one is registered.
	GitHubAppSlug string `json:"github_app_slug,omitempty"`
	// GitHubConnected reports whether the App's credentials are present.
	// The credentials themselves are never returned: an endpoint that
	// handed a private key back would turn every read of the
	// configuration into a way out for it.
	GitHubConnected bool `json:"github_connected"`
}

// ToResponse renders the settings for the API. Exported because the
// GitHub module writes four of them and answers with the result.
//
// reachedAt is the address the request arrived at, which is where a
// fresh install's public address comes from — see PublicAddressFor.
// Pass settings.ReachedAt(r).
func ToResponse(v Values, reachedAt string) Response { return toResponse(v, reachedAt) }

func toResponse(v Values, reachedAt string) Response {
	r := Response{
		Domain:     v.Get(Domain),
		ACMEEmail:  v.Get(ACMEEmail),
		TLSEnabled: v.HasTLS(),
	}
	if v.HasDomain() {
		r.RegistryHost = RegistryHostFor(v.Get(Domain))
	}
	r.PublicIP = v.PublicAddressFor(reachedAt)
	r.PublicIPConfigured = v.Get(PublicIP) != ""
	r.DNSProviderID = v.Get(DNSProviderID)
	r.GitHubAppSlug = v.Get(GitHubAppSlug)
	r.GitHubConnected = v.HasGitHub()
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
	httpx.WriteJSON(w, http.StatusOK, toResponse(values, ReachedAt(r)))
}

// set applies the settings given and leaves the rest alone, the same way
// PATCH on environment variables does — for the same reason: a dashboard
// saving one field must not clear another.
func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain        *string `json:"domain"`
		ACMEEmail     *string `json:"acme_email"`
		PublicIP      *string `json:"public_ip"`
		DNSProviderID *string `json:"dns_provider_id"`

		// The GitHub App's registration. Write-only, and normally
		// written once by the connect flow rather than typed.
		GitHubAppID         *string `json:"github_app_id"`
		GitHubAppSlug       *string `json:"github_app_slug"`
		GitHubPrivateKey    *string `json:"github_private_key"`
		GitHubWebhookSecret *string `json:"github_webhook_secret"`
		GitHubClientID      *string `json:"github_client_id"`
		GitHubClientSecret  *string `json:"github_client_secret"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	values := map[string]string{}
	if req.Domain != nil {
		values[Domain] = *req.Domain
	}
	if req.PublicIP != nil {
		values[PublicIP] = *req.PublicIP
	}
	if req.DNSProviderID != nil {
		values[DNSProviderID] = *req.DNSProviderID
	}
	if req.ACMEEmail != nil {
		values[ACMEEmail] = *req.ACMEEmail
	}
	for key, given := range map[string]*string{
		GitHubAppID:         req.GitHubAppID,
		GitHubAppSlug:       req.GitHubAppSlug,
		GitHubPrivateKey:    req.GitHubPrivateKey,
		GitHubWebhookSecret: req.GitHubWebhookSecret,
		GitHubClientID:      req.GitHubClientID,
		GitHubClientSecret:  req.GitHubClientSecret,
	} {
		if given != nil {
			values[key] = *given
		}
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
	httpx.WriteJSON(w, http.StatusOK, toResponse(current, ReachedAt(r)))
}
