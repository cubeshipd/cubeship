package audit

import (
	"context"
	"log"
	"strings"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service { return &Service{db: db} }

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Record writes one event. It never fails the request it describes: a
// change that happened is not undone by the log of it failing, so the
// failure is logged and the caller carries on.
func (s *Service) Record(ctx context.Context, e Event) {
	e.Detail = strings.TrimSpace(e.Detail)
	if len(e.Detail) > maxDetail {
		e.Detail = e.Detail[:maxDetail] + "…"
	}
	// Recorded after the response, so the request's own context may be
	// gone by now.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.Repo().Insert(ctx, e); err != nil {
		log.Printf("audit: %v (%s %s by %s)", err, e.Action, e.Target, e.Username)
	}
}

// List reads the log, newest first. Admin only: it is everybody's
// activity on the instance.
func (s *Service) List(ctx context.Context, caller *user.User, f Filter) ([]*Event, error) {
	if err := user.Require(caller, user.RoleAdmin); err != nil {
		return nil, err
	}
	if f.Limit <= 0 {
		f.Limit = DefaultLimit
	}
	f.Limit = min(f.Limit, MaxLimit)
	return s.Repo().List(ctx, f)
}

// Purge drops what is past Retention.
func (s *Service) Purge(ctx context.Context) (int64, error) {
	return s.Repo().Purge(ctx, time.Now().Add(-Retention))
}
