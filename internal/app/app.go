// Package app owns apps and their lifecycle: the App and Deployment
// entities, their persistence, the zero-downtime deploy orchestrator, and
// the HTTP and MCP surfaces both are reached through.
//
// An app lives in an environment, inside a project, inside an
// organization — and the organization is what authorizes every action on
// it.
package app

import (
	"errors"
	"strings"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
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
	// Description is what this app is, in a sentence. It and the slug
	// are all an app is created with.
	Description string
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
	// Limits is what **one copy** of this app may take from the machine
	// it runs on. Zero in either half is no limit, which is what every
	// app is until somebody says otherwise.
	Limits Limits
	// Replicas are the machines this app runs on, and what is running on
	// each. One machine is the ordinary case and the shape is the same:
	// a second is a row, not a different kind of app.
	Replicas  []Replica
	Env       envvar.Map
	CreatedAt time.Time
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

// Limits is the ceiling a container runs under: how much CPU it may use
// and how much memory it may hold.
//
// **It is per copy, not per app.** Three replicas under a one-core limit
// may take three cores between them. That is the only arithmetic that
// survives the replica count changing, and it is what every scheduler
// that has both numbers does.
//
// **Zero is no limit**, in either half and independently: capping memory
// and leaving CPU alone is an ordinary thing to want.
type Limits struct {
	// CPU is cores, and fractional on purpose — half a core is an
	// ordinary answer on a box this size. It is a ceiling rather than a
	// share: a container at its limit is throttled, not merely
	// preferred less when the machine is busy.
	CPU float64 `json:"cpu"`
	// Memory is a hard ceiling in bytes. The kernel enforces it by
	// killing the process that crosses it, which is why **lowering one
	// below what a container is already holding kills it on the spot**
	// — the one thing about this setting that surprises people.
	Memory int64 `json:"memory_bytes"`
}

// None reports whether nothing is capped.
func (l Limits) None() bool { return l.CPU == 0 && l.Memory == 0 }

// Resources is this ceiling in the Engine's own units.
func (l Limits) Resources() dockerx.Resources {
	return dockerx.Resources{
		NanoCPUs:    int64(l.CPU * 1e9),
		MemoryBytes: l.Memory,
	}
}

// MinCPU and MinMemory are the smallest ceilings that mean anything.
//
// The memory floor is the Engine's own — it refuses less than 6 MiB,
// because a cgroup below it cannot hold the runtime that would be
// started inside it. The CPU floor is a hundredth of a core, which is
// the resolution Docker's own --cpus works to; anything smaller is a
// number somebody typed wrong, and it would be rounded to zero and read
// as "no limit" — the opposite of what they asked for.
const (
	MinCPU    = 0.01
	MinMemory = 6 << 20
)

// ErrInvalidLimits is a ceiling this instance will not set. It names
// both floors rather than the one that was crossed, because whichever
// it is the other is the next thing to get wrong.
var ErrInvalidLimits = errors.New("a limit is either zero, meaning none, or at least 0.01 of a core and 6MiB of memory")

// Valid reports whether this is a ceiling the Engine would accept.
func (l Limits) Valid() bool {
	if l.CPU < 0 || l.Memory < 0 {
		return false
	}
	if l.CPU != 0 && l.CPU < MinCPU {
		return false
	}
	if l.Memory != 0 && l.Memory < MinMemory {
		return false
	}
	return true
}

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

// Spread divides a number of copies over a number of machines.
//
// Round-robin in the machines' own order, so the first few take the
// remainder: four over three machines is 2, 1, 1. Deterministic,
// because the answer decides which containers exist — a spread that
// moved between two reads would be a machine told to start a copy and
// then told to stop it.
func Spread(replicas, machines int) []int {
	if machines <= 0 {
		return nil
	}
	if replicas < machines {
		// Never fewer copies than machines: a machine an app was placed
		// on and given nothing to run is a machine somebody put it on
		// for no effect. Asking for fewer copies than machines is asking
		// for fewer machines.
		replicas = machines
	}
	out := make([]int, machines)
	for i := range out {
		out[i] = replicas / machines
		if i < replicas%machines {
			out[i]++
		}
	}
	return out
}

// everRan distinguishes an app nothing has ever deployed from one whose
// container has gone. Both have nothing running; only the second is a
// fault.
func (a *App) everRan() bool {
	for _, r := range a.Replicas {
		if r.Deploy != 0 || r.Status != StatusPending {
			return true
		}
	}
	return false
}

// Deployment is one attempt to run a new image for an app. It is created
// when the deploy is accepted and finished when it ends, so it is also
// how a caller finds out how a deploy went after the request that
// started it is long gone.
type Deployment struct {
	ID       int64
	AppID    int64
	ImageRef string
	Status   string
	Error    string
	// Stalled says this deploy is waiting on a machine that has stopped
	// answering. Derived on read; see Stall.
	Stalled *Stall
	// Logs is what the build printed, when the source builds. It lives
	// here because a detached deploy has nobody on the connection to
	// tell, and a build that failed is only explicable by its output.
	//
	// **Empty in a listing, whatever the row holds.** It is capped at
	// 256 KiB and a history is fifty rows, so a list that carried them
	// would be twelve megabytes — fetched every two seconds while a
	// deploy is running, which is exactly when somebody is looking at
	// it. HasLogs is what a listing answers instead, and one deployment
	// read on its own carries the log itself.
	Logs string
	// HasLogs says the row holds output, so a caller knows whether
	// there is anything to open without being sent it.
	HasLogs bool
	// Deletable says this record may be removed. Only one may not be:
	// a deploy still running, because the orchestrator is writing to
	// that row.
	Deletable bool
	// Live says this is the deploy the app is running — and therefore
	// that deleting it takes the app down, which is the whole reason it
	// is reported separately from Deletable rather than folded into it.
	//
	// Filled by the service, which is what knows whether the app has a
	// container at all.
	Live      bool
	CreatedAt time.Time
}

// Deployment statuses.
const (
	DeploymentPending   = "pending"
	DeploymentSucceeded = "succeeded"
	DeploymentFailed    = "failed"
)

// Done reports whether the deployment has finished, either way.
func (d *Deployment) Done() bool {
	return d.Status == DeploymentSucceeded || d.Status == DeploymentFailed
}

// Stalled is a deploy that is waiting on a machine that has stopped
// answering, and has been for long enough that it is not a blip.
//
// **It is still `pending`, and deliberately so.** A pending deploy is
// what a machine picks up when it comes back — DeploymentToRun takes
// the newest that resolved to an image and did not *fail* — so a
// machine that reboots finishes the rollout it missed. Marking it
// failed instead would send that machine to the deployment below this
// one while the machines that took it stay on this one, which is an app
// running two versions with nothing on any screen saying so.
//
// So what this changes is what is *said*, not what is run: a screen can
// name the machine everyone is waiting for instead of showing a
// spinner, and the record can be cleared by somebody who decides the
// rollout is not happening. That decision has a consequence — the
// machine that returns will run the version below — and it belongs to a
// person rather than to a timer.
//
// Derived on read like every other status here. Nothing writes it.
type Stall struct {
	// Waiting is the machines that have not taken this deploy and are
	// not answering, by name.
	Waiting []string
}

// StuckAfter is how long a machine has to have been silent before a
// deploy waiting on it stops being called "in progress".
//
// It is the **machine's** silence, not the deploy's age: a deploy
// started two minutes ago onto a box that died this morning is stalled
// now, and waiting another quarter of an hour would not make that
// truer.
//
// Long because the cheap answer is already taken. `unreachable` is
// three missed passes, which is the right patience for taking a machine
// out of a load balancer — where being wrong costs one interval of
// traffic — and nowhere near enough here, where being wrong tells
// somebody a rollout is never happening while the box is rebooting.
const StuckAfter = 15 * time.Minute

// Statuses an app can be in. "pending" is the initial state of an app
// that has never had an image pushed to it.
const (
	// StatusPending is a freshly created app: nothing has been deployed
	// and, until it has a domain, nothing can be.
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDown    = "down"
	// StatusDegraded is some of an app's machines serving and some not.
	// It cannot happen to an app on one machine, which is why it did not
	// exist before there was more than one.
	StatusDegraded = "degraded"
)

// ErrNoSuchNode is placing an app on a machine that is not in this
// cluster.
var ErrNoSuchNode = errors.New("no server of that name is in this cluster")

// ErrRemote is something this machine cannot do for an app that runs on
// another one. It is not a refusal of the act — it is the act not
// reaching that far yet.
var ErrRemote = errors.New("that app runs on another machine")

// ErrNotPlaceable refuses a placement that would not work, and says
// which half of it. See Service.checkPlacement.
var ErrNotPlaceable = errors.New("this app cannot run on another machine yet")

// DefaultPort is what a name reaches when nobody said otherwise. It is
// the most common answer rather than a detected one — see Domain.Port.
const DefaultPort = 8080

// Network is the Docker network app containers must join. Traefik
// resolves a container's backend IP on this network specifically (see the
// traefik.docker.network label), so a container left on the default
// bridge is invisible to the proxy and its domain serves 503.
const Network = "cubeship"

// NormalizeHost renders a name the way a browser sends one and Traefik
// matches it: lowercase, without a trailing dot. A name stored any other
// way is a name that never matches.
func NormalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

// SuggestedHostFor is the name an app answers at when nobody has a
// domain of their own to give it: the app's reference, most specific
// first, under the instance's own domain.
//
// It is a suggestion rather than a default. An app is created with no
// domain and that is a real state — plenty of them should never answer
// on the internet — so nothing assigns this. What it removes is the
// other side of the same problem: giving an app an address used to mean
// owning a domain, pointing a record at this host, and waiting for it,
// before anything could be reached at all.
//
// Whether it resolves without further work depends on the instance's
// domain. An install that took its default has an sslip.io address, and
// every name under one of those already resolves to the same host — so
// this is a name that works the moment it is added. Under a real
// domain it needs a wildcard record, or the DNS provider that writes
// the instance's own records.
func SuggestedHostFor(ref Reference, instanceDomain string) string {
	if instanceDomain == "" {
		return ""
	}
	host := NormalizeHost(strings.Join(
		[]string{ref.Name, ref.Environment, ref.Project, instanceDomain}, "."))
	if !ValidHost(host) {
		return ""
	}
	return host
}

// MaxHealthPathLength bounds the path. Generous for a path and far
// short of anything that would make a label or a YAML line unwieldy.
const MaxHealthPathLength = 255

// ValidHealthPath reports whether a health check path is one this
// instance will hand to Traefik.
//
// **Empty is valid, and means no check.** A health check needs a path,
// the path is something only the app's author knows, and a wrong one
// does not degrade anything by halves: Traefik marks every replica down
// at once and the name answers 503. So no check is the default, and
// turning one on is a deliberate act by somebody who knows what the app
// answers.
//
// The rule is strict for the same reason ValidHost's is: this value is
// interpolated into a Traefik dynamic YAML document *and* into a
// container label, so a quote, a newline or a colon in it is
// configuration somebody else wrote. A leading slash is required
// because Traefik uses it as a request URI, and one without it silently
// checks something else.
func ValidHealthPath(path string) bool {
	if path == "" {
		return true
	}
	if path[0] != '/' || len(path) > MaxHealthPathLength {
		return false
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("/-._~%!$&'()*+,;=:@", c) >= 0:
			// The unreserved and sub-delimiter characters a path
			// segment may hold, per RFC 3986, and nothing else. `?` and
			// `#` are left out deliberately: a health check with a
			// query string is not something this refuses to *do* so
			// much as something it will not guess the meaning of.
		default:
			return false
		}
	}
	return true
}

