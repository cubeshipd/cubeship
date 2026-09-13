// Command catalog keeps cubeship.dev's template catalog and serves it: a
// pass over GitHub on start and every CATALOG_INTERVAL after, and the
// read-only API under /v1 the whole time.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"catalog"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	interval := 5 * time.Minute
	if v := os.Getenv("CATALOG_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute {
			logger.Fatalf("CATALOG_INTERVAL %q is not a duration of a minute or more", v)
		}
		interval = d
	}

	db, err := sql.Open("pgx", need(logger, "DATABASE_URL"))
	if err != nil {
		logger.Fatal(err)
	}
	topic := envOr("CATALOG_TOPIC", catalog.Topic)
	store := &catalog.Postgres{DB: db, Topic: topic}
	syncer := &catalog.Syncer{
		GitHub: &catalog.Client{HTTP: &http.Client{Timeout: 30 * time.Second}, Token: need(logger, "GITHUB_TOKEN"), API: "https://api.github.com"},
		Store:  store,
		Topic:  topic,
		Log:    logger,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st := &status{}
	mux := http.NewServeMux()
	(&catalog.API{
		Reader:         store,
		PublicURL:      envOr("CATALOG_PUBLIC_URL", "https://cubeship.dev/api/v1"),
		VerifiedOwners: strings.Split(envOr("CATALOG_VERIFIED_OWNERS", "cubeshipd"), ","),
		Log:            logger,
	}).Routes(mux)
	mux.Handle("GET /healthz", st)
	server := &http.Server{Addr: ":" + envOr("PORT", "8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal(err)
		}
	}()

	// A database that is not up yet is waited for, not crashed on: the
	// container restarting in a loop says less than one line a minute.
	for {
		err := store.Migrate(ctx)
		if err == nil {
			break
		}
		logger.Printf("migrations: %v; retrying in 30s", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		pass(ctx, store, syncer, st, logger, interval)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			server.Shutdown(shutdown)
			cancel()
			return
		case <-ticker.C:
		}
	}
}

func pass(ctx context.Context, store *catalog.Postgres, syncer *catalog.Syncer, st *status, logger *log.Logger, interval time.Duration) {
	// A pass never outlives the next one's turn.
	ctx, cancel := context.WithTimeout(ctx, interval-10*time.Second)
	defer cancel()
	started := time.Now()
	var rep catalog.Report
	ran, err := store.Locked(ctx, func() error {
		var err error
		rep, err = syncer.Run(ctx)
		return err
	})
	switch {
	case err != nil:
		logger.Printf("pass failed after %s: %v", time.Since(started).Round(time.Millisecond), err)
	case !ran:
		logger.Printf("another copy is running a pass; skipping this one")
		return
	default:
		logger.Printf("pass: %d repositories, %d accepted, %d rejected, %d hidden, %d failed, in %s",
			rep.Repositories, rep.Accepted, rep.Rejected, rep.Hidden, rep.Failed, time.Since(started).Round(time.Millisecond))
	}
	st.record(started, rep, err)
}

// status is the last pass. Always 200: a GitHub outage is this service
// working as intended, and the API is still answering.
type status struct {
	mu    sync.Mutex
	state struct {
		LastPass  *time.Time      `json:"last_pass"`
		LastError string          `json:"last_error,omitempty"`
		Report    *catalog.Report `json:"report,omitempty"`
	}
}

func (s *status) record(at time.Time, rep catalog.Report, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.LastPass = &at
	s.state.Report = &rep
	s.state.LastError = ""
	if err != nil {
		s.state.LastError = err.Error()
	}
}

func (s *status) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.state)
}

func need(logger *log.Logger, name string) string {
	v := os.Getenv(name)
	if v == "" {
		logger.Fatalf("%s is not set", name)
	}
	return v
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
