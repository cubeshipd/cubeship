package app

import (
	"errors"
	"net/http"
	"time"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/user"
)

// DefaultLogTail is how much of an app's log the API returns when the
// caller doesn't ask for a specific amount. A container that has been up
// for weeks holds far more output than anyone wants streamed at them, and
// the recent lines are the ones that explain what is happening now.
const DefaultLogTail = "500"

// Response is one app as both the API and the MCP tools report it.
type Response struct {
	// Reference is the app's canonical identifier,
	// project/environment/name — also its registry repository path.
	Reference string `json:"reference"`
	Name      string `json:"name"`
	// Domains are every name this app answers at, each with the port
	// behind it.
	Domains []DomainResponse `json:"domains"`
	// Image is the image this app is about, which depends on where it
	// comes from: for a registry app, where a push should go — empty
	// while the instance has no domain, because there is nowhere to push
	// yet — and for an external app, what it pulls.
	Image string `json:"image,omitempty"`
	// Tag is the tag this app runs, and absent means the default for
	// where it pulls from: on Cubeship's own registry, whatever is
	// pushed — which is what `autodeploy` reports — and anywhere else,
	// `latest`.
	Tag string `json:"tag,omitempty"`
	// Autodeploy says a push to this instance's registry deploys this
	// app. Derived rather than stored: it is true exactly when the app
	// is on that registry and pinned to no tag, so the two cannot
	// disagree about what happens when something is pushed.
	Autodeploy bool   `json:"autodeploy,omitempty"`
	Status     string `json:"status"`
	// HasContainer says whether a container currently backs this app,
	// which is what decides whether there is a log to read. The status
	// alone cannot answer it: an app that has never been deployed and
	// one whose container went away both read as not running, and only
	// the second has anything to say.
	HasContainer bool `json:"has_container"`
	// Source is where this app's image comes from.
	Source string `json:"source"`
	// Repo, Ref and Dockerfile describe a building app's source. Absent
	// for one that does not build.
	Repo        string `json:"repo,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Dockerfile  string `json:"dockerfile,omitempty"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
	// Nodes are the machines it runs on, by name, once each. One is the
	// ordinary answer and always includes Node; several is an app whose
	// traffic that edge spreads across them.
	Nodes []string `json:"nodes"`
	// Scale is how many copies run in total, across those machines. One
	// per machine is the ordinary answer, and it is what the spread
	// divides: four over three machines is 2, 1, 1.
	//
	// A number where `replicas` is the list, because they are two
	// shapes of the same fact and reusing one name for both is how a
	// client ends up sending an array where a count was meant.
	Scale int `json:"scale"`
	// Spread says this app follows the cluster: it runs on every machine
	// there is, and is re-spread whenever one is added or taken away.
	// Absent on an app placed by hand, which is every app until
	// somebody turns this on.
	Spread bool `json:"spread,omitempty"`
	// HealthPath is what Traefik asks this app for before it trusts a
	// container with traffic. Absent is no check, which is the default.
	HealthPath       string     `json:"health_path,omitempty"`
	PredeployCommand *Predeploy `json:"predeploy_command,omitempty"`
	PredeployTimeout int64      `json:"predeploy_timeout,omitempty"`
	StopGracePeriod  int64      `json:"stop_grace_period"`
	StopSignal       string     `json:"stop_signal"`
	WatchPaths       []string   `json:"watch_paths,omitempty"`
	IgnorePaths      []string   `json:"ignore_paths,omitempty"`
	// Limits is what **one copy** of this app may take from the machine
	// it runs on: three replicas under a one-core limit may take three
	// cores. Zero in either half is no ceiling, which is the default
	// and what every app had before this existed.
	Limits Limits `json:"limits"`
	// Autoscale is when this instance changes the replica count on its
	// own. Off on every app until somebody turns it on: `max` is zero.
	Autoscale Autoscale `json:"autoscale"`
	// Replicas is what is running on each of those machines. It is what
	// a `degraded` status is made of: which of them is serving, and
	// which is not.
	Replicas []ReplicaResponse `json:"replicas"`
	// Split says the machines serving this app are not all serving the
	// same deployment. Reported apart from Status because the two are
	// orthogonal — an app can be degraded and split, or running and
	// split — and absent on the overwhelming majority of apps, which
	// run one version on one machine.
	Split bool `json:"split,omitempty"`
	// Address is where a DNS record for this app has to point, which is
	// this instance's own: every name arrives at the control plane and
	// is routed from there to whichever machine runs the app. Empty
	// when the instance could not work out a routable address of its
	// own — see settings.PublicIP.
	Address string `json:"address,omitempty"`
	// SuggestedHost is a name this app could answer at, under the
	// instance's own domain — see SuggestedHostFor. Nothing assigns it:
	// an app with no domain is a normal app, and this is only what the
	// dashboard offers when somebody does want one. Empty while the
	// instance has no domain to build it under.
	SuggestedHost string `json:"suggested_host,omitempty"`
	// InternalHost is where another app on this instance reaches this
	// one — see InternalHost. Always present: it is derived from the
	// reference, so every app has one whether or not it answers at any
	// public name, and a worker with no domain at all is exactly the
	// app most likely to be called this way.
	InternalHost string `json:"internal_host"`
}

