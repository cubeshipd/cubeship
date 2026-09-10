package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/user"

	"github.com/docker/docker/pkg/stdcopy"
)

// DefaultLogTail is how much of an app's log the API returns when the
// caller doesn't ask for a specific amount. A container that has been up
// for weeks holds far more output than anyone wants streamed at them, and
// the recent lines are the ones that explain what is happening now.
const DefaultLogTail = "500"

// Response is one app as both the API and the MCP tools report it.
type Response struct {
	// Reference is the app's canonical identifier,
	// org/project/environment/name — also its registry repository path.
	Reference   string `json:"reference"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Domains are every name this app answers at, each with the port
	// behind it.
	Domains []DomainResponse `json:"domains"`
	// Image is the image this app is about, which depends on where it
	// comes from: for a registry app, where a push should go — empty
	// while the instance has no domain, because there is nowhere to push
	// yet — and for an external app, what it pulls.
	Image  string `json:"image,omitempty"`
	Status string `json:"status"`
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
	HealthPath string `json:"health_path,omitempty"`
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
	r := Response{
		Reference: ref.String(),
		Name:      a.Name, Description: a.Description, Domains: toDomains(a.Domains),
		Status: a.Status(), HasContainer: a.HasContainer(), Source: a.Source,
		Project: a.ProjectSlug, Environment: a.EnvironmentSlug,
		SuggestedHost: SuggestedHostFor(ref, in.Domain),
		Nodes:         a.Nodes(),
		Scale:         len(a.Replicas),
		HealthPath:    a.HealthPath,
		Spread:        a.Spread,
		Limits:        a.Limits,
		Autoscale:     a.Autoscale,
		Replicas:      toReplicas(a),
		Split:         a.Split(),
		Address:       in.PublicIP,
	}
	switch Source(a.Source) {
	case SourceExternal:
		r.Image = a.SourceImage
	case SourceDockerfile, SourceRailpack:
		r.Repo, r.Ref, r.Dockerfile = a.SourceRepo, a.SourceRef, a.SourceDockerfile
	default:
		if in.RegistryHost != "" {
			r.Image = ref.ImageFor(in.RegistryHost)
		}
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
	r.Handle("POST "+appPath+"/deploy", auth(http.HandlerFunc(h.deploy)))
	r.Handle("GET "+appPath+"/deployments", auth(http.HandlerFunc(h.deployments)))
	r.Handle("GET "+appPath+"/deployments/{id}", auth(http.HandlerFunc(h.deployment)))
	r.Handle("DELETE "+appPath+"/deployments/{id}", auth(http.HandlerFunc(h.deleteDeployment)))
	r.Handle("POST "+appPath+"/domains", auth(http.HandlerFunc(h.addDomain)))
	r.Handle("PATCH "+appPath+"/domains/{domainID}", auth(http.HandlerFunc(h.setDomainPort)))
	r.Handle("DELETE "+appPath+"/domains/{domainID}", auth(http.HandlerFunc(h.removeDomain)))
	r.Handle("GET "+appPath+"/env", auth(http.HandlerFunc(h.getEnv)))
	r.Handle("PUT "+appPath+"/env", auth(http.HandlerFunc(h.setEnv)))
	r.Handle("PATCH "+appPath+"/env", auth(http.HandlerFunc(h.mergeEnv)))
	r.Handle("GET "+appPath+"/logs", auth(http.HandlerFunc(h.logs)))
	r.Handle("GET "+appPath+"/metrics", auth(http.HandlerFunc(h.metrics)))
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
// through to project's (and so to org's) for what it re-raises.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrAlreadyExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrUnknownSource), errors.Is(err, ErrImageRequired),
		errors.Is(err, ErrImageNotAllowed), errors.Is(err, ErrImageCarriesTag),
		errors.Is(err, ErrRepoRequired), errors.Is(err, ErrRepoNotAllowed),
		errors.Is(err, ErrRepoNotSupported), errors.Is(err, ErrDockerfileNotAllowed):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrBadHost), errors.Is(err, ErrHostIsTheInstance),
		errors.Is(err, ErrHostRequired), errors.Is(err, ErrInvalidHealthPath),
		errors.Is(err, ErrInvalidLimits), errors.Is(err, ErrInvalidAutoscale):
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
	default:
		project.WriteError(w, err)
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Project     string `json:"project"`
		Environment string `json:"environment"`
		Source      string `json:"source"`
		// Image is where an external app pulls from, without a tag.
		Image string `json:"image"`
		// Repo, Ref and Dockerfile are where a building app builds from.
		Repo       string `json:"repo"`
		Ref        string `json:"ref"`
		Dockerfile string `json:"dockerfile"`
	}
	// The domain is not required: an app is created empty and made
	// deployable afterwards, in its own settings. Everything that says
	// *where the app is* still is.
	if err := httpx.DecodeJSON(r, &req); err != nil ||
		req.Name == "" || req.Project == "" {
		http.Error(w, "name and project are required", http.StatusBadRequest)
		return
	}
	created, err := h.svc.Create(r.Context(), user.FromContext(r.Context()),
		req.Project, req.Environment, req.Name, req.Description, Source(req.Source),
		Origin{Image: req.Image, Repo: req.Repo, Ref: req.Ref, Dockerfile: req.Dockerfile})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created, h.svc.InstanceConfig(r.Context())))
}

// update is PATCH: a field left out of the body is left alone, so one
// section of the settings screen can be saved without sending the rest.
//
// The source and its origin fields are one field group, not four
// independent ones — naming a source without the settings it needs, or
// settings the new source would ignore, is what the service refuses.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description *string `json:"description"`
		Source      *string `json:"source"`
		Image       *string `json:"image"`
		Repo        *string `json:"repo"`
		Ref         *string `json:"ref"`
		Dockerfile  *string `json:"dockerfile"`
		// Node is which machine serves this app's names, and Nodes are
		// the machines it runs on. Their own fields rather than part of
		// the source group: where an app runs and what it runs are
		// different decisions, and so are where it runs and where its
		// traffic arrives.
		//
		// Sending `nodes` without `node` keeps the edge it has when
		// that machine is still in the set, so scaling an app out does
		// not silently move its DNS record.
		Node  *string   `json:"node"`
		Nodes *[]string `json:"nodes"`
		// Scale is how many copies run in total, spread over those
		// machines. Left out keeps however many it has, so adding a
		// machine does not silently change the count.
		Scale *int `json:"scale"`
		// Spread makes the app follow the cluster: run on every machine
		// there is, now and whenever one is added. Sending `nodes`
		// alongside it turns it off — naming machines is choosing by
		// hand, which is what this exists to stop being necessary.
		Spread *bool `json:"spread"`
		// HealthPath is what Traefik asks this app for to decide
		// whether a container behind one of its names is worth
		// traffic. Its own field for the same reason the placement is:
		// it is neither what the app is nor where it runs.
		HealthPath *string `json:"health_path"`
		// Limits is what one copy of this app may take from the
		// machine it runs on. Sent as an object so that clearing a
		// ceiling is a value rather than a gap: `{"cpu":0}` removes
		// the CPU limit, and leaving the field out entirely is what
		// keeps whatever is there.
		Limits *Limits `json:"limits"`
		// Autoscale is when this instance decides the replica count
		// for itself. Sent as an object, and `max: 0` is how it is
		// turned off — there is no separate flag to disagree with.
		Autoscale *Autoscale `json:"autoscale"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	var source *Source
	if req.Source != nil {
		s := Source(*req.Source)
		source = &s
	}
	var origin *Origin
	if req.Image != nil || req.Repo != nil || req.Ref != nil || req.Dockerfile != nil {
		origin = &Origin{
			Image:      deref(req.Image),
			Repo:       deref(req.Repo),
			Ref:        deref(req.Ref),
			Dockerfile: deref(req.Dockerfile),
		}
	}
	if req.Description == nil && source == nil && origin == nil &&
		req.Node == nil && req.Nodes == nil && req.HealthPath == nil && req.Scale == nil &&
		req.Limits == nil && req.Spread == nil && req.Autoscale == nil {
		http.Error(w, "nothing to change", http.StatusBadRequest)
		return
	}

	var place *Placement
	if req.Node != nil || req.Nodes != nil || req.Scale != nil || req.Spread != nil {
		place = &Placement{Spread: req.Spread}
		if req.Scale != nil {
			place.Replicas = *req.Scale
		}
		switch {
		case req.Nodes != nil:
			place.Nodes = *req.Nodes
		case req.Node != nil:
			// `node` alone is the whole placement: put it there and
			// serve it from there. It is what one machine meant before
			// there was more than one, and it is still the shortest way
			// to say "move this app".
			place.Nodes = []string{*req.Node}
		}
		// And neither, which is `scale` on its own: the machines
		// stay as they are and the new count is spread over them.
		// Scaling up and scaling out are separate acts.
	}

	updated, err := h.svc.Update(r.Context(), user.FromContext(r.Context()), refFrom(r),
		req.Description, source, origin, req.HealthPath, req.Limits, req.Autoscale, place)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, h.svc.InstanceConfig(r.Context())))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	apps, err := h.svc.List(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(apps, h.svc.InstanceConfig(r.Context())))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), refFrom(r), orgRoleMember)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(a, h.svc.InstanceConfig(r.Context())))
}

