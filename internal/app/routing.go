package app

import (
	"context"
	"log"
	"time"

	"cubeship/internal/node"
	"cubeship/internal/platform/traefik"
)

// Spreading one app over several machines.
//
// An app runs on the machines in its replica set, and **one** of them —
// the one it names — is where its traffic arrives. That machine's
// Traefik balances across every replica, over the mesh, by container
// name.
//
// **Why one machine and not all of them.** A machine that routes a name
// asks Let's Encrypt for that name, over TLS-ALPN on its own :443. A
// machine the record does not resolve to fails that challenge every
// time, forever, and each failure is counted against a limit shared
// with everybody else under that registered domain. So the machine that
// answers for a name is the machine the record points at, there is one
// of it, and the balancing happens behind it.
//
// **Why a file and not labels.** Traefik's Docker provider sees the one
// Engine it is pointed at. A machine can discover its own containers and
// none of the ones on the rest of the cluster, so the backends that are
// elsewhere have to be told to it — and the only thing that knows where
// every replica is, is the control plane.

// RoutesFor is what one machine's edge has to serve that its own
// containers' labels do not say: every app that names it and runs on
// more than one machine.
//
// An app on **one** machine is not here at all, and that is the point of
// checking: its container carries the router already, discovered on the
// machine it is on, exactly as it was before any of this existed. What
// this adds is the case that could not work — a name with more than one
// container behind it.
func (s *Service) RoutesFor(ctx context.Context, nodeID int64) ([]node.Route, error) {
	apps, err := s.Repo().ListScoped(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, a.ID)
	}
	domains, err := s.Repo().DomainsFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := []node.Route{}
	for _, a := range apps {
		if a.NodeID != nodeID || !balanced(a) {
			continue
		}
		for _, d := range domains[a.ID] {
			port := d.Port
			if port == 0 {
				port = DefaultPort
			}
			servers := backends(a, port)
			if len(servers) == 0 {
				// Every replica is either not up or was started before
				// its name was written down. A router with no backends
				// answers 503 to traffic the label-router on this
				// machine may still be serving correctly, so it is left
				// out rather than published empty.
				continue
			}
			out = append(out, node.Route{App: ReferenceOf(a).String(), Host: d.Host, Servers: servers})
		}
	}
	return out, nil
}

// balanced is whether this machine's edge has to route an app at all,
// rather than leaving it to the container's own labels.
//
// **More than one machine** is the obvious half. The other is an app
// that has been scaled back **down** to one: a container keeps the
// labels it was created with, so the one left behind routes nothing,
// and dropping the route with the second machine would take the name
// off the internet until somebody happened to redeploy. Replica.Routed
// is what remembers that, and the route stays until a deploy puts the
// labels back on.
func balanced(a *Scoped) bool {
	if len(a.Replicas) > 1 {
		return true
	}
	for _, r := range a.Replicas {
		if r.Container != "" && !r.Routed {
			return true
		}
	}
	return false
}

// backends is every replica of an app that can actually take traffic.
//
// A replica with no container name is skipped rather than guessed at:
// the name is the address, and an app whose container predates this
// table has none until its next deploy. Skipping it costs that replica
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

// RouteWriter keeps the control plane's own routes file up to date.
//
// It runs in cmd/cubeshipd rather than in server.New, for the reason
// the metrics collector does: a server is a request handler, and a test
// that builds one must not thereby start writing files on a timer.
//
// A worker gets the same answer down its reconcile loop and writes the
// same file with the same function. This one is that loop for the
// machine that has no loop, because it is the one being called.
type RouteWriter struct {
	Apps    *Service
	DataDir string
	// TLS says whether certificates are possible, which decides the
	// entrypoint the routers sit on. Read per pass rather than captured:
	// an operator sets a domain from the dashboard, and the next pass is
	// what picks it up.
	TLS func(ctx context.Context) bool
}

// Run writes on every tick until ctx is done.
func (w *RouteWriter) Run(ctx context.Context) {
	ticker := time.NewTicker(node.Interval)
	defer ticker.Stop()
	for {
		w.once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *RouteWriter) once(ctx context.Context) {
	id, err := w.Apps.Repo().ControlPlaneID(ctx)
	if err != nil {
		return
	}
	routes, err := w.Apps.RoutesFor(ctx, id)
	if err != nil {
		log.Printf("routes: working out what this machine serves: %v", err)
		return
	}
	changed, err := node.WriteRoutes(w.DataDir, routes, w.TLS(ctx))
	if err != nil {
		log.Printf("routes: could not write them: %v", err)
		return
	}
	if changed {
		log.Printf("routes: this machine now balances %d name(s) across the cluster", len(routes))
	}
}
