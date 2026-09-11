package certificates

import (
	"context"
	"log"
	"time"

	"cubeship/internal/platform/traefik"
)

// RetryInterval is how often this instance asks Traefik to have another
// go at a name it has no certificate for.
//
// It is set by Let's Encrypt's limits rather than by how quickly anybody
// wants the certificate. **Five failed validations per hostname per
// hour** is the one that bites, and every attempt asks for every name
// that is missing — so a name that is simply never going to work costs
// two of those five an hour, which leaves room for the deploys and the
// manual retries of somebody actually fixing it.
//
// Half an hour is also about the shape of the failures this is for. The
// ones worth retrying are a record that was written a minute ago, a
// resolver that could not be reached from one of Let's Encrypt's
// vantage points, an outage at the CA: none of them clears in seconds,
// and all of them clear well inside a day.
const RetryInterval = 30 * time.Minute

// Retrier asks Traefik to resolve the certificates it is missing, on a
// timer, for as long as any are.
//
// **It exists because nothing else ever asks twice.** Traefik resolves
// certificates when its configuration changes and at no other moment —
// see traefik.RetryCertificates — so the first attempt for a name is
// usually the only one it ever gets. A name whose record was a minute
// too fresh, or whose validation landed in a bad ten minutes at Let's
// Encrypt, then has no certificate for the life of the instance, with
// nothing anywhere trying and nothing saying why. The DNS is fixed, the
// screen still says pending, and the only way out is a redeploy of an
// app that has nothing wrong with it.
//
// **It issues nothing**, which is what keeps this module read-only in
// the way that matters: Traefik owns acme.json and still does, and this
// writes a file beside the routes that no router refers to. What it is
// spending is attempts against a shared rate limit, which is why the
// interval above is the part worth arguing about.
//
// It runs in cmd/cubeshipd rather than in server.New, for the reason
// every other loop here does: a server is a request handler, and a test
// that builds one must not thereby start asking a CA for certificates.
type Retrier struct {
	Certs   *Service
	DataDir string
	// Interval is RetryInterval when it is zero.
	Interval time.Duration
}

// Run asks on every tick until ctx is done.
//
// **The first tick is a whole interval away**, deliberately. Traefik
// resolves everything it is missing when it starts, and this daemon
// starting is very often that same moment — an install, an upgrade, a
// reboot. Asking again straight away would spend an attempt on a name
// Traefik is at that second already trying for.
func (r *Retrier) Run(ctx context.Context) {
	every := r.Interval
	if every <= 0 {
		every = RetryInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		r.Once(ctx)
	}
}

// Once is one pass: ask, if anything is waiting on an answer.
func (r *Retrier) Once(ctx context.Context) {
	pending, err := r.Certs.Pending(ctx)
	if err != nil {
		log.Printf("certificates: working out which names are missing one: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}
	if _, err := traefik.RetryCertificates(r.DataDir); err != nil {
		log.Printf("certificates: asking Traefik to try again: %v", err)
		return
	}
	log.Printf("certificates: asked Traefik to try again for %d name(s), the first being %s",
		len(pending), pending[0].Host)
}
