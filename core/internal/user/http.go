package user

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cubeship/internal/platform/httpx"
)

// Handler is this module's HTTP surface. Every method is a thin adapter
// over Service — parse the request, call one use case, write the result.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes registers this module's endpoints. auth wraps a handler in
// authentication; the server supplies it so every module is mounted the
// same way.
//
// Only the identity lookup is part of the documented API. Managing your
// own keys is self-service plumbing you do once, through the CLI or an
// MCP client — not something anyone integrates against — so those four
// routes are internal.
func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	// Signing in is the one route that cannot require being signed in.
	r.HandleInternalFunc("POST /auth/login", h.login)
	r.HandleInternal("POST /auth/logout", auth(http.HandlerFunc(h.logout)))
	r.HandleInternal("PUT /users/me/password", auth(http.HandlerFunc(h.setPassword)))

	r.Handle("POST /users", auth(http.HandlerFunc(h.add)))
	r.Handle("GET /users", auth(http.HandlerFunc(h.list)))
	r.Handle("GET /users/me", auth(http.HandlerFunc(h.whoAmI)))
	r.Handle("PATCH /users/me", auth(http.HandlerFunc(h.setPreferences)))
	r.Handle("GET /users/{username}/avatar", auth(http.HandlerFunc(h.avatar)))
	r.Handle("PUT /users/me/avatar", auth(http.HandlerFunc(h.setAvatar)))
	r.Handle("DELETE /users/me/avatar", auth(http.HandlerFunc(h.clearAvatar)))
	// Someone leaves, or a laptop does. Neither had an answer here
	// before, and "go and delete the rows yourself" is not one.
	r.Handle("PATCH /users/{username}", auth(http.HandlerFunc(h.updateUser)))
	r.Handle("POST /users/{username}/password", auth(http.HandlerFunc(h.resetPassword)))
	r.Handle("DELETE /users/{username}", auth(http.HandlerFunc(h.remove)))
	r.Handle("DELETE /users/{username}/credentials", auth(http.HandlerFunc(h.revokeCredentials)))
	r.HandleInternal("POST /users/me/api-key/rotate", auth(http.HandlerFunc(h.rotateAPIKey)))
	r.HandleInternal("POST /users/me/api-keys", auth(http.HandlerFunc(h.createAPIKey)))
	r.HandleInternal("GET /users/me/api-keys", auth(http.HandlerFunc(h.listAPIKeys)))
	r.HandleInternal("DELETE /users/me/api-keys/{id}", auth(http.HandlerFunc(h.revokeAPIKey)))

	r.Handle("GET /roles", auth(http.HandlerFunc(h.listRoles)))
	r.Handle("POST /roles", auth(http.HandlerFunc(h.createRole)))
	r.Handle("PUT /roles/{id}", auth(http.HandlerFunc(h.updateRole)))
	r.Handle("DELETE /roles/{id}", auth(http.HandlerFunc(h.deleteRole)))
}

// --- authentication ---

type contextKey string

const (
	userContextKey    contextKey = "cubeship-user"
	apiKeyHashCtxKey  contextKey = "cubeship-api-key-hash"
	sessionHashCtxKey contextKey = "cubeship-session-hash"
)

// FromContext returns the authenticated caller Middleware put in ctx.
// Only valid inside a handler mounted behind that middleware.
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userContextKey).(*User)
	return u
}

// KeyHashFromContext returns the hash of the API key the current request
// authenticated with — the specific key, not just the user it belongs to.
// Rotation needs this to replace exactly the key in use, leaving every
// other key that user holds untouched.
func KeyHashFromContext(ctx context.Context) string {
	h, _ := ctx.Value(apiKeyHashCtxKey).(string)
	return h
}

// SessionHashFromContext returns the hash of the session this request
// arrived on, or "" when it authenticated with an API key. Changing a
// password uses it to end every session except this one.
func SessionHashFromContext(ctx context.Context) string {
	h, _ := ctx.Value(sessionHashCtxKey).(string)
	return h
}

