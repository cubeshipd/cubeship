package api

import (
	"context"
	"crypto/rsa"
	"net/http"
	"strings"
	"sync"
	"time"

	"cubeship/internal/authkey"
	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

// localRegistryHost is where the daemon pulls app images from. The
// registry container publishes 127.0.0.1:5000 precisely so the daemon's
// own pulls stay on loopback: pulling the public registry.<domain> name
// would hairpin out to the VPS's public IP and require a valid ACME
// certificate to already exist, which the spec forbids as a
// precondition for deploying.
const localRegistryHost = "127.0.0.1:5000"

// webhookDeployTimeout bounds a deploy kicked off by a registry push.
// The webhook itself acks immediately, so this is not the registry's
// notification timeout — it just stops a wedged deploy running forever.
const webhookDeployTimeout = 10 * time.Minute

type Server struct {
	store *store.Store
	orch  *deploy.Orchestrator
	// token is the shared secret the registry's own push-notification
	// webhook authenticates with — a system-to-system credential,
	// unrelated to per-user API keys. See handleRegistryWebhook.
	token        string
	registryHost string
	mux          *http.ServeMux

	// deployWG tracks deploys started by the registry webhook, which run
	// in the background after the response is sent. Tests wait on it.
	deployWG sync.WaitGroup

	// registrySigningKey signs the access tokens handleRegistryToken
	// issues. nil until SetRegistrySigningKey is called (which
	// cmd/cubeshipd/main.go does at startup); the token endpoint 503s
	// until then rather than issuing unsigned/broken tokens.
	registrySigningKey *rsa.PrivateKey
}

// SetRegistrySigningKey wires the daemon's registry-token signing key
// into the server. Must be called before the daemon starts accepting
// requests; not safe to call concurrently with handleRegistryToken.
func (s *Server) SetRegistrySigningKey(key *rsa.PrivateKey) {
	s.registrySigningKey = key
}

func NewServer(s *store.Store, orch *deploy.Orchestrator, token, registryHost string) *Server {
	srv := &Server{
		store:        s,
		orch:         orch,
		token:        token,
		registryHost: registryHost,
		mux:          http.NewServeMux(),
	}
	srv.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv.mux.HandleFunc("POST /hooks/registry", srv.handleRegistryWebhook)
	srv.mux.HandleFunc("GET /v2/token", srv.handleRegistryToken)
	srv.handleAuth("POST /orgs", srv.handleCreateOrg)
	srv.handleAuth("GET /orgs", srv.handleListOrgs)
	srv.handleAuth("POST /apps", srv.handleCreateApp)
	srv.handleAuth("GET /apps", srv.handleListApps)
	srv.handleAuth("GET /apps/{name}", srv.handleGetApp)
	srv.handleAuth("POST /apps/{name}/deploy", srv.handleManualDeploy)
	srv.handleAuth("PUT /apps/{name}/env", srv.handleSetEnv)
	srv.handleAuth("GET /apps/{name}/logs", srv.handleGetLogs)
	srv.handleAuth("POST /orgs/{slug}/users", srv.handleCreateOrgUser)
	srv.handleAuth("POST /users/me/api-key/rotate", srv.handleRotateAPIKey)
	srv.handleAuth("GET /users/me", srv.handleWhoAmI)
	return srv
}

func (s *Server) Router() http.Handler {
	return s.mux
}

type contextKey string

const userContextKey contextKey = "cubeship-user"

// userFromContext returns the authenticated caller set by authMiddleware.
// Only valid inside a handler registered via handleAuth.
func userFromContext(ctx context.Context) *store.User {
	u, _ := ctx.Value(userContextKey).(*store.User)
	return u
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		keyHash := authkey.Hash(strings.TrimPrefix(authHeader, prefix))
		user, err := s.store.GetUserByAPIKeyHash(r.Context(), keyHash)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.store.TouchAPIKeyLastUsed(r.Context(), keyHash)
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleAuth registers a handler on the mux behind authMiddleware.
func (s *Server) handleAuth(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.authMiddleware(h))
}

// localPullRef rewrites a public image reference
// (registry.<domain>/<org-slug>/<app>) into the loopback-published
// reference the daemon actually pulls. Only the repository part is
// kept; the host is replaced. See localRegistryHost.
func localPullRef(image, tag string) string {
	repo := image
	if i := strings.Index(image, "/"); i >= 0 {
		repo = image[i+1:]
	}
	return localRegistryHost + "/" + repo + ":" + tag
}
