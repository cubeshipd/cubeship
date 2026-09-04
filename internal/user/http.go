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

// Routes registers this module's endpoints on mux. auth wraps a handler
// in authentication; the server supplies it so every module is mounted
// the same way.
func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /users/me", auth(http.HandlerFunc(h.whoAmI)))
	r.Handle("POST /users/me/api-key/rotate", auth(http.HandlerFunc(h.rotateAPIKey)))
	r.Handle("POST /users/me/api-keys", auth(http.HandlerFunc(h.createAPIKey)))
	r.Handle("GET /users/me/api-keys", auth(http.HandlerFunc(h.listAPIKeys)))
	r.Handle("DELETE /users/me/api-keys/{id}", auth(http.HandlerFunc(h.revokeAPIKey)))
}

// --- authentication ---

type contextKey string

const (
	userContextKey   contextKey = "cubeship-user"
	apiKeyHashCtxKey contextKey = "cubeship-api-key-hash"
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

// Middleware authenticates a bearer API key and puts the caller in the
// request context.
func (h *Handler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, prefix) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		u, keyHash, err := h.svc.Authenticate(r.Context(), strings.TrimPrefix(authHeader, prefix))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, u)
		ctx = context.WithValue(ctx, apiKeyHashCtxKey, keyHash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