// ReplicaResponse is one machine an app runs on.
type ReplicaResponse struct {
	Node   string `json:"node"`
	Status string `json:"status"`
	// Serving is whether the edge is sending traffic here. A replica
	// that is up but has no name written down is not a backend — see
	// app.Replica.Name — and that difference is worth being able to see
	// rather than reading as an even split that is not happening.
	Serving bool `json:"serving"`
	// Ordinal tells this copy from the others of the same app on the
	// same machine, starting at 1. Absent on the first, which is the
	// only one an app that has never been scaled out has.
	Ordinal int `json:"ordinal,omitempty"`
	// Deploy is which deployment this machine is running, by id — the
	// same id the app's deploy history is listed under. Absent for a
	// machine that has been given the app and not yet run it.
	//
	// It is here because an app's machines can be on different ones,
	// and until this was reported nothing said so: a replica running
	// *something* reads as running, so two versions read as one healthy
	// app.
	Deploy int64 `json:"deploy,omitempty"`
}

func toReplicas(a *Scoped) []ReplicaResponse {
	out := make([]ReplicaResponse, 0, len(a.Replicas))
	for _, r := range a.Replicas {
		ordinal := r.Ordinal
		if ordinal == 1 {
			ordinal = 0
		}
		out = append(out, ReplicaResponse{
			Node: r.NodeSlug, Status: r.Status, Deploy: r.Deploy, Ordinal: ordinal,
			Serving: r.Running() && (len(a.Replicas) == 1 || r.Name != ""),
		})
	}
	return out
}

// toResponse needs the instance's configuration, which is not a
// property of the app, so it is passed in — resolved once per request
// instead of once per app in a listing.
func toResponse(a *Scoped, in Instance) Response {
	ref := ReferenceOf(a)
	var predeploy *Predeploy
	if !a.Predeploy.Empty() {
		p := a.Predeploy
		predeploy = &p
	}
	r := Response{
		Reference: ref.String(),
		Name:      a.Name, Domains: toDomains(a.Domains),
		Status: a.Status(), HasContainer: a.HasContainer(), Source: a.Source,
		Project: a.ProjectSlug, Environment: a.EnvironmentSlug,
		SuggestedHost:    SuggestedHostFor(ref, in.Domain),
		InternalHost:     InternalHost(ref),
		Nodes:            a.Nodes(),
		Scale:            len(a.Replicas),
		HealthPath:       a.HealthPath,
		PredeployCommand: predeploy,
		PredeployTimeout: int64(a.Predeploy.Timeout / time.Second),
		StopGracePeriod:  int64(a.StopGracePeriod / time.Second),
		StopSignal:       a.StopSignal,
		Spread:           a.Spread,
		Limits:           a.Limits,
		Autoscale:        a.Autoscale,
		Replicas:         toReplicas(a),
		Split:            a.Split(),
		Address:          in.PublicIP,
	}
	switch Source(a.Source) {
	case SourceExternal:
		r.Image, r.Tag = a.SourceImage, a.SourceTag
	case SourceDockerfile, SourceRailpack:
		r.Repo, r.Ref, r.Dockerfile = a.SourceRepo, a.SourceRef, a.SourceDockerfile
	default:
		if in.RegistryHost != "" {
			r.Image = ref.ImageFor(in.RegistryHost)
		}
		// The push is the deploy for exactly the app that has not been
		// pinned. Derived here rather than stored, so there is no flag
		// that could say one thing while the tag says the other.
		r.Tag, r.Autodeploy = a.SourceTag, a.SourceTag == ""
	}
	return r
}