// delete removes an app and the container serving it. Requires the
// member role — the same level that can deploy it.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if _, err := h.svc.Delete(r.Context(), user.FromContext(r.Context()), refFrom(r)); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// DeploymentResponse is one deploy attempt, as the API reports it.
type DeploymentResponse struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Image  string `json:"image"`
	Error  string `json:"error,omitempty"`
	// Logs is what a build printed. Absent for a source that only
	// pulls — and absent from a *listing* whatever the deploy printed,
	// because a build's output is capped at 256 KiB and a history is
	// fifty rows. Read one deployment to get it.
	Logs string `json:"logs,omitempty"`
	// HasLogs says there is output to read, which is what a listing can
	// answer without carrying it.
	HasLogs bool `json:"has_logs"`
	// Deletable says this record may be removed. Only a deploy still
	// running may not be — and one that is stalled counts as not
	// running, because nothing is writing to it any more.
	Deletable bool `json:"deletable"`
	// StalledOn is the machines this deploy is waiting for that have
	// stopped answering. Absent on every deploy that is not waiting on
	// one, which is almost all of them.
	//
	// The status stays `pending`, deliberately: a machine that comes
	// back picks a pending deploy up and finishes the rollout it
	// missed. This says who everybody is waiting for, so a screen can
	// name them rather than show a spinner that never ends.
	StalledOn []string `json:"stalled_on,omitempty"`
	// Live says the app is running this deploy — so deleting it takes
	// the app down, which is why it is reported apart from Deletable.
	Live      bool      `json:"live"`
	CreatedAt time.Time `json:"created_at"`
}

