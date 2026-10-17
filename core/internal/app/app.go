// Package app owns apps and their lifecycle: the App and Deployment
// entities, their persistence, the zero-downtime deploy orchestrator, and
// the HTTP and MCP surfaces both are reached through.
//
// An app lives in an environment inside a project. The caller's instance
// role authorizes actions on it.
package app

import (
	"encoding/json"
	"strings"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/limits"
	"cubeship/internal/node"

	"github.com/bmatcuk/doublestar/v4"
)

// App is one deployable service: a name, the domain Traefik routes to
// it, and whichever container is currently serving it.
//
// An app on the embedded registry stores no image: the path is derived
// from its reference, so an app created before the instance had a domain
// gets a correct push path the moment one is configured. An external app
// has nothing to derive from, so SourceImage is where it pulls.
type App struct {
	ID            int64
	ProjectID     int64
	EnvironmentID int64
	Name          string
	// Domains are every name Traefik serves this app at, each with the
	// port behind it.
	//
	// Empty is a normal state, not a half-finished one: an app nobody
	// outside the instance should reach — a worker, a queue consumer,
	// something its neighbours call by container name — deploys with
	// none, and Traefik is simply given no opinion about it.
	Domains []Domain

	Source string
	// SourceImage is the image an external app pulls, without a tag.
	// Empty for every other source.
	SourceImage string
	// SourceTag is the tag this app runs, and empty means two things
	// that are one idea: on Cubeship's own registry, whatever is pushed
	// — the push is the deploy — and anywhere else, `latest`.
	//
	// Setting it is how an app is pinned, and it is therefore also how
	// autodeploy is turned off. There is no second flag, because a flag
	// and a tag can disagree and this cannot.
	SourceTag string
	// SourceRepo and SourceRef are the repository a building app builds
	// from, and which commit-ish of it. An empty ref means the
	// repository's default branch.
	SourceRepo string
	SourceRef  string
	// SourceDockerfile is the recipe's path within that repository.
	// Empty means "Dockerfile" at the root.
	SourceDockerfile string
	// HealthPath is what Traefik asks this app for to decide whether
	// the container behind a name is worth sending traffic to. Empty is
	// no check, and it is the default: see ValidHealthPath.
	HealthPath string
	Predeploy  Predeploy
	// Scale is how many copies were **asked for**, which is not how
	// many there are — that is len(Replicas), and it is what every
	// surface reports.
	//
	// **Zero means one per machine**, and it is what every app is until
	// somebody says otherwise. The distinction is load-bearing: a row
	// count cannot express "however many machines there are", so
	// reading the intent off it made taking a machine away from an app
	// running one copy on each of two leave two copies on the
	// survivor.
	Scale int
	// Spread is whether this app follows the cluster: it runs on every
	// machine there is, and is re-spread the moment one is added or
	// taken away.
	//
	// A switch rather than a second way of placing things. Nodes and
	// Scale already say where an app runs and how many of it; what they
	// cannot say is "wherever the cluster goes", which is a decision
	// that stops being one the moment somebody makes it. Naming
	// machines turns it off, because that is choosing by hand.
	Spread bool
	// Autoscale is when this instance decides the replica count for
	// itself. Off on every app until somebody turns it on — see
	// Autoscale.On.
	Autoscale Autoscale
	// Limits is what **one copy** of this app may take from the machine
	// it runs on. Zero in either half is no limit, which is what every
	// app is until somebody says otherwise.
	Limits limits.Limits
	// Replicas are the machines this app runs on, and what is running on
	// each. One machine is the ordinary case and the shape is the same:
	// a second is a row, not a different kind of app.
	Replicas []Replica
	// Volumes are the directories mounted into its container that
	// outlive it. Any at all pins the app: see Volume.
	Volumes []Volume
	// TCPPorts are its container's ports published on the control plane.
	// Any at all pins the app there: see TCPPort.
	TCPPorts        []TCPPort
	Env             envvar.Map
	StopGracePeriod time.Duration
	StopSignal      string
	WatchPaths      []string
	IgnorePaths     []string
	BuildArgs       map[string]string
	CreatedAt       time.Time
}