func toResponses(apps []*Scoped, in Instance) []Response {
	out := make([]Response, 0, len(apps))
	for _, a := range apps {
		out = append(out, toResponse(a, in))
	}
	return out
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// An app is addressed by its four-part reference, because a name is only
// unique within its environment.
const appPath = "/apps/{project}/{env}/{name}"

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("POST /apps", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /apps", auth(http.HandlerFunc(h.list)))
	r.Handle("GET "+appPath, auth(http.HandlerFunc(h.get)))
	r.Handle("PATCH "+appPath, auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE "+appPath, auth(http.HandlerFunc(h.delete)))
	r.Handle("POST "+appPath+"/stop", auth(http.HandlerFunc(h.stop)))
	r.Handle("POST "+appPath+"/start", auth(http.HandlerFunc(h.start)))
	r.Handle("POST "+appPath+"/deploy", auth(http.HandlerFunc(h.deploy)))
	r.Handle("GET "+appPath+"/deployments", auth(http.HandlerFunc(h.deployments)))
	r.Handle("GET "+appPath+"/deployments/{id}", auth(http.HandlerFunc(h.deployment)))
	r.Handle("DELETE "+appPath+"/deployments/{id}", auth(http.HandlerFunc(h.deleteDeployment)))
	r.Handle("POST "+appPath+"/deployments/{id}/cancel", auth(http.HandlerFunc(h.cancelDeployment)))
	r.Handle("POST "+appPath+"/domains", auth(http.HandlerFunc(h.addDomain)))
	r.Handle("PATCH "+appPath+"/domains/{domainID}", auth(http.HandlerFunc(h.setDomainPort)))
	r.Handle("DELETE "+appPath+"/domains/{domainID}", auth(http.HandlerFunc(h.removeDomain)))
	r.Handle("GET "+appPath+"/env", auth(http.HandlerFunc(h.getEnv)))
	r.Handle("PUT "+appPath+"/env", auth(http.HandlerFunc(h.setEnv)))
	r.Handle("PATCH "+appPath+"/env", auth(http.HandlerFunc(h.mergeEnv)))
	r.Handle("GET "+appPath+"/logs", auth(http.HandlerFunc(h.logs)))
	r.Handle("GET "+appPath+"/metrics", auth(http.HandlerFunc(h.metrics)))
	r.Handle("GET "+appPath+"/volumes", auth(http.HandlerFunc(h.listVolumes)))
	r.Handle("POST "+appPath+"/volumes", auth(http.HandlerFunc(h.addVolume)))
	r.Handle("DELETE "+appPath+"/volumes/{volumeID}", auth(http.HandlerFunc(h.removeVolume)))
	r.Handle("GET "+appPath+"/tcp-ports", auth(http.HandlerFunc(h.listTCPPorts)))
	r.Handle("POST "+appPath+"/tcp-ports", auth(http.HandlerFunc(h.addTCPPort)))
	r.Handle("DELETE "+appPath+"/tcp-ports/{portID}", auth(http.HandlerFunc(h.removeTCPPort)))
	r.Handle("GET /volumes/orphans", auth(http.HandlerFunc(h.volumeOrphans)))
	r.Handle("DELETE /volumes/orphans/{id}", auth(http.HandlerFunc(h.deleteVolumeOrphan)))
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Stop(r.Context(), user.FromContext(r.Context()), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(a, h.svc.InstanceConfig(r.Context())))
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Start(r.Context(), user.FromContext(r.Context()), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(a, h.svc.InstanceConfig(r.Context())))
}

// refFrom builds the app reference from the request path.
func refFrom(r *http.Request) Reference {
	return Reference{
		Project:     r.PathValue("project"),
		Environment: r.PathValue("env"),
		Name:        r.PathValue("name"),
	}
}

// WriteError maps this module's domain errors onto status codes, falling
// through to project's mapping for errors it re-raises.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrAlreadyExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrUnknownSource), errors.Is(err, ErrImageRequired),
		errors.Is(err, ErrImageNotAllowed), errors.Is(err, ErrImageCarriesTag),
		errors.Is(err, ErrTagNotAllowed), errors.Is(err, ErrInvalidTag),
		errors.Is(err, ErrRepoRequired), errors.Is(err, ErrRepoNotAllowed),
		errors.Is(err, ErrRepoNotSupported), errors.Is(err, ErrDockerfileNotAllowed):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrBadHost), errors.Is(err, ErrHostIsTheInstance),
		errors.Is(err, ErrHostRequired), errors.Is(err, ErrInvalidHealthPath),
		errors.Is(err, ErrInvalidLimits), errors.Is(err, ErrInvalidAutoscale), errors.Is(err, ErrInvalidPredeploy), errors.Is(err, ErrInvalidStopPolicy), errors.Is(err, ErrInvalidPathFilters):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNoBuilder):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoRegistry):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoContainer):
		http.Error(w, "app has no running container yet", http.StatusConflict)
	case errors.Is(err, ErrDeploymentNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrDeploymentRunning):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoSuchNode):
		http.Error(w, err.Error(), http.StatusNotFound)
	// Both are "not here, not yet", which is a state of this instance
	// rather than a bad request: the same call is right once the app is
	// somewhere else, or once the thing it needs exists.
	case errors.Is(err, ErrNotPlaceable), errors.Is(err, ErrRemote):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrInvalidVolumePath):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrVolumeNotFound), errors.Is(err, ErrOrphanNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrVolumeExists), errors.Is(err, ErrVolumeNeedsOneCopy),
		errors.Is(err, ErrVolumePinsApp), errors.Is(err, ErrVolumeOnWorker), errors.Is(err, ErrNoDataDir):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrInvalidTCPPort):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrTCPPortNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrTCPPortExists), errors.Is(err, ErrTCPPortTaken), errors.Is(err, ErrNoTCPPortsLeft),
		errors.Is(err, ErrTCPNeedsOneCopy), errors.Is(err, ErrTCPPinsApp):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		project.WriteError(w, err)
	}
}
