package app

import (
	"maps"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/openapi"
	"cubeship/internal/project"
)

func predeployCommandSchema(description string) *openapi.Schema {
	return &openapi.Schema{Description: description, OneOf: []*openapi.Schema{
		{Type: "string"},
		{Type: "array", Items: openapi.String("argv element")},
	}}
}

// mergeSchemas folds b into a and returns a.
func mergeSchemas(a, b map[string]*openapi.Schema) map[string]*openapi.Schema {
	maps.Copy(a, b)
	return a
}

func (h *Handler) OpenAPI() openapi.Spec {
	// An app is addressed by all three parts of its reference, because a
	// name is only unique within its environment.
	refParams := []openapi.Parameter{
		openapi.PathParam("project", "The project's slug."),
		openapi.PathParam("env", "The environment's slug."),
		openapi.PathParam("name", "The app's name, unique within that environment."),
	}
	const appPath = "/apps/{project}/{env}/{name}"

	// Writing an app's environment takes the role its source takes to
	// deploy, which is not the same answer for every app.
	const envRole = "\n\nRequires the member role — or the admin role if the app **builds**: for a building source the environment is build input as well as the container's, and Railpack turns `RAILPACK_INSTALL_CMD`, `RAILPACK_BUILD_CMD` and `RAILPACK_START_CMD` into commands the build runs on this host."

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Apps",
			Description: "A deployable service. Pushing a tag to an app's registry path is what deploys it; these endpoints register apps, configure them, and redeploy on demand.",
		}},
		Schemas: mergeSchemas(mergeSchemas(project.EnvSchemas(), metrics.Schemas()), map[string]*openapi.Schema{
			"Deployment": openapi.Object(map[string]*openapi.Schema{
				"id":          openapi.Integer("Poll this deploy at .../deployments/{id}."),
				"status":      {Type: "string", Enum: []string{"pending", "succeeded", "failed", "skipped"}},
				"git_sha":     openapi.String("Git commit SHA supplied by a push webhook or resolved from the repository for a manual Git deploy."),
				"git_branch":  openapi.String("Git branch or ref used for the deploy, when it builds from a repository."),
				"phase":       {Type: "string", Description: "Current deploy phase: queued, resolving, pulling, predeploy, swapping, waiting, done, failed or cancelled."},
				"duration_ms": openapi.Integer("Elapsed deploy duration in milliseconds."),
				"cancelled":   openapi.Bool("Whether the deploy was cancelled before the swap."),
				"has_logs":    openapi.Bool("Whether this deploy printed anything — a build does, a pull does not. A listing answers this and does not carry the output; read one deployment for that."),
				"deletable":   openapi.Bool("Whether this record may be removed. False only for a deploy still running, which the daemon is still writing to — a stalled one counts as not running, because nothing is writing to it any more."),
				"stalled_on":  arrayOf(openapi.String("A machine's name."), "The machines this deploy is waiting for that have stopped answering. Absent on every deploy that is not waiting on one.\n\nThe status stays `pending`, deliberately: a machine that comes back picks a pending deploy up and finishes the rollout it missed, where a failed one would send it to the deployment below this while the machines that took it stay on this one — an app running two versions with nothing saying so. What this changes is what is said, not what is run. It is also what makes the record deletable, since nothing is writing to it any more."),
				"live":        openapi.Bool("Whether the app is running this deploy. Deleting this one takes the app down — see DELETE on the deployment."),
				"image":       openapi.String("The reference the daemon pulled, or the name it gave what it built."),
				"error":       openapi.String("Why it failed, when it did."),
				"logs":        openapi.String("What the build printed, for a source that builds. Written while it runs, so polling this deployment shows a build in progress.\n\nAbsent for a source that only pulls, and **absent from a listing whatever the deploy printed**: it is capped at 256 KiB and a history is fifty rows, which is twelve megabytes fetched every couple of seconds by anything watching a build. Read one deployment for it."),
				"created_at":  {Type: "string", Format: "date-time"},
			}, "id", "status", "image", "has_logs", "created_at"),

			"App": openapi.Object(map[string]*openapi.Schema{
				"reference":         openapi.String("The app's identifier, project/environment/name — also its registry repository path."),
				"name":              openapi.String("Unique within its environment, not across the instance. Permanent."),
				"domains":           openapi.Array(openapi.Ref("AppDomain")),
				"image":             openapi.String("For a registry app, the path to push to — a push there deploys. For an external app, the image it pulls."),
				"tag":               openapi.String("The tag this app runs. Absent means it follows the registry: on this instance's own that is whatever is pushed — see `autodeploy` — and anywhere else it is `latest`."),
				"autodeploy":        openapi.Bool("Whether a push to this instance's registry deploys this app. True exactly when the app is on that registry and pinned to no tag — derived from those two, so nothing can say it is on while a tag says otherwise."),
				"status":            {Type: "string", Enum: []string{"pending", "running", "down", "degraded", "stopped", "starting"}, Description: "`stopped` means an operator intentionally stopped every replica; it stays stopped until started again. `starting` means those replicas are being resumed."},
				"has_container":     openapi.Bool("Whether a container currently backs this app, which is what decides whether there is a log to read. The status cannot answer it: an app that has never been deployed and one whose container went away both read as not running, and only the second has anything to say."),
				"source":            {Type: "string", Enum: []string{"registry", "external", "dockerfile", "railpack"}, Description: "Where this app's image comes from. \"registry\" is an image pushed to Cubeship, and the push is what deploys it; \"external\" is an image in a registry Cubeship does not run; \"dockerfile\" and \"railpack\" are built here from a Git repository, the first from a Dockerfile you wrote and the second worked out from the code. Only a push to Cubeship's own registry deploys on its own — the other two deploy when asked."},
				"repo":              openapi.String("For a building app, the Git repository it builds from."),
				"ref":               openapi.String("For a building app, the branch, tag or commit built. Empty means the repository's default branch."),
				"dockerfile":        openapi.String("For a dockerfile app, the recipe's path in that repository. Empty means \"Dockerfile\" at the root."),
				"project":           openapi.String(""),
				"environment":       openapi.String(""),
				"nodes":             arrayOf(openapi.String("A machine's name."), "The machines this app runs on. More than one is an app whose copies this instance's proxy spreads traffic across, over the cluster's private network.\n\nWhere it runs does not change where its traffic arrives: every name this instance serves arrives at the control plane, which is what makes moving an app not a DNS change and not a second certificate."),
				"health_path":       openapi.String("What Traefik asks this app for before trusting a container with traffic. Absent is no check, which is the default."),
				"predeploy_command": predeployCommandSchema("Optional shell command or argv array run from the new image before the swap."),
				"predeploy_timeout": openapi.Integer("Pre-deploy timeout in seconds, at most 3600."),
				"stop_grace_period": openapi.Integer("Seconds to wait for a container to stop, from 0 to 3600."),
				"stop_signal":       openapi.String("Signal sent when stopping a container, such as SIGTERM."),
				"watch_paths":       openapi.Array(openapi.String("Repository-relative glob.")),
				"ignore_paths":      openapi.Array(openapi.String("Repository-relative glob excluded from watched paths.")),
				"build_args":        {Type: "object", Description: "Write-only Dockerfile build arguments. Values are excluded from responses and may remain in image history, layers, or cache.", AdditionalProperties: &openapi.Schema{Type: "string"}},
				"limits":            openapi.Ref("AppLimits"),
				"autoscale":         openapi.Ref("AppAutoscale"),
				"spread":            openapi.Bool("Whether this app follows the cluster: it runs on every machine there is, and is re-spread whenever one is added or taken away. Absent on an app placed by hand, which is every app until somebody turns this on."),
				"scale":             openapi.Integer("How many copies of this app run in total, across the machines in `nodes`. One per machine is the ordinary answer.\n\nA number where `replicas` is the list: they are two shapes of the same fact, and one name for both is how a client sends an array where a count was meant."),
				"replicas": arrayOf(openapi.Ref("AppReplica"),
					"What is running on each of those machines. It is what a `degraded` status is made of: some of them serving and some not."),
				"split":          openapi.Bool("Whether the machines serving this app are not all serving the same deployment. Reported apart from `status` because the two are orthogonal — an app can be degraded and split, or running and split.\n\nEvery rollout across several machines passes through this for as long as it takes the last machine to pull, so it is a fact rather than a fault. What makes it worth reporting is the rollout that never finishes: without it, a replica running *something* reads as running, so two versions read as one healthy app."),
				"suggested_host": openapi.String("A name this app could answer at, under the instance's own domain: `<app>.<environment>.<project>.<instance domain>`. Nothing assigns it — an app with no domain is a normal app, and this is what a client offers when somebody does want one. Under a wildcard address (`settings.wildcard_domain`) it resolves the moment it is added; under a real domain it needs a record. Absent while the instance has no domain."),
				"internal_host":  openapi.String("Where another app **on this instance** reaches this one: a name every container resolves, on whichever machine the app runs, to whichever copies of it are running.\n\nIt is the app's container name without the deployment id on the end, carried as a network alias — the same shape a database (`cubeship-db-pg`) and a managed store (`cubeship-s3-media`) already have. Reach it on the port the app listens on; there is no proxy in the path, so no TLS is terminated for it and the health check does not apply.\n\n**A public name cannot do this from inside.** A request for one leaves the box for a record that points back at it, and a host that does not hairpin its own NAT answers nothing — which reads as the other app being down.\n\nAlways present, because it is derived from the reference. A container joins the alias when it is created, so an app that has not deployed since this instance was upgraded is not reachable by it until it is deployed again."),
			}, "reference", "name", "domains", "status", "has_container", "source", "project", "environment", "internal_host"),
			"AppAutoscale": openapi.Object(map[string]*openapi.Schema{
				"min": openapi.Integer("The fewest copies the instance will leave running. At least 1 while autoscaling is on."),
				"max": openapi.Integer("The most it will run. **Zero is off**, which is what every app is until somebody turns it on — there is no separate flag to disagree with it.\n\nA ceiling is not optional. Without one a loop of requests is a loop of replicas until the machine has nothing left, which is a worse outage than the one autoscaling was turned on to avoid. The largest accepted is 100, which is a typo limit rather than a resource one."),
				"cpu": {Type: "number", Description: "What each copy should be sitting at, as a percentage where **100 is one core** — the same scale every container chart on this instance is drawn on, so the number you type is the number you were looking at.\n\nCPU is the only signal, and deliberately: adding a copy does not lower any copy's memory, so a memory rule would scale up forever and never come back down."},
				"at":  {Type: "string", Format: "date-time", Description: "When the instance last changed this app's count. Absent until it has. It is what the cooldown is measured from — three minutes before another change, ten before a smaller one."},
			}, "min", "max", "cpu"),
			"AppLimits": openapi.Object(map[string]*openapi.Schema{
				"cpu":          {Type: "number", Description: "Cores, and fractional on purpose — half a core is an ordinary answer on a box this size. It is a ceiling rather than a share: a container at its limit is throttled, not merely preferred less when the machine is busy. Zero is no limit."},
				"memory_bytes": openapi.Integer("A hard ceiling in bytes. The kernel enforces it by killing the process that crosses it, which is why **lowering one below what a container is already holding kills it on the spot**. Zero is no limit."),
			}, "cpu", "memory_bytes"),
			"AppReplica": openapi.Object(map[string]*openapi.Schema{
				"node":    openapi.String("The machine, by name."),
				"status":  {Type: "string", Enum: []string{"pending", "running", "down"}, Description: "What that machine's copy is doing."},
				"serving": openapi.Bool("Whether this instance's proxy is sending traffic to this copy. One can be running and not yet a backend: a container from before this instance wrote container names down has none, and the name is the address the proxy reaches it at. Its next deploy fixes it."),
				"deploy":  openapi.Integer("Which deployment this machine is running, by id — the same id the deploy history is listed under. Absent for a machine that has been given the app and not yet run it."),
				"ordinal": openapi.Integer("Which copy of the app on this machine this is, starting at 1. Absent on the first, which is the only one an app that has never been scaled out has."),
			}, "node", "status", "serving"),
			"AppVolume": openapi.Object(map[string]*openapi.Schema{
				"id":         openapi.Integer("Identifies this volume on this app, for removing it."),
				"path":       openapi.String("Where the container sees the volume: an absolute path, cleaned."),
				"node":       openapi.String("The machine the data is on. The app runs there, as one copy, for as long as it has the volume: data does not move between machines."),
				"created_at": {Type: "string", Format: "date-time"},
			}, "id", "path", "node", "created_at"),
			"AppTCPPort": openapi.Object(map[string]*openapi.Schema{
				"id":             openapi.Integer("Identifies this port on this app, for removing it."),
				"container_port": openapi.Integer("What the app listens on inside its container."),
				"host_port":      openapi.Integer("Where the control plane publishes it: connect to the instance's address on this port."),
				"created_at":     {Type: "string", Format: "date-time"},
			}, "id", "container_port", "host_port", "created_at"),
			"VolumeOrphan": openapi.Object(map[string]*openapi.Schema{
				"id":         openapi.Integer("The id the volume had, which is also its directory's name under the data directory."),
				"app":        openapi.String("The app it belonged to. Absent when its record is missing."),
				"path":       openapi.String("Where that app's container saw it. Absent when its record is missing."),
				"created_at": {Type: "string", Format: "date-time", Description: "When the volume was created. Absent when its record is missing."},
			}, "id"),
			"AppDomain": openapi.Object(map[string]*openapi.Schema{
				"id":   openapi.Integer("Identifies this domain on this app, for changing or removing it."),
				"host": openapi.String("The name Traefik routes to this app, over HTTPS. Lowercase, without a trailing dot — which is how a browser sends one and how Traefik matches it."),
				"port": openapi.Integer("What this name reaches inside the container. Omitted or 0 falls back to the default, 8080. Nothing is read off the image: `EXPOSE` is a hint its author wrote, an image exposing several ports has no single answer, and an app that has not been built yet has no image to ask."),
			}, "id", "host", "port"),
		}),
		Paths: appPaths(refParams, envRole, appPath),
	}
}

// arrayOf is openapi.Array with something to say about the list itself.
// The items carry their own description; this is what the field means.
func arrayOf(items *openapi.Schema, description string) *openapi.Schema {
	a := openapi.Array(items)
	a.Description = description
	return a
}
