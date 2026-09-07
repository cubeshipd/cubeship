package machine

import (
	"context"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// RoleToRead is what reading the machine takes.
//
// A **member's**, like an app's metrics and a database's log. What the
// box is doing is the context for every "why is this slow" anybody
// deploying here will ever have, and none of it is a secret: it is four
// numbers about a machine whose apps that person already administers.
// Nothing here says what is on the disk, only how much of it is left.
const RoleToRead = user.RoleMember

// Service answers what the machine has been doing.
//
// It owns no configuration and takes no writes. The numbers are the
// kernel's; this holds the history of them, which is the one thing the
// kernel does not keep.
type Service struct {
	db     *database.DB
	reader *Reader
}

func NewService(db *database.DB, reader *Reader) *Service {
	return &Service{db: db, reader: reader}
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Reader is how the collector and the tests reach the machine.
func (s *Service) Reader() *Reader { return s.reader }

// Series is the machine's recent CPU, memory, disk and network.
//
// The facts about the machine come from the kernel on every read rather
// than from the newest row: a series that is empty — a daemon that
// started a minute ago — still has to be able to say how many cores
// this box has and how big its disk is, which is most of what somebody
// opening the screen wanted to know.
func (s *Service) Series(ctx context.Context, caller *user.User, window string) (Series, error) {
	if err := user.Require(caller, RoleToRead); err != nil {
		return Series{}, err
	}
	w, err := metrics.ParseWindow(window)
	if err != nil {
		return Series{}, err
	}
	samples, err := s.Repo().Series(ctx, w, now())
	if err != nil {
		return Series{}, err
	}
	out := Series{
		Window:   w.Name,
		Samples:  samples,
		Cores:    s.reader.Cores(),
		DiskPath: s.reader.DiskPath(),
	}
	if out.Samples == nil {
		// An empty list rather than null: every client of this draws a
		// chart from it, and `null.length` is a different bug in each
		// of them.
		out.Samples = []Sample{}
	}
	if mem, err := s.reader.Memory(); err == nil {
		out.MemoryTotalBytes = mem.Total
	}
	if disk, err := s.reader.Disk(); err == nil {
		out.DiskTotalBytes = disk.Total
	}
	if _, interfaces, err := s.reader.Network(); err == nil {
		out.Interfaces = interfaces
	}
	out.Unavailable = s.reader.Unavailable()
	return out, nil
}