const DefaultStopGracePeriod = 10 * time.Second
const DefaultStopSignal = "SIGTERM"

func ValidStopPolicy(grace time.Duration, signal string) bool {
	if grace < 0 || grace > time.Hour || grace%time.Second != 0 || signal == "" {
		return false
	}
	if signal == "SIGKILL" || signal == "SIGSTOP" {
		return false
	}
	for _, r := range signal {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return strings.HasPrefix(signal, "SIG")
}

// ValidPathFilters accepts repository-relative doublestar globs. Empty
// watch paths preserve the historical deploy-on-every-push behavior.
func ValidPathFilters(watch, ignore []string) bool {
	for _, patterns := range [][]string{watch, ignore} {
		for _, pattern := range patterns {
			if pattern == "" || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "\\") || !doublestar.ValidatePattern(pattern) {
				return false
			}
		}
	}
	return true
}

// WatchPathsMatch reports whether a push with these changed repository paths
// should trigger an app. Missing paths are represented by an empty slice and
// are handled by the webhook before calling this function.
func WatchPathsMatch(watch, ignore, changed []string) bool {
	for _, path := range changed {
		skipped := false
		for _, pattern := range ignore {
			if doublestar.MatchUnvalidated(pattern, path) {
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}
		if len(watch) == 0 {
			return true
		}
		for _, pattern := range watch {
			if doublestar.MatchUnvalidated(pattern, path) {
				return true
			}
		}
	}
	return false
}

// Predeploy is an optional command run from the new image before its
// container replaces the old one. A JSON string is a shell command; an
// array is argv. Empty means disabled.
type Predeploy struct {
	Args    []string
	Shell   string
	Timeout time.Duration
}

func (p Predeploy) Empty() bool { return p.Shell == "" && len(p.Args) == 0 }

func (p Predeploy) MarshalJSON() ([]byte, error) {
	if p.Shell != "" {
		return json.Marshal(p.Shell)
	}
	return json.Marshal(p.Args)
}

func (p *Predeploy) UnmarshalJSON(b []byte) error {
	var shell string
	if err := json.Unmarshal(b, &shell); err == nil {
		p.Shell, p.Args = shell, nil
		return nil
	}
	if err := json.Unmarshal(b, &p.Args); err != nil {
		return err
	}
	p.Shell = ""
	return nil
}

func (p Predeploy) Valid() bool {
	if p.Timeout < 0 || p.Timeout > time.Hour || p.Timeout%time.Second != 0 {
		return false
	}
	if p.Empty() {
		return true
	}
	if p.Shell != "" && len(p.Args) != 0 {
		return false
	}
	if len(p.Args) > 0 && p.Args[0] == "" {
		return false
	}
	return true
}

// Replica is one machine an app runs on, and the container that is
// there.
//
// **Name is not decoration.** It is the address every other machine
// reaches this replica at over the mesh, and it is what the edge's load
// balancer is built out of — so a replica whose name nothing wrote down
// is one nothing can send traffic to. See routing.go.
type Replica struct {
	NodeID   int64
	NodeSlug string
	// Ordinal tells this copy from the others on the same machine. It
	// starts at 1, and **the first one keeps the plain container name**
	// — so an app that has always run one copy is byte-identical to
	// what it was, and only the second and later carry a suffix.
	Ordinal int
	// Container is the id, and Name is what it is called.
	Container string
	Name      string
	// Deploy is the deployment the container is running. Zero for a
	// machine that has been given the app and not yet run it.
	Deploy    int64
	Status    string
	UpdatedAt time.Time
}

// Running reports whether this replica is serving.
func (r Replica) Running() bool { return r.Status == StatusRunning && r.Container != "" }

// Status is the app as a whole, from its replicas.
//
// Derived rather than stored, for the reason every other derived status
// here is: a column saying an app is up is only as honest as whatever
// was supposed to update it, and with several machines writing there is
// no one writer to trust. **Degraded is the answer that only exists
// with more than one machine** — some of them serving and some not is a
// real state, and reporting it as `running` hides an outage while
// reporting it as `down` invents one.
func (a *App) Status() string {
	if len(a.Replicas) > 0 {
		stopped := 0
		starting := 0
		for _, r := range a.Replicas {
			if r.Status == StatusStopped {
				stopped++
			}
			if r.Status == StatusStarting {
				starting++
			}
		}
		if stopped == len(a.Replicas) {
			return StatusStopped
		}
		if starting == len(a.Replicas) {
			return StatusStarting
		}
	}
	up := 0
	for _, r := range a.Replicas {
		if r.Running() {
			up++
		}
	}
	switch {
	case up == 0 && !a.everRan():
		return StatusPending
	case up == 0:
		return StatusDown
	case up < len(a.Replicas):
		return StatusDegraded
	default:
		return StatusRunning
	}
}

// Split reports whether the machines serving this app are not all
// serving the same deployment.
//
// It is a **fact, not a fault**, and it is reported apart from Status
// because the two are orthogonal: an app can be degraded and split, or
// running and split. Every rollout across several machines passes
// through this state for as long as it takes the last machine to pull —
// which is exactly why it is worth being able to see, since a rollout
// that never finishes leaves it here for good.
//
// Only the replicas that are actually serving count. One that has been
// given the app and not yet run it is an absence rather than a second
// version, and a machine that is down is not serving anything to be
// split across.
func (a *App) Split() bool {
	var first int64
	for _, r := range a.Replicas {
		if !r.Running() || r.Deploy == 0 {
			continue
		}
		if first == 0 {
			first = r.Deploy
			continue
		}
		if r.Deploy != first {
			return true
		}
	}
	return false
}

// HasContainer reports whether anything is running this app anywhere.
// It is what says there is a log to read and something to stop.
func (a *App) HasContainer() bool {
	for _, r := range a.Replicas {
		if r.Container != "" {
			return true
		}
	}
	return false
}

// ReplicasOn is every copy a machine should be running, in ordinal
// order.
func (a *App) ReplicasOn(nodeID int64) []Replica {
	var out []Replica
	for _, r := range a.Replicas {
		if r.NodeID == nodeID {
			out = append(out, r)
		}
	}
	return out
}

// ReplicaOn is a machine's first copy, and whether it has one at all.
// For the questions that are about the machine rather than about a
// particular container — is this app on this box, what is its edge
// serving.
func (a *App) ReplicaOn(nodeID int64) (Replica, bool) {
	on := a.ReplicasOn(nodeID)
	if len(on) == 0 {
		return Replica{}, false
	}
	return on[0], true
}

// Nodes are the machines this app runs on, by name, once each and in
// the order the repository read them — which is by id, so a listing
// does not reshuffle between two reads of the same thing.
func (a *App) Nodes() []string {
	out := make([]string, 0, len(a.Replicas))
	seen := map[string]bool{}
	for _, r := range a.Replicas {
		if seen[r.NodeSlug] {
			continue
		}
		seen[r.NodeSlug] = true
		out = append(out, r.NodeSlug)
	}
	return out
}

// Elsewhere is the machines this app runs on that are not the control
// plane, by name.
//
// What it answers is whether the image has to leave this box: a build
// loaded into this Engine is one no other machine has heard of. It is
// the replicas that decide, not where the traffic arrives — that is one
// machine for every app now.
func (a *App) Elsewhere() []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range a.Replicas {
		if r.NodeSlug == node.ControlPlaneSlug || seen[r.NodeSlug] {
			continue
		}
		seen[r.NodeSlug] = true
		out = append(out, r.NodeSlug)
	}
	return out
}

// Copies is how many of this app should run, given the machines it is
// on. It is Scale, or one per machine when nobody chose a number.
func (a *App) Copies(machines int) int {
	if a.Scale > 0 {
		return a.Scale
	}
	return machines
}
