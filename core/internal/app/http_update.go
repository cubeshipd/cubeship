package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Project     string `json:"project"`
		Environment string `json:"environment"`
		Source      string `json:"source"`
		// Image is where an external app pulls from, without a tag.
		Image string `json:"image"`
		// Tag is which one to run. Empty follows the registry: on this
		// instance's own that means a push deploys the app, and on any
		// other it means `latest`.
		Tag string `json:"tag"`
		// Repo, Ref and Dockerfile are where a building app builds from.
		Repo             string            `json:"repo"`
		Ref              string            `json:"ref"`
		Dockerfile       string            `json:"dockerfile"`
		PredeployCommand *Predeploy        `json:"predeploy_command"`
		PredeployTimeout *int64            `json:"predeploy_timeout"`
		StopGracePeriod  *int64            `json:"stop_grace_period"`
		StopSignal       *string           `json:"stop_signal"`
		WatchPaths       *[]string         `json:"watch_paths"`
		IgnorePaths      *[]string         `json:"ignore_paths"`
		BuildArgs        map[string]string `json:"build_args"`
	}
	// The domain is not required: an app is created empty and made
	// deployable afterwards, in its own settings. Everything that says
	// *where the app is* still is.
	if err := httpx.DecodeJSON(r, &req); err != nil ||
		req.Name == "" || req.Project == "" {
		http.Error(w, "name and project are required", http.StatusBadRequest)
		return
	}
	var predeploy *Predeploy
	if req.PredeployCommand != nil || req.PredeployTimeout != nil {
		p := Predeploy{}
		if req.PredeployCommand != nil {
			p = *req.PredeployCommand
		}
		if req.PredeployTimeout != nil {
			p.Timeout = time.Duration(*req.PredeployTimeout) * time.Second
		}
		predeploy = &p
	}
	created, err := h.svc.CreateWithPredeploy(r.Context(), user.FromContext(r.Context()),
		req.Project, req.Environment, req.Name, Source(req.Source),
		Origin{Image: req.Image, Tag: req.Tag, Repo: req.Repo, Ref: req.Ref, Dockerfile: req.Dockerfile}, predeploy)
	if err != nil {
		WriteError(w, err)
		return
	}
	if req.WatchPaths != nil || req.IgnorePaths != nil {
		watch, ignore := []string(nil), []string(nil)
		if req.WatchPaths != nil {
			watch = *req.WatchPaths
		}
		if req.IgnorePaths != nil {
			ignore = *req.IgnorePaths
		}
		created, err = h.svc.UpdatePathFilters(r.Context(), user.FromContext(r.Context()), Reference{Project: req.Project, Environment: req.Environment, Name: req.Name}, watch, ignore)
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	if req.BuildArgs != nil {
		created, err = h.svc.UpdateBuildArgs(r.Context(), user.FromContext(r.Context()), Reference{Project: req.Project, Environment: req.Environment, Name: req.Name}, req.BuildArgs)
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	if req.StopGracePeriod != nil || req.StopSignal != nil {
		grace := DefaultStopGracePeriod
		signal := DefaultStopSignal
		if req.StopGracePeriod != nil {
			grace = time.Duration(*req.StopGracePeriod) * time.Second
		}
		if req.StopSignal != nil {
			signal = *req.StopSignal
		}
		created, err = h.svc.UpdateStopPolicy(r.Context(), user.FromContext(r.Context()), Reference{Project: req.Project, Environment: req.Environment, Name: req.Name}, grace, signal)
		if err != nil {
			WriteError(w, err)
			return
		}
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
		Source     *string `json:"source"`
		Image      *string `json:"image"`
		Tag        *string `json:"tag"`
		Repo       *string `json:"repo"`
		Ref        *string `json:"ref"`
		Dockerfile *string `json:"dockerfile"`
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
		Autoscale        *Autoscale        `json:"autoscale"`
		PredeployCommand *Predeploy        `json:"predeploy_command"`
		PredeployTimeout *int64            `json:"predeploy_timeout"`
		StopGracePeriod  *int64            `json:"stop_grace_period"`
		StopSignal       *string           `json:"stop_signal"`
		WatchPaths       *[]string         `json:"watch_paths"`
		IgnorePaths      *[]string         `json:"ignore_paths"`
		BuildArgs        map[string]string `json:"build_args"`
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
	if req.Image != nil || req.Tag != nil || req.Repo != nil || req.Ref != nil || req.Dockerfile != nil {
		origin = &Origin{
			Image:      deref(req.Image),
			Tag:        deref(req.Tag),
			Repo:       deref(req.Repo),
			Ref:        deref(req.Ref),
			Dockerfile: deref(req.Dockerfile),
		}
	}
	var predeploy *Predeploy
	if req.PredeployCommand != nil || req.PredeployTimeout != nil {
		current, lookupErr := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), refFrom(r), user.LevelManage)
		if lookupErr != nil {
			WriteError(w, lookupErr)
			return
		}
		p := current.Predeploy
		if req.PredeployCommand != nil {
			p = *req.PredeployCommand
			p.Timeout = current.Predeploy.Timeout
		}
		if req.PredeployTimeout != nil {
			p.Timeout = time.Duration(*req.PredeployTimeout) * time.Second
		}
		predeploy = &p
	}
	if source == nil && origin == nil &&
		req.Node == nil && req.Nodes == nil && req.HealthPath == nil && req.Scale == nil &&
		req.Limits == nil && req.Spread == nil && req.Autoscale == nil && predeploy == nil && req.StopGracePeriod == nil && req.StopSignal == nil && req.WatchPaths == nil && req.IgnorePaths == nil && req.BuildArgs == nil {
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

	updated, err := h.svc.UpdateWithPredeploy(r.Context(), user.FromContext(r.Context()), refFrom(r),
		source, origin, req.HealthPath, req.Limits, req.Autoscale, place, predeploy)
	if err != nil {
		WriteError(w, err)
		return
	}
	if req.WatchPaths != nil || req.IgnorePaths != nil {
		current, lookupErr := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), refFrom(r), user.LevelManage)
		if lookupErr != nil {
			WriteError(w, lookupErr)
			return
		}
		watch, ignore := current.WatchPaths, current.IgnorePaths
		if req.WatchPaths != nil {
			watch = *req.WatchPaths
		}
		if req.IgnorePaths != nil {
			ignore = *req.IgnorePaths
		}
		updated, err = h.svc.UpdatePathFilters(r.Context(), user.FromContext(r.Context()), refFrom(r), watch, ignore)
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	if req.BuildArgs != nil {
		updated, err = h.svc.UpdateBuildArgs(r.Context(), user.FromContext(r.Context()), refFrom(r), req.BuildArgs)
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	if req.StopGracePeriod != nil || req.StopSignal != nil {
		current, lookupErr := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), refFrom(r), user.LevelManage)
		if lookupErr != nil {
			WriteError(w, lookupErr)
			return
		}
		grace, signal := current.StopGracePeriod, current.StopSignal
		if req.StopGracePeriod != nil {
			grace = time.Duration(*req.StopGracePeriod) * time.Second
		}
		if req.StopSignal != nil {
			signal = *req.StopSignal
		}
		updated, err = h.svc.UpdateStopPolicy(r.Context(), user.FromContext(r.Context()), refFrom(r), grace, signal)
		if err != nil {
			WriteError(w, err)
			return
		}
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
	a, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), refFrom(r), user.LevelView)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(a, h.svc.InstanceConfig(r.Context())))
}

