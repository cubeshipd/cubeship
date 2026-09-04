package user

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/platform/database"
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

	r.Handle("GET /users/me", auth(http.HandlerFunc(h.whoAmI)))
	r.HandleInternal("POST /users/me/api-key/rotate", auth(http.HandlerFunc(h.rotateAPIKey)))
	r.HandleInternal("POST /users/me/api-keys", auth(http.HandlerFunc(h.createAPIKey)))
	r.HandleInternal("GET /users/me/api-keys", auth(http.HandlerFunc(h.listAPIKeys)))
	r.HandleInternal("DELETE /users/me/api-keys/{id}", auth(http.HandlerFunc(h.revokeAPIKey)))
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
	Username     string `json:"username"`
	IsSuperAdmin bool   `json:"is_super_admin"`
}

// APIKeyResponse is one key's metadata. The key value itself appears
// only in the response to creating it.
type APIKeyResponse struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CurrentKey bool       `json:"current_key"`
}

func toAPIKeyResponses(keys []*APIKey, currentHash string) []APIKeyResponse {
	out := make([]APIKeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, APIKeyResponse{
			ID: k.ID, Name: k.Name, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt,
			CurrentKey: k.KeyHash == currentHash,
		})
	}
	return out
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
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	token, session, err := h.svc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
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
	case errors.Is(err, ErrPasswordTooShort):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	httpx.WriteJSON(w, http.StatusOK, WhoAmIResponse{Username: u.Username, IsSuperAdmin: u.IsSuperAdmin})
}

// rotateAPIKey replaces exactly the key this request authenticated with,
// keeping its name. A user can hold several independent keys precisely so
// that rotating one — routine hygiene on the key your terminal uses, for
// instance — can't silently invalidate an unrelated integration's key.
func (h *Handler) rotateAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key, err := h.svc.RotateAPIKey(ctx, FromContext(ctx), KeyHashFromContext(ctx))
	if errors.Is(err, ErrUnauthenticated) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"api_key": key})
}

func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	created, generated, err := h.svc.CreateAPIKey(r.Context(), FromContext(r.Context()), req.Name)
	if errors.Is(err, ErrUnauthenticated) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id": created.ID, "name": created.Name, "api_key": generated,
	})
}

func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	keys, err := h.svc.ListAPIKeys(ctx, FromContext(ctx))
	if errors.Is(err, ErrUnauthenticated) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAPIKeyResponses(keys, KeyHashFromContext(ctx)))
}

func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}
	switch err := h.svc.RevokeAPIKey(r.Context(), FromContext(r.Context()), id); {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, ErrLastAPIKey):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, database.ErrNotFound):
		http.Error(w, "api key not found", http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
