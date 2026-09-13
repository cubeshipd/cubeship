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
	// AutoUpdateAt is when this instance updates itself, as HH:MM, and
	// AutoUpdateTimezone is what that is in. Empty is off.
	AutoUpdateAt       string `json:"auto_update_at,omitempty"`
	AutoUpdateTimezone string `json:"auto_update_timezone,omitempty"`

	// RegistryHost is where a `docker push` goes, or empty while no
	// domain is set.
	RegistryHost string `json:"registry_host,omitempty"`

	// TLSEnabled is false until both a domain and a contact address
	// exist. While it is false, apps are served over plain HTTP.
	TLSEnabled bool `json:"tls_enabled"`

	// WildcardDomain says every name under the instance's domain
	// already resolves here — which is true of the sslip.io address a
	// default install takes, and is what lets an app be given a name
	// with no record to write and no DNS provider to connect.
	WildcardDomain bool `json:"wildcard_domain"`

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
	// GitHubOAuthReady reports whether the registered App can be
	// installed on anything but the account that owns it. An App from
	// before Cubeship asked for OAuth on install was also registered
	// private, and neither can be changed after the fact — so a false
	// here means the App has to be replaced, not fixed.
	GitHubOAuthReady bool `json:"github_oauth_ready"`

	// GitHubConnected reports whether the App's credentials are present.
	// The credentials themselves are never returned: an endpoint that
	// handed a private key back would turn every read of the
	// configuration into a way out for it.
	GitHubConnected bool `json:"github_connected"`
}

// ToResponse renders the settings for the API. Exported because the
// GitHub module writes four of them and answers with the result.
//
// publicIP is already resolved rather than worked out here, because
// working it out may take asking the host — see Service.PublicIP, which
// is what both callers use.
func ToResponse(v Values, publicIP string) Response { return toResponse(v, publicIP) }

func toResponse(v Values, publicIP string) Response {
	r := Response{
		Domain:     v.Get(Domain),
		ACMEEmail:  v.Get(ACMEEmail),
		TLSEnabled: v.HasTLS(),
	}
	if v.HasDomain() {
		r.RegistryHost = RegistryHostFor(v.Get(Domain))
	}
	r.WildcardDomain = ResolvesEveryName(v.Get(Domain))
	r.PublicIP = publicIP
	r.PublicIPConfigured = v.Get(PublicIP) != ""
	r.DNSProviderID = v.Get(DNSProviderID)
	r.AutoUpdateAt = v.Get(AutoUpdateAt)
	r.AutoUpdateTimezone = v.Get(AutoUpdateTimezone)
	r.GitHubAppSlug = v.Get(GitHubAppSlug)
	r.GitHubConnected = v.HasGitHub()
	r.GitHubOAuthReady = v.HasGitHubOAuth()
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
	ctx := r.Context()
	values, err := h.svc.All(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(values, h.svc.PublicIP(ctx, values, ReachedAt(r))))
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
		// When this instance updates itself, and what that time is in.
		// Empty is off, which is what every instance is until somebody
		// says otherwise.
		AutoUpdateAt       *string `json:"auto_update_at"`
		AutoUpdateTimezone *string `json:"auto_update_timezone"`

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
	if req.AutoUpdateAt != nil {
		if !ValidTimeOfDay(*req.AutoUpdateAt) {
			http.Error(w, ErrBadTimeOfDay.Error(), http.StatusBadRequest)
			return
		}
		values[AutoUpdateAt] = *req.AutoUpdateAt
	}
	if req.AutoUpdateTimezone != nil {
		if !ValidTimezone(*req.AutoUpdateTimezone) {
			http.Error(w, ErrBadTimezone.Error(), http.StatusBadRequest)
			return
		}
		values[AutoUpdateTimezone] = *req.AutoUpdateTimezone
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

	ctx := r.Context()
	current, err := h.svc.Set(ctx, user.FromContext(ctx), values)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(current, h.svc.PublicIP(ctx, current, ReachedAt(r))))
}