// ErrInvalidHealthPath is a path this instance will not hand to
// Traefik. It names the two rules rather than the character, because
// the character is usually a typo and the rule is what somebody has to
// read.
var ErrInvalidHealthPath = errors.New("a health check path has to start with / and hold only what a URL path may: no spaces, quotes, query strings or fragments")

// MaxHostLength is what a DNS name can be, dots included.
const MaxHostLength = 253

// ValidHost reports whether host is a DNS name, on an already
// normalised value.
//
// The rule is the grammar, not a guess at what resolves: labels of
// letters, digits and dashes, none of them starting or ending with a
// dash, none empty, and 253 characters in total.
//
// It is checked because the name is interpolated into a Traefik rule —
// Host(`%s`) — and a backtick, a space or a `||` in it is a routing rule
// somebody else wrote. An admin is the only caller today and already
// controls the instance's own domain, so this is not the boundary that
// makes the instance safe; it is the one that keeps a typo from becoming
// a rule nobody can see. A name that cannot appear in DNS could never
// have reached the app anyway.
func ValidHost(host string) bool {
	if host == "" || len(host) > MaxHostLength {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			default:
				return false
			}
		}
	}
	return true
}

var (
	// ErrDomainTaken is a name another app already answers at. Traefik
	// routes by host and nothing else, so two apps claiming one name
	// would give it two answers.
	ErrDomainTaken = errors.New("another app is already served at that name")

	// ErrBadHost is a name that is not a DNS name. It goes into a
	// Traefik routing rule, so what it may contain is the grammar and
	// nothing wider.
	ErrBadHost = errors.New("that is not a valid host name")

	// ErrHostIsTheInstance is one of the names this instance answers at
	// itself. Traefik routes by host and nothing else, so an app
	// claiming it would fight the daemon's own router for the dashboard
	// or the registry, and which one won would be a detail of label
	// ordering.
	ErrHostIsTheInstance = errors.New("that name is this instance's own; the dashboard and registry answer there")

	// ErrDomainNotFound is a domain id that is not this app's.
	ErrDomainNotFound = errors.New("no such domain on this app")

	// ErrBadPort is a port outside what a port can be.
	ErrBadPort = errors.New("a port is between 1 and 65535")

	// ErrNotFound covers both "no such app" and "not yours to see", so a
	// response never confirms that another organization's app exists.
	ErrNotFound = errors.New("app not found")

	// ErrAlreadyExists reports a name already taken in that environment.
	ErrAlreadyExists = errors.New("app already exists")

	// ErrNoContainer reports an app that has never had an image pushed
	// to it, so there is nothing to read logs from.
	ErrNoContainer = errors.New("app has no running container")

	// ErrDeploymentNotFound covers a deployment id that does not belong
	// to the app it was asked for.
	ErrDeploymentNotFound = errors.New("deployment not found")

	// ErrDeploymentRunning refuses deleting a deploy that has not
	// finished. The orchestrator is still writing that row.
	ErrDeploymentRunning = errors.New("this deploy is still running; wait for it to finish")

	// ErrNoRegistry reports that the instance has no domain yet, so
	// there is no registry to push to or pull from.
	ErrNoRegistry = errors.New("no registry: set a domain in the instance settings first")
	// ErrNoBuilder reports a daemon built without one. It cannot happen
	// in a real install; it exists so a test wiring the orchestrator
	// without a builder fails loudly rather than at a nil pointer.
	ErrNoBuilder = errors.New("this daemon has no image builder")
)

// Domain is one name an app is served at, and the port behind it.
//
// The pair is the unit, and it has to be: a container can expose several
// ports, so "which port does this app listen on" stops having one answer
// as soon as the app has more than one name. api.example.com and
// admin.example.com on one image are two ports on one container.
type Domain struct {
	ID   int64
	Host string
	// Port is what this name reaches inside the container. Zero falls
	// back to DefaultPort.
	//
	// It is asked for rather than detected. Cubeship used to read the
	// image's EXPOSE, and that was a guess dressed as an answer: EXPOSE
	// is a hint the image's author wrote, an image exposing several has
	// no single answer, and an app that has not been built yet has no
	// image to ask. When the guess was wrong the container came up
	// answering nothing, at a name that resolved, for a reason nobody
	// could see.
	Port int
}

// Hosts is every name this app answers at.
func (a *App) Hosts() []string {
	out := make([]string, 0, len(a.Domains))
	for _, d := range a.Domains {
		out = append(out, d.Host)
	}
	return out
}
