package api

import (
	"net/http"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

type Server struct {
	store        *store.Store
	orch         *deploy.Orchestrator
	token        string
	registryHost string
	mux          *http.ServeMux
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
	srv.handleAuth("POST /apps", srv.handleCreateApp)
	srv.handleAuth("GET /apps", srv.handleListApps)
	srv.handleAuth("GET /apps/{name}", srv.handleGetApp)
	srv.handleAuth("POST /apps/{name}/deploy", srv.handleManualDeploy)
	srv.handleAuth("PUT /apps/{name}/env", srv.handleSetEnv)
	srv.handleAuth("GET /apps/{name}/logs", srv.handleGetLogs)
	return srv
}

func (s *Server) Router() http.Handler {
	return s.mux
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleAuth registers a handler on the mux behind authMiddleware.
// Task 8+ use this instead of calling s.mux.HandleFunc directly.
func (s *Server) handleAuth(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.authMiddleware(h))
}
