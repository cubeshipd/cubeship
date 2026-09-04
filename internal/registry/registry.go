// Package registry is the embedded container registry's side of
// Cubeship: it authorizes docker login/push/pull against organization
// membership, and it receives the push notifications that trigger a
// deploy.
//
// The token signing itself lives in internal/platform/regauth; this
// module decides who may do what.
package registry

import (
	"context"
	"crypto/rsa"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/org"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/regauth"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// Handler serves the registry's token realm and its webhook.
type Handler struct {
	users *user.Service
	orgs  *org.Service
	apps  *app.Service

	// webhookToken is the shared secret the registry's own push
	// notifications carry. It is a system-to-system credential, unrelated
	// to per-user API keys.
	webhookToken string

	// settings supplies the registry host, which follows the instance's
	// domain and therefore changes without a restart.
	settings *settings.Service

	// localRegistry is where the daemon reads the registry's own API,
	// which is the address it pulls from — never the public name.
	localRegistry string

	// maintenance runs commands inside the registry container, which is
	// where garbage collection lives — it is a subcommand of the
	// registry binary, not an API. nil when the daemon has no Engine,
	// and the endpoint refuses rather than pretending.
	maintenance Maintainer

	// signingKey signs the access tokens the token endpoint issues. nil
	// until SetSigningKey is called; the endpoint 503s until then rather
	// than issuing unsigned tokens.
	signingKey *rsa.PrivateKey
}

// Maintainer is what runs a command inside the registry container, and
// what stops and starts it around one. *dockerx.Client satisfies it.
type Maintainer interface {
	InspectContainerByName(ctx context.Context, name string) (dockerx.ContainerInfo, error)
	Exec(ctx context.Context, containerID string, cmd []string) (string, int, error)
	StopContainer(ctx context.Context, id string) error
	StartContainer(ctx context.Context, id string) error
}

func NewHandler(users *user.Service, orgs *org.Service, apps *app.Service, cfg *settings.Service, webhookToken, localRegistry string) *Handler {
	return &Handler{
		users: users, orgs: orgs, apps: apps, settings: cfg,
		webhookToken: webhookToken, localRegistry: localRegistry,
	}
}

// SetMaintainer wires in what can run a garbage collection. Must be
// called before the daemon accepts requests; not safe to call
// concurrently with serving.
func (h *Handler) SetMaintainer(m Maintainer) { h.maintenance = m }

// registryHost is the public registry name, or "" while the instance has
// no domain — in which case there is no registry running to notify us.
func (h *Handler) registryHost(ctx context.Context) string {
	values, err := h.settings.Load(ctx)
	if err != nil {
		return ""
	}
	return settings.RegistryHostFor(values.Get(settings.Domain))
}

// SetSigningKey wires in the daemon's registry-token signing key. Must be
// called before the daemon accepts requests; not safe to call
// concurrently with serving.
func (h *Handler) SetSigningKey(key *rsa.PrivateKey) {
	h.signingKey = key
}

// pushPullActions is the complete set of registry actions Cubeship will
// ever grant. Anything else a client asks for — "delete", most notably,
// which would let a member erase another team's images — is dropped.
var pushPullActions = map[string]bool{"pull": true, "push": true}

// authorizeScope parses one "type:name:actions" scope string (the shape
// the Docker client sends, e.g. "repository:acme/myapp:pull,push") and
// returns it back with only the actions the caller's org membership
// actually grants for it.
//
// An action the caller isn't authorized for is silently dropped rather
// than failing the whole request — an empty access entry for a scope is
// exactly how the token spec expects "denied" to be expressed, and the
// registry then rejects that specific action.
func (h *Handler) authorizeScope(ctx scopeContext, caller *user.User, scope string) []regauth.AccessEntry {
	parts := strings.SplitN(scope, ":", 3)
	if len(parts) != 3 {
		return nil
	}
	typ, name, actionsStr := parts[0], parts[1], parts[2]
	if typ != "repository" {
		return nil
	}

	orgSlug := name
	if i := strings.Index(name, "/"); i >= 0 {
		orgSlug = name[:i]
	}

	if !caller.IsSuperAdmin {
		o, err := h.orgs.Repo().BySlug(ctx, orgSlug)
		if err != nil {
			return nil
		}
		if !h.orgs.Authorize(ctx, caller, o.ID, org.RoleMember) {
			return nil
		}
	}

	// Keep only actions this instance grants at all. Without this filter
	// the requested action list was echoed back verbatim, so a client
	// could ask for — and receive — a token for "delete".
	var granted []string
	for _, action := range strings.Split(actionsStr, ",") {
		if pushPullActions[action] {
			granted = append(granted, action)
		}
	}
	if len(granted) == 0 {
		return nil
	}
	return []regauth.AccessEntry{{Type: typ, Name: name, Actions: granted}}
}