func toDeploymentResponse(d *Deployment) DeploymentResponse {
	return DeploymentResponse{
		ID: d.ID, Status: d.Status, Image: d.ImageRef, Error: d.Error,
		Logs: d.Logs, HasLogs: d.HasLogs, Deletable: d.Deletable, Live: d.Live,
		StalledOn: stalledOn(d),
		CreatedAt: d.CreatedAt,
	}
}

func stalledOn(d *Deployment) []string {
	if d.Stalled == nil {
		return nil
	}
	return d.Stalled.Waiting
}

// deploy accepts a redeploy and answers 202 with the deployment that
// records it. The work runs detached, so hanging up here does not stop
// it — poll the deployment to find out how it went.
func (h *Handler) deploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tag string `json:"tag"`
	}
	// An empty or absent body is fine; Tag stays "" and defaults later.
	_ = httpx.DecodeJSON(r, &req)

	_, deployment, err := h.svc.Deploy(r.Context(), user.FromContext(r.Context()), refFrom(r), req.Tag)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, toDeploymentResponse(deployment))
}

// deleteDeployment removes one deploy's record, and the container it
// produced when that deploy is the one the app is running. See
// Service.DeleteDeployment.
func (h *Handler) deleteDeployment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid deployment id", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if err := h.svc.DeleteDeployment(ctx, user.FromContext(ctx), refFrom(r), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deployments(w http.ResponseWriter, r *http.Request) {
	history, err := h.svc.Deployments(r.Context(), user.FromContext(r.Context()), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]DeploymentResponse, 0, len(history))
	for _, d := range history {
		out = append(out, toDeploymentResponse(d))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// deployment reads one deploy. `?wait=true` holds the response open
// until it finishes, which is what a CLI wants — but the deploy runs
// regardless of whether anyone waits.
func (h *Handler) deployment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid deployment id", http.StatusBadRequest)
		return
	}

	ctx, caller, ref := r.Context(), user.FromContext(r.Context()), refFrom(r)

	var d *Deployment
	if r.URL.Query().Get("wait") == "true" {
		d, err = h.svc.WaitForDeployment(ctx, caller, ref, id)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			// The wait ran out, not the deploy. Hand back where it had
			// got to and let the caller ask again.
			httpx.WriteJSON(w, http.StatusOK, toDeploymentResponse(d))
			return
		}
	} else {
		d, err = h.svc.Deployment(ctx, caller, ref, id)
	}
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDeploymentResponse(d))
}

