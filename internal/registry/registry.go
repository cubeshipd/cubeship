// Package registry is the embedded container registry's side of
// Cubeship: it authorizes docker login/push/pull against organization
// membership, and it receives the push notifications that trigger a
// deploy.
//
// The token signing itself lives in internal/platform/regauth; this
// module decides who may do what.
package registry

import (
	"crypto/rsa"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/org"
	"cubeship/internal/platform/regauth"
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
	registryHost string

	// signingKey signs the access tokens the token endpoint issues. nil
	// until SetSigningKey is called; the endpoint 503s until then rather
	// than issuing unsigned tokens.
	signingKey *rsa.PrivateKey
}

func NewHandler(users *user.Service, orgs *org.Service, apps *app.Service, webhookToken, registryHost string) *Handler {
	return &Handler{users: users, orgs: orgs, apps: apps, webhookToken: webhookToken, registryHost: registryHost}
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
