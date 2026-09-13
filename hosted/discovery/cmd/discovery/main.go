// Command discovery keeps cubeship.dev's template catalog: a pass over
// GitHub on start, then one every DISCOVERY_INTERVAL.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"discovery"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	interval := 5 * time.Minute
	if v := os.Getenv("DISCOVERY_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute {
			logger.Fatalf("DISCOVERY_INTERVAL %q is not a duration of a minute or more", v)
		}
		interval = d
	}

	db, err := sql.Open("pgx", need(logger, "DATABASE_URL"))
	if err != nil {
		logger.Fatal(err)
	}
	store := &discovery.Postgres{DB: db}
	bucket, err := discovery.NewBucket(need(logger, "S3_ENDPOINT"), envOr("S3_REGION", "us-east-1"),
		need(logger, "S3_BUCKET"), need(logger, "S3_ACCESS_KEY_ID"), need(logger, "S3_SECRET_ACCESS_KEY"),
		envOr("S3_PATH_STYLE", "true") != "false")
	if err != nil {
		logger.Fatal(err)
	}
	syncer := &discovery.Syncer{
		GitHub:  &discovery.Client{HTTP: &http.Client{Timeout: 30 * time.Second}, Token: need(logger, "GITHUB_TOKEN"), API: "https://api.github.com"},
		Store:   store,
		Objects: bucket,
		Topic:   envOr("DISCOVERY_TOPIC", discovery.Topic),
		Log:     logger,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st := &status{}
	server := &http.Server{Addr: ":" + envOr("PORT", "8080"), Handler: st, ReadHeaderTimeout: 5 * time.Second}
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

func pass(ctx context.Context, store *discovery.Postgres, syncer *discovery.Syncer, st *status, logger *log.Logger, interval time.Duration) {
	// A pass never outlives the next one's turn.
	ctx, cancel := context.WithTimeout(ctx, interval-10*time.Second)
	defer cancel()
	started := time.Now()
	var rep discovery.Report
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

// status answers every request with the last pass. Always 200: a GitHub
// outage is this service working as intended, not a container to replace.
type status struct {
	mu    sync.Mutex
	state struct {
		LastPass  *time.Time        `json:"last_pass"`
		LastError string            `json:"last_error,omitempty"`
		Report    *discovery.Report `json:"report,omitempty"`
	}
}

func (s *status) record(at time.Time, rep discovery.Report, err error) {
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