// delete removes an app and the container serving it. Requires the
// member role — the same level that can deploy it.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	deleteData := r.URL.Query().Get("delete_volume_data") == "true"
	if _, err := h.svc.DeleteApp(r.Context(), user.FromContext(r.Context()), refFrom(r), deleteData); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// DeploymentResponse is one deploy attempt, as the API reports it.
type DeploymentResponse struct {
	ID             int64  `json:"id"`
	Status         string `json:"status"`
	GitSHA         string `json:"git_sha,omitempty"`
	GitBranch      string `json:"git_branch,omitempty"`
	Skipped        bool   `json:"skipped,omitempty"`
	Image          string `json:"image"`
	Error          string `json:"error,omitempty"`
	Phase          string `json:"phase"`
	DurationMillis int64  `json:"duration_ms"`
	Cancelled      bool   `json:"cancelled"`
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
		ID: d.ID, Status: d.Status, Skipped: d.Status == DeploymentSkipped, GitSHA: d.GitSHA, GitBranch: d.GitBranch, Image: d.ImageRef, Error: d.Error,
		Phase: d.Phase, DurationMillis: d.Duration().Milliseconds(), Cancelled: d.Cancelled,
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

func (h *Handler) cancelDeployment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid deployment id", http.StatusBadRequest)
		return
	}
	if err := h.svc.CancelDeployment(r.Context(), user.FromContext(r.Context()), refFrom(r), id); err != nil {
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
