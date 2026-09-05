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
	ContainerID      string
	Status           string
	Env              envvar.Map
	CreatedAt        time.Time
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
	// Logs is what the build printed, when the source builds. It lives
	// here because a detached deploy has nobody on the connection to
	// tell, and a build that failed is only explicable by its output.
	Logs      string
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

// Statuses an app can be in. "pending" is the initial state of an app
// that has never had an image pushed to it.
const (
	// StatusPending is a freshly created app: nothing has been deployed
	// and, until it has a domain, nothing can be.
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDown    = "down"
)

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
