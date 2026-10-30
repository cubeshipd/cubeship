package app

import (
	"context"
	"fmt"
	"strings"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/traefik"
)

func healthPort(a *Scoped) int {
	if len(a.HealthChecks) > 0 {
		return a.HealthChecks[0].Port
	}
	for _, d := range a.Domains {
		if d.Port > 0 {
			return d.Port
		}
	}
	if len(a.InternalPorts) == 1 {
		return a.InternalPorts[0]
	}
	if len(a.TCPPorts) > 0 {
		return a.TCPPorts[0].ContainerPort
	}
	return DefaultPort
}

// inheritedEnv resolves the full environment a deploy of a should run
// with: the project's vars, overridden by its environment's vars,
// overridden by the app's own vars — so an app can override a value its
// environment sets, and an environment can override one its project sets,
// but never the other way around.
func (o *Orchestrator) inheritedEnv(ctx context.Context, a *App) (envvar.Map, error) {
	p, err := o.proj.ByID(ctx, a.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	e, err := o.envs.ByID(ctx, a.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("get environment: %w", err)
	}
	// A database's variables are read fresh at every deploy, not stored
	// on the app: an attachment made after the last deploy is exactly
	// the case this has to pick up, and the connection string is
	// derived from the datastore rather than copied anywhere.
	var stores envvar.Map
	if o.datastores != nil {
		stores, err = o.datastores.VarsForApp(ctx, a.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve attached datastores: %w", err)
		}
	}
	var buckets envvar.Map
	if o.objectStores != nil {
		buckets, err = o.objectStores.VarsForApp(ctx, a.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve attached object stores: %w", err)
		}
	}
	return envvar.Merge(p.Env, e.Env, stores, buckets, a.Env), nil
}

// routing pairs every name the app answers at with the port behind it.
//
// A domain with no port of its own gets DefaultPort. Nothing inspects
// the image: EXPOSE is a hint the author wrote, not a promise, and an
// image exposing several has no single answer — so guessing produced a
// container that came up answering nothing, at a name that resolved, for
// a reason nobody could see. The port is asked for instead.
func (o *Orchestrator) routing(domains []Domain) []traefik.Domain {
	out := make([]traefik.Domain, 0, len(domains))
	for _, d := range domains {
		port := d.Port
		if port == 0 {
			port = DefaultPort
		}
		out = append(out, traefik.Domain{Host: d.Host, Port: port})
	}
	return out
}

// SetMeshNetwork wires in what says whether this instance is a cluster.
//
// It answers with the cluster overlay's name when there is one and with
// nothing when there is not, and what it decides is one extra network
// on every container this deploys — the local bridge stays, because
// everything on this box already resolves the container there.
//
// A function rather than an interface because that is the whole of it,
// and `server.New` is the only caller. See internal/mesh.
func (o *Orchestrator) SetMeshNetwork(fn func(context.Context) string) { o.meshNetwork = fn }

// mesh is the network to also join, or none. Nothing is asked of the
// Engine when nobody wired one in, which is every test.
func (o *Orchestrator) mesh(ctx context.Context) []string {
	if o.meshNetwork == nil {
		return nil
	}
	if name := o.meshNetwork(ctx); name != "" {
		return []string{name}
	}
	return nil
}

// cap applies a new ceiling to every copy of an app that is already
// running, without restarting any of them.
//
// A ceiling is the one part of a container the Engine can change under
// a running process, which is what makes raising an app's memory a
// request rather than a redeploy. Nothing is pulled and nothing swaps.
//
// A copy on another machine is capped by that machine — the ceiling
// travels in its placement — so all there is to do here is wake it, the
// same way a deploy does.
//
// **Removing a limit is the one direction that waits.** The Engine
// merges an update and reads a zero as "leave that one alone", so a
// container goes back to uncapped by being created again, which is the
// app's next deploy. Until then it keeps the ceiling it has.
func (o *Orchestrator) cap(ctx context.Context, a *App, limits Limits) error {
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	for _, r := range a.Replicas {
		if r.NodeID != here {
			if o.remote != nil {
				o.remote.Wake(r.NodeID)
			}
			continue
		}
		if r.Container == "" {
			continue
		}
		if err := o.docker.SetResources(ctx, r.Container, limits.Resources()); err != nil {
			// The limit is stored either way — it is what the next
			// container is created with. What failed is the running
			// one taking it now, and that is worth saying rather than
			// logging: a cgroup the host cannot enforce is a fact
			// about the machine, not a transient error.
			return fmt.Errorf("the limit is saved, and %s did not take it: %w", r.Name, err)
		}
	}
	return nil
}

// runningTag is the tag of the image this app should be running: the
// newest deploy that resolved to one and did not fail — the same row a
// machine falls back to, so a redeploy and a rollback cannot disagree
// about which version an app is on.
//
// It answers "" when there is no such deploy, or when the ref is pinned
// by digest rather than named by a tag, and the source fills that in.
func (o *Orchestrator) runningTag(ctx context.Context, appID int64) string {
	d, err := o.apps.DeploymentToRun(ctx, appID)
	if err != nil || d == nil {
		return ""
	}
	return tagOf(d.ImageRef)
}

// tagOf is the tag part of an image reference, or "" for one carrying a
// digest or no tag at all. The colon it looks for is the last one, and
// only when nothing after it is a path separator — a registry host with
// a port has a colon too.
func tagOf(ref string) string {
	if strings.Contains(ref, "@") {
		return ""
	}
	i := strings.LastIndex(ref, ":")
	if i < 0 || strings.Contains(ref[i+1:], "/") {
		return ""
	}
	return ref[i+1:]
}