// Middleware authenticates a request and puts the caller in its context.
//
// Two credentials reach the same place. An API key is what a CLI or an
// MCP client carries; a session cookie is what a browser carries. The
// header is tried first because it is explicit — a request that sends
// both meant the key.
func (h *Handler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "

		if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, prefix) {
			u, keyHash, err := h.svc.Authenticate(r.Context(), strings.TrimPrefix(authHeader, prefix))
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey, u)
			ctx = context.WithValue(ctx, apiKeyHashCtxKey, keyHash)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// A cookie is sent by the browser, not by the page: the request
		// has to be shown to come from this dashboard before it counts
		// as something the signed-in person asked for. See
		// httpx.SameOrigin for why SameSite=Lax is not enough on its
		// own — an app deployed on this instance is same-site with it.
		if !httpx.SameOrigin(r) {
			http.Error(w, "cross-site request refused; sign in on this instance and try again",
				http.StatusForbidden)
			return
		}
		u, sessionHash, err := h.svc.AuthenticateSession(r.Context(), cookie.Value)
		if err != nil {
			// The cookie is stale. Clear it, so a browser holding an
			// expired session stops sending it on every request.
			http.SetCookie(w, h.sessionCookie(r, "", -1))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, u)
		ctx = context.WithValue(ctx, sessionHashCtxKey, sessionHash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sessionCookie builds the cookie carrying a session token.
//
// HttpOnly keeps it out of reach of any script on the page. SameSite=Lax
// is what stands in for CSRF tokens: a cross-site POST does not carry
// the cookie, so a form on another site cannot act as the signed-in
// user, while an ordinary top-level navigation still works.
//
// Secure follows the request rather than being hard-coded. A fresh
// install is reached at http://<ip>:3000 with no certificate, and a
// Secure cookie there would simply never be sent — the sign-in would
// appear to succeed and nothing would stay signed in.
func (h *Handler) sessionCookie(r *http.Request, token string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   overTLS(r),
	}
}

// overTLS reports whether the client spoke HTTPS, honouring the
// forwarded header Traefik sets — the daemon itself is always reached
// over plain HTTP from behind it.
func overTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	forwarded := r.Header.Get("X-Forwarded-Proto")
	return strings.EqualFold(strings.TrimSpace(strings.Split(forwarded, ",")[0]), "https")
}

// --- responses ---

// WhoAmIResponse is also what the MCP whoami tool returns.
type WhoAmIResponse struct {
	Username string `json:"username"`
	Role     Role   `json:"role"`
	// HasPassword says whether this account can sign in without an API
	// key. It is here because revoking a key is allowed to leave you
	// with none, and what that costs depends entirely on this: with a
	// password it costs the CLI until you make another, and without one
	// it is the way in. The screen that offers the revoke has to be
	// able to say which.
	//
	// Not a secret: it is the caller's own account, and it is the same
	// fact the sign-in page discovers by being used.
	HasPassword bool `json:"has_password"`
	// Theme is which palette this person sees the dashboard in, absent
	// for the default. It is on this response rather than on one of
	// its own because the dashboard already waits for this before it
	// renders anything — a second request would be a second thing to
	// wait for, and waiting is what a flash of the wrong colours is.
	Theme string `json:"theme,omitempty"`
	// Themes are the palettes this instance offers. Served rather than
	// compiled into the dashboard: the daemon is what refuses a name,
	// and a second list is one to disagree with.
	Themes []string `json:"themes,omitempty"`
	// What this account says about the person holding it. The first two
	// are absent when nothing was set, which is their normal state.
	//
	// Avatar is legacy metadata. AvatarURL identifies the uploaded image;
	// when it is absent, clients render initials.
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Avatar      string `json:"avatar"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	// Avatars is retained for response compatibility; new clients upload images.
	Avatars []string `json:"avatars,omitempty"`
	// Key is the API key this request carried, absent for a session.
	Key *KeyResponse `json:"key,omitempty"`
	// AccessRole is a member's role, absent for the member default.
	AccessRole string `json:"access_role,omitempty"`
	// Grants is what this request may reach — the account's role narrowed
	// by the key's — and null for everything. The dashboard reads it to
	// offer only what will be allowed; the daemon decides regardless.
	Grants []Grant `json:"grants"`
}

func grantsOf(u *User) []Grant {
	if u.Admin() {
		return nil
	}
	return u.Effective().Grants()
}

// APIKeyResponse is one key's metadata. The key value itself appears
// only in the response to creating it.
type APIKeyResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// AccessRoleID and AccessRole are the role narrowing the key, absent
	// for a key that carries all of its owner's access.
	AccessRoleID int64      `json:"access_role_id,omitempty"`
	AccessRole   string     `json:"access_role,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	CurrentKey   bool       `json:"current_key"`
}

