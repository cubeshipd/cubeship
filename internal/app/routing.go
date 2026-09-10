package app

import (
	"context"
	"log"
	"time"

	"cubeship/internal/node"
	"cubeship/internal/platform/traefik"
)

// Where an app's traffic arrives, and how it reaches a container that
// may be on another machine.
//
// **Every name arrives at the control plane.** Its Traefik is the load
// balancer for the whole instance: one router per name, whose backends
// are every copy of that app anywhere in the cluster, reached by
// container name over the mesh.
//
// **The certificate is what decided this.** A machine that routes a
// name asks Let's Encrypt for it over TLS-ALPN on its own :443, so a
// machine the record does not resolve to fails that challenge every
// time, for ever, spending a limit shared with everyone else under that
// registered domain. An edge per app was the way round it and cost a
// DNS record per app — repointed by hand every time an app moved — and
// a certificate store on every box.
//
// Traefik is a load balancer. It was already the thing in front of
// every container on this machine, and this is the same job over a
// network that now exists.
//
// What it costs is worth saying plainly: all app traffic arrives here,
// so it stops when this box does. Before, an app on a worker outlived a
// dead control plane — at the price of a record per app, a certificate
// per machine, and an app on two machines being served entirely by
// whichever one its record happened to name.
//
// **A file, not labels.** Traefik's Docker provider sees the one Engine
// it is pointed at, so this machine discovers its own containers and
// none of the ones on the rest of the cluster. The control plane is the
// only thing that knows where every copy is, so it writes them out and
// its own Traefik reads them.

// Routes is every name this instance serves, and what is behind each.
//
// One router and one service per (app, host), because a host carries
// its own port — the same reason `traefik.Labels` never merged two
// names into one rule.
func (s *Service) Routes(ctx context.Context) ([]traefik.Route, error) {
	apps, err := s.Repo().ListScoped(ctx)
	if err != nil {
		return nil, err
	}
	if apps, err = s.withDomains(ctx, apps); err != nil {
		return nil, err
	}

	out := []traefik.Route{}
	for _, a := range apps {
		for _, d := range a.Domains {
			port := d.Port
			if port == 0 {
				port = DefaultPort
			}
			servers := backends(a, port)
			if len(servers) == 0 {
				// Nothing is running this app anywhere, so there is no
				// backend to name. A router with none answers 503; no
				// router at all answers 404, which is the truer of the
				// two for a name nothing is behind.
				continue
			}
			out = append(out, traefik.Route{
				App: ReferenceOf(a).String(), Host: d.Host,
				Servers: servers, Health: a.HealthPath,
			})
		}
	}
	return out, nil
}

// backends is every copy of an app that can actually take traffic.
//
// A replica with no container name is skipped rather than guessed at:
// the name is the address, and an app whose container predates that
// column has none until its next deploy. Skipping it costs that copy
// its share of the traffic; guessing would cost the whole name.
func backends(a *Scoped, port int) []string {
	var out []string
	for _, r := range a.Replicas {
		if !r.Running() || r.Name == "" {
			continue
		}
		out = append(out, traefik.ServerURL(r.Name, port))
	}
	return out
}

// RouteWriter keeps the routes file up to date.
//
// It runs in cmd/cubeshipd rather than in server.New, for the reason
// the metrics collector does: a server is a request handler, and a test
// that builds one must not thereby start writing files on a timer.
//
// **It is woken as well as ticked.** A deploy swaps a container, and
// until this has run the file still names the one that has gone —
// which is a 502 on every request for that name. A ticker alone would
// make that up to a full interval of them on every deploy of every app,
// so whatever changes the set of live containers says so, and the tick
// is the backstop for what nothing thought to announce.
type RouteWriter struct {
	Apps    *Service
	DataDir string
	// TLS says whether certificates are possible, which decides the
	// entrypoint the routers sit on. Read per pass rather than captured:
	// an operator sets a domain from the dashboard, and the next pass is
	// what picks it up.
	TLS func(ctx context.Context) bool

	// wake is buffered, so telling it to write when it is already
	// writing is not a caller that blocks — and one wake is all a pass
	// needs, since it reads everything.
	wake chan struct{}
}

// Wake asks for a write now. Safe from any goroutine, and safe before
// Run: the buffered slot holds the news until the loop starts.
func (w *RouteWriter) Wake() {
	w.ensure()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *RouteWriter) ensure() {
	if w.wake == nil {
		w.wake = make(chan struct{}, 1)
	}
}

// Run writes on every tick, and whenever something says the answer has
// changed, until ctx is done.
func (w *RouteWriter) Run(ctx context.Context) {
	w.ensure()
	ticker := time.NewTicker(node.Interval)
	defer ticker.Stop()
	for {
		w.once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

func (w *RouteWriter) once(ctx context.Context) {
	routes, err := w.Apps.Routes(ctx)
	if err != nil {
		log.Printf("routes: working out what this instance serves: %v", err)
		return
	}
	changed, err := traefik.WriteRoutes(w.DataDir, routes, w.TLS(ctx))
	if err != nil {
		log.Printf("routes: could not write them: %v", err)
		return
	}
	if changed {
		log.Printf("routes: this instance now serves %d name(s)", len(routes))
	}
}