// EnvResponse is what reading an app's variables returns: the ones set
// on the app itself, and the full set its container runs with, each
// value labelled with the level it came from.
type EnvResponse struct {
	Vars      envvar.Map        `json:"vars"`
	Effective []envvar.Resolved `json:"effective"`
}

func (h *Handler) getEnv(w http.ResponseWriter, r *http.Request) {
	own, effective, err := h.svc.Env(r.Context(), user.FromContext(r.Context()), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, EnvResponse{Vars: own, Effective: effective})
}

// MergeEnvRequest adds or overwrites the variables in set and removes
// those named in unset. Anything not mentioned is left alone — which is
// the difference between this and PUT.
type MergeEnvRequest struct {
	Set   envvar.Map `json:"set"`
	Unset []string   `json:"unset"`
}

func (h *Handler) mergeEnv(w http.ResponseWriter, r *http.Request) {
	var req MergeEnvRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.MergeEnv(r.Context(), user.FromContext(r.Context()),
		refFrom(r), req.Set, req.Unset); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) setEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Vars envvar.Map `json:"vars"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.SetEnv(r.Context(), user.FromContext(r.Context()), refFrom(r), req.Vars); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// metrics is what this app's container has been using. The series
// itself is metrics' to render — see metrics.WriteSeries — and what
// this adds is the one thing that package must not decide: who is
// allowed to look.
func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), refFrom(r), user.RoleMember)
	if err != nil {
		WriteError(w, err)
		return
	}
	metrics.WriteSeries(w, r, h.svc.Metrics(), metrics.KindApp, a.ID, a.HasContainer())
}

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = DefaultLogTail
	}

	// A log belongs to one container and so to one machine. `server`
	// names which; leaving it off gets the machine the app's traffic
	// arrives at, which is the one somebody looking at a name means.
	server := r.URL.Query().Get("server")

	rc, err := h.svc.Logs(r.Context(), user.FromContext(r.Context()), refFrom(r), server, tail)
	if err != nil {
		WriteError(w, err)
		return
	}
	defer rc.Close()

	w.WriteHeader(http.StatusOK)
	// Containers are created without a TTY, so the Engine returns stdout
	// and stderr multiplexed behind an 8-byte binary frame header per
	// chunk. Copying that straight through prints binary garbage between
	// the log lines — demultiplex it first.
	if _, err := stdcopy.StdCopy(w, w, rc); err != nil {
		// The status line is already sent; all we can do is record it.
		log.Printf("logs for app %s: %v", refFrom(r), err)
	}
}

// DomainResponse is one name an app answers at.
type DomainResponse struct {
	ID   int64  `json:"id"`
	Host string `json:"host"`
	// Port is what this name reaches, or 0 for "read it from the
	// image". Zero is the normal answer.
	Port int `json:"port"`
}

func toDomains(domains []Domain) []DomainResponse {
	out := make([]DomainResponse, 0, len(domains))
	for _, d := range domains {
		out = append(out, DomainResponse{ID: d.ID, Host: d.Host, Port: d.Port})
	}
	return out
}

func (h *Handler) addDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
		// Omitted or 0 means DefaultPort.
		Port int `json:"port"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	updated, err := h.svc.AddDomain(ctx, user.FromContext(ctx), refFrom(r), req.Host, req.Port)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(updated, h.svc.InstanceConfig(ctx)))
}

func (h *Handler) setDomainPort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port int `json:"port"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, ok := domainIDFrom(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	updated, err := h.svc.SetDomainPort(ctx, user.FromContext(ctx), refFrom(r), id, req.Port)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, h.svc.InstanceConfig(ctx)))
}

func (h *Handler) removeDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := domainIDFrom(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	updated, err := h.svc.RemoveDomain(ctx, user.FromContext(ctx), refFrom(r), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, h.svc.InstanceConfig(ctx)))
}

// domainIDFrom reads the domain's id, answering 404 for anything that is
// not one: a path segment that is not a number names nothing, and that
// is the same answer as naming something that does not exist.
func domainIDFrom(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("domainID"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return 0, false
	}
	return id, true
}