func toAPIKeyResponses(keys []*APIKey, currentHash string, roles map[int64]string) []APIKeyResponse {
	out := make([]APIKeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, APIKeyResponse{
			ID: k.ID, Name: k.Name, AccessRoleID: k.AccessRoleID, AccessRole: roles[k.AccessRoleID],
			CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt,
			CurrentKey: k.KeyHash == currentHash,
		})
	}
	return out
}

// KeyResponse is the key a request authenticated with, as whoami
// reports it.
type KeyResponse struct {
	Name       string `json:"name"`
	AccessRole string `json:"access_role,omitempty"`
}

// RoleResponse is an access role as the API returns it.
type RoleResponse struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Grants      []Grant `json:"grants"`
	// System names a role the instance ships, which cannot be changed.
	System    string    `json:"system,omitempty"`
	Members   int       `json:"members"`
	Keys      int       `json:"keys"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toRoleResponse(r *AccessRole) RoleResponse {
	grants := r.Grants
	if grants == nil {
		grants = []Grant{}
	}
	return RoleResponse{
		ID: r.ID, Name: r.Name, Description: r.Description, Grants: grants, System: r.System,
		Members: r.Members, Keys: r.Keys, UpdatedAt: r.UpdatedAt,
	}
}

// RolesResponse is every role, and every resource a role can grant.
type RolesResponse struct {
	Roles     []RoleResponse `json:"roles"`
	Resources []ResourceInfo `json:"resources"`
}

// --- signing in ---

// StartSession signs a user in without a password, and sets the cookie.
//
// It exists for setup, which has just created the account from a
// password it verified itself: sending someone to a sign-in form to
// retype what they typed a second ago would be silly. Nothing else
// should use it — every other way in proves something first.
func (h *Handler) StartSession(w http.ResponseWriter, r *http.Request, u *User) error {
	token, session, err := h.svc.StartSession(r.Context(), u)
	if err != nil {
		return err
	}
	http.SetCookie(w, h.sessionCookie(r, token, int(time.Until(session.ExpiresAt).Seconds())))
	return nil
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	// Login is a small JSON exchange; uploads on other routes keep streaming.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSONBounded(w, r, &req, 16*1024); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "login body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	token, session, err := h.svc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, ErrRateLimited) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		// Deliberately the same answer for an unknown username and a
		// wrong password.
		http.Error(w, ErrInvalidCredentials.Error(), http.StatusUnauthorized)
		return
	}

	http.SetCookie(w, h.sessionCookie(r, token, int(SessionLifetime.Seconds())))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"expires_at": session.ExpiresAt,
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if err := h.svc.Logout(r.Context(), cookie.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.SetCookie(w, h.sessionCookie(r, "", -1))
	w.WriteHeader(http.StatusOK)
}

// setPassword sets or changes the caller's own. An account that already
// has a password must prove it knows it; one that has none is setting
// its first, and the API key it authenticated with is the proof.
func (h *Handler) setPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.NewPassword == "" {
		http.Error(w, "new_password is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err := h.svc.SetPassword(ctx, FromContext(ctx), SessionHashFromContext(ctx), req.CurrentPassword, req.NewPassword)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, ErrInvalidCredentials):
		http.Error(w, "the current password is wrong", http.StatusForbidden)
	case errors.Is(err, ErrPasswordTooShort), errors.Is(err, ErrPasswordTooLong):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		WriteError(w, err)
	}
}

// --- handlers ---

// whoAmI reports the identity of the caller's own API key. `cubeship
// registry login` uses it to learn the username to authenticate the
// registry's per-user token auth with — the saved credentials file only
// ever stored the key itself, never the username.
func (h *Handler) whoAmI(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// A miss is reported as "no password" rather than as an error: this
	// endpoint's job is to say who you are, and the CLI calls it to
	// learn a username. Failing the whole call over a hint would be the
	// tail wagging the dog.
	has, _ := h.svc.HasPassword(r.Context(), u)
	httpx.WriteJSON(w, http.StatusOK, WhoAmIResponse{
		Username: u.Username, Role: u.Role, HasPassword: has,
		Theme: u.Theme, Themes: Themes,
		DisplayName: u.DisplayName, Email: u.Email, Avatar: u.Avatar, AvatarURL: u.AvatarURL(),
		Key: h.svc.KeyResponse(r.Context(), u), AccessRole: h.svc.RoleName(r.Context(), u.AccessRoleID),
		Grants: grantsOf(u),
	})
}
