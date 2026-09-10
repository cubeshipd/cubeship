package update

import (
	"context"
	"log"
	"time"

	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// Scheduler updates the instance at an hour somebody chose.
//
// **A time of day, not an interval.** What is being chosen is when this
// instance is allowed to be briefly unusable, and "every 24 hours from
// whenever you turned it on" is not something anybody can plan around.
//
// It runs in cmd/cubeshipd rather than in server.New, for the reason
// every other loop here does: a server is a request handler, and a test
// that builds one must not thereby start replacing containers.
type Scheduler struct {
	Updates  *Service
	Settings *settings.Service
	// Now is replaceable for a test. Nil is time.Now.
	Now func() time.Time
}

// ScheduleTick is how often the clock is read.
//
// A minute, because the setting is to the minute. Cheaper than it looks:
// it is two settings rows and a comparison, and the release lookup only
// happens in the minute that matches.
const ScheduleTick = time.Minute

// Run watches the clock until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(ScheduleTick)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// The minute this fired in, so a tick that lands twice in one
		// minute — a clock adjustment, a slow tick — does not start
		// two updates. The run itself refuses a second one anyway;
		// this stops the *lookup* happening twice.
		at, in := s.when(ctx)
		if at == "" {
			continue
		}
		now := s.now().In(in)
		stamp := now.Format("2006-01-02 15:04")
		if now.Format("15:04") != at || stamp == last {
			continue
		}
		last = stamp
		s.once(ctx)
	}
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// when is the configured time and the location it is in. An empty time
// is off; a timezone that is not one is UTC, because refusing to update
// over a typo in a timezone would be a setting that silently turns the
// feature off.
func (s *Scheduler) when(ctx context.Context) (string, *time.Location) {
	values, err := s.Settings.Load(ctx)
	if err != nil {
		return "", time.UTC
	}
	at := values[settings.AutoUpdateAt]
	if at == "" {
		return "", time.UTC
	}
	in, err := time.LoadLocation(values[settings.AutoUpdateTimezone])
	if err != nil || values[settings.AutoUpdateTimezone] == "" {
		in = time.UTC
	}
	return at, in
}

// once updates if there is anything to update to.
//
// **Stable releases only**, which is what Newer answers: an instance
// left to update itself must not wander onto a release candidate at
// three in the morning.
//
// The caller is the instance itself, so it passes an admin that is not
// anybody: this is machinery, and the authorization on updating lives
// where a person reaches it.
func (s *Scheduler) once(ctx context.Context) {
	if r := s.Updates.Current(); r.Running() {
		return
	}
	newer, err := Newer(ctx, s.Updates.client, s.Updates.Version())
	if err != nil {
		log.Printf("auto-update: checking for a newer release: %v", err)
		return
	}
	if newer == nil {
		return
	}
	log.Printf("auto-update: %s is out; updating", newer.Version)
	if _, err := s.Updates.Start(ctx, &user.User{Role: user.RoleAdmin}, newer.Version); err != nil {
		log.Printf("auto-update to %s: %v", newer.Version, err)
	}
}
