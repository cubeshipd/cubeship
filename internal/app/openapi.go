package app

import (
	"maps"
	"strconv"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/openapi"
	"cubeship/internal/project"
)

// mergeSchemas folds b into a and returns a.
func mergeSchemas(a, b map[string]*openapi.Schema) map[string]*openapi.Schema {
	maps.Copy(a, b)
	return a
}

func (h *Handler) OpenAPI() openapi.Spec {
	// An app is addressed by all four parts of its reference, because a
	// name is only unique within its environment.
	refParams := []openapi.Parameter{
		openapi.PathParam("org", "The organization's slug."),
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
				"id":         openapi.Integer("Poll this deploy at .../deployments/{id}."),
				"status":     {Type: "string", Enum: []string{"pending", "succeeded", "failed"}},
				"has_logs":   openapi.Bool("Whether this deploy printed anything — a build does, a pull does not. A listing answers this and does not carry the output; read one deployment for that."),
				"deletable":  openapi.Bool("Whether this record may be removed. False only for a deploy still running, which the daemon is still writing to — a stalled one counts as not running, because nothing is writing to it any more."),
				"stalled_on": arrayOf(openapi.String("A machine's name."), "The machines this deploy is waiting for that have stopped answering. Absent on every deploy that is not waiting on one.\n\nThe status stays `pending`, deliberately: a machine that comes back picks a pending deploy up and finishes the rollout it missed, where a failed one would send it to the deployment below this while the machines that took it stay on this one — an app running two versions with nothing saying so. What this changes is what is said, not what is run. It is also what makes the record deletable, since nothing is writing to it any more."),
				"live":       openapi.Bool("Whether the app is running this deploy. Deleting this one takes the app down — see DELETE on the deployment."),
				"image":      openapi.String("The reference the daemon pulled, or the name it gave what it built."),
				"error":      openapi.String("Why it failed, when it did."),
				"logs":       openapi.String("What the build printed, for a source that builds. Written while it runs, so polling this deployment shows a build in progress.\n\nAbsent for a source that only pulls, and **absent from a listing whatever the deploy printed**: it is capped at 256 KiB and a history is fifty rows, which is twelve megabytes fetched every couple of seconds by anything watching a build. Read one deployment for it."),
				"created_at": {Type: "string", Format: "date-time"},
			}, "id", "status", "image", "has_logs", "created_at"),

			"App": openapi.Object(map[string]*openapi.Schema{
				"reference":     openapi.String("The app's identifier, org/project/environment/name — also its registry repository path."),
				"name":          openapi.String("Unique within its environment, not across the instance. Permanent."),
				"description":   openapi.String("What this app is. Empty unless someone set it."),
				"domains":       openapi.Array(openapi.Ref("AppDomain")),
				"image":         openapi.String("For a registry app, the path to push to — a push there deploys. For an external app, the image it pulls."),
				"status":        {Type: "string", Enum: []string{"pending", "running", "down", "degraded"}, Description: "`pending` until the first image is pushed, and `degraded` when some of the machines an app runs on are serving it and some are not — which cannot happen to an app on one."},
				"has_container": openapi.Bool("Whether a container currently backs this app, which is what decides whether there is a log to read. The status cannot answer it: an app that has never been deployed and one whose container went away both read as not running, and only the second has anything to say."),
				"source":        {Type: "string", Enum: []string{"registry", "external", "dockerfile", "railpack"}, Description: "Where this app's image comes from. \"registry\" is an image pushed to Cubeship, and the push is what deploys it; \"external\" is an image in a registry Cubeship does not run; \"dockerfile\" and \"railpack\" are built here from a Git repository, the first from a Dockerfile you wrote and the second worked out from the code. Only a push to Cubeship's own registry deploys on its own — the other two deploy when asked."},
				"repo":          openapi.String("For a building app, the Git repository it builds from."),
				"ref":           openapi.String("For a building app, the branch, tag or commit built. Empty means the repository's default branch."),
				"dockerfile":    openapi.String("For a dockerfile app, the recipe's path in that repository. Empty means \"Dockerfile\" at the root."),
				"org":           openapi.String(""),
				"project":       openapi.String(""),
				"environment":   openapi.String(""),
				"node":          openapi.String("The machine whose edge serves this app's names — where its traffic arrives, and what a DNS record for it points at. On an instance of one box it is always `control-plane`."),
				"nodes":         arrayOf(openapi.String("A machine's name."), "The machines this app runs on. Always includes `node`. More than one is an app whose edge spreads its traffic across them, over the cluster's private network."),
				"health_path":   openapi.String("What Traefik asks this app for before trusting a container with traffic. Absent is no check, which is the default."),
				"replicas": arrayOf(openapi.Ref("AppReplica"),
					"What is running on each of those machines. It is what a `degraded` status is made of: some of them serving and some not."),
				"split":          openapi.Bool("Whether the machines serving this app are not all serving the same deployment. Reported apart from `status` because the two are orthogonal — an app can be degraded and split, or running and split.\n\nEvery rollout across several machines passes through this for as long as it takes the last machine to pull, so it is a fact rather than a fault. What makes it worth reporting is the rollout that never finishes: without it, a replica running *something* reads as running, so two versions read as one healthy app."),
				"suggested_host": openapi.String("A name this app could answer at, under the instance's own domain: `<app>.<environment>.<project>.<instance domain>`. Nothing assigns it — an app with no domain is a normal app, and this is what a client offers when somebody does want one. Under a wildcard address (`settings.wildcard_domain`) it resolves the moment it is added; under a real domain it needs a record. Absent while the instance has no domain."),
			}, "reference", "name", "description", "domains", "status", "has_container", "source", "org", "project", "environment"),
			"AppReplica": openapi.Object(map[string]*openapi.Schema{
				"node":    openapi.String("The machine, by name."),
				"status":  {Type: "string", Enum: []string{"pending", "running", "down"}, Description: "What that machine's copy is doing."},
				"serving": openapi.Bool("Whether the edge is sending traffic here. A replica can be running and not yet a backend: an app that gained a second machine before this release has containers whose names were never written down, and a name is the address the edge reaches a replica at. Its next deploy fixes it."),
				"deploy":  openapi.Integer("Which deployment this machine is running, by id — the same id the deploy history is listed under. Absent for a machine that has been given the app and not yet run it."),
			}, "node", "status", "serving"),
			"AppDomain": openapi.Object(map[string]*openapi.Schema{
				"id":   openapi.Integer("Identifies this domain on this app, for changing or removing it."),
				"host": openapi.String("The name Traefik routes to this app, over HTTPS. Lowercase, without a trailing dot — which is how a browser sends one and how Traefik matches it."),
				"port": openapi.Integer("What this name reaches inside the container. Omitted or 0 falls back to the default, 8080. Nothing is read off the image: `EXPOSE` is a hint its author wrote, an image exposing several ports has no single answer, and an app that has not been built yet has no image to ask."),
			}, "id", "host", "port"),
		}),
		Paths: map[string]openapi.PathItem{
			"/apps": {
				"post": {
					OperationID: "createApp",
					Summary:     "Register an app",
					Description: "Returns the registry path to push to. Nothing is deployed until an image lands there.\n\nThe name only has to be unique within its environment, so the same app can exist in `production` and `staging` at once — they get different registry paths and different containers.\n\nApp containers are expected to listen on port 8080. Requires the member role in the organization.",
					Tags:        []string{"Apps"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"name":        openapi.String("Lowercase letters, digits and dashes — it becomes a path component of the registry image. Permanent."),
						"description": openapi.String("What this app is. Optional."),
						"org":         openapi.String("Organization slug."),
						"project":     openapi.String("Project slug."),
						"environment": openapi.String(`Environment slug. Defaults to "production".`),
						"source":      {Type: "string", Enum: []string{"registry", "external", "dockerfile", "railpack"}, Description: `Where the image comes from. "registry" (the default) is an image you push to Cubeship, and the push deploys it. "external" is an image in a registry Cubeship does not run. "dockerfile" builds a Dockerfile from a Git repository; "railpack" builds from a Git repository with no Dockerfile at all, working out how from the code. Both builds need the admin role — a build runs whatever the repository contains, on this host.`},
						"image":       openapi.String(`Required for an external app, and refused for any other: the image it pulls, without a tag — e.g. "registry.digitalocean.com/acme/api". The tag is chosen per deploy, so an image pinned to one here could never be told to run another. A private registry needs a login for its host under the organization's registries.`),
						"repo":        openapi.String(`Required for a building app, and refused for any other: the Git repository to build. An https://, http:// or git:// URL — ssh needs a key this instance does not have. Only https authenticates what comes back, and a build runs whatever comes back, so use it for anything reachable from the internet. Do not put a "#ref" on it; the ref is its own field.`),
						"ref":         openapi.String(`For a building app: the branch, tag or commit to build. Defaults to the repository's default branch, and a deploy can name a different one.`),
						"dockerfile":  openapi.String(`For a dockerfile app only: the recipe's path within the repository. Defaults to "Dockerfile" at the root. Refused for railpack, which works the build out itself.`),
					}, "name", "org", "project")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The registered app, including its push path.", openapi.Ref("App")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("The organization, project or environment does not exist, or is not yours."),
						"409": openapi.TextResponse("An app with that name already exists."),
					},
				},
				"get": {
					OperationID: "listApps",
					Summary:     "List apps",
					Description: "Every app in every organization you belong to — or all of them, if you are a super-admin.",
					Tags:        []string{"Apps"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("Apps you can see.", openapi.Array(openapi.Ref("App"))),
						"401": openapi.Unauthorized,
					},
				},
			},
			appPath: {
				"patch": {
					OperationID: "updateApp",
					Summary:     "Reconfigure an app",
					Description: "Changes the description, the domain, and where the image comes from. **A field you leave out is left as it was.**\n\nAn app is created with almost none of this, so this is what makes one deployable. The source and its settings are judged together: naming a source without what it needs, or settings the source would ignore, is refused the same way it is at creation. Moving an app to a source that builds requires the admin role, because it decides that this instance will execute whatever that repository contains. The app's name is not editable.",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
					RequestBody: &openapi.RequestBody{
						Required:    true,
						Description: "Any of the fields below. Omit one to leave it alone.",
						Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
							"description": openapi.String("May be empty."),
							"source":      {Type: "string", Enum: []string{"registry", "external", "dockerfile", "railpack"}, Description: "Send the settings the new source needs alongside it."},
							"image":       openapi.String("For an external app: the image it pulls, without a tag."),
							"repo":        openapi.String("For a building app: the https://, http:// or git:// repository to build."),
							"ref":         openapi.String("For a building app: the branch, tag or commit to build."),
							"dockerfile":  openapi.String("For a dockerfile app only: the recipe's path within the repository."),
							"health_path": openapi.String("The path Traefik asks this app for before trusting a container with traffic, e.g. `/healthz`. It has to start with `/` and hold only what a URL path may — it is interpolated into a proxy's configuration.\n\nAn empty string checks nothing, which is the default and what every app starts as. **A path the app does not answer 2xx or 3xx on takes every replica out of rotation at once**, which is a name answering 503 rather than a name degrading, so this is opted into by somebody who knows what the app answers.\n\nFor an app on several machines it is also what takes one replica out and leaves the rest serving. A dead replica needs no check to be routed around — the edge retries against the next one — but a replica that is up and broken answers the connection, so nothing but a check catches it."),
							"node":        openapi.String("Which machine serves this app's names, by name — where its traffic arrives. Sent alone it is the whole placement: run the app there and serve it from there.\n\nOne machine rather than all of them, and the reason is the certificate: a machine that routes a name asks Let's Encrypt for it, and one the name does not resolve to fails that challenge forever while spending a limit shared with everyone else under that domain."),
							"nodes":       arrayOf(openapi.String("A machine's name."), "Which machines this app runs on. More than one puts its edge in front of all of them, round-robin, over the cluster's private network — which is what makes a name survive one of those machines going away.\n\nSent without `node`, the edge stays where it is when that machine is still in the set, so scaling an app out does not silently move its DNS record. Moving it takes effect on each machine's next pass: the new one starts the app before the old one stops it, so a move that fails is not an outage.\n\nOne refusal, and it is a thing that would otherwise not work in a way nobody would notice: an app that **builds** cannot leave the control plane on an instance with no domain, because a build only reaches another machine through this instance's own registry and the registry follows the domain."),
						})),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The app as it now stands.", openapi.Ref("App")),
						"400": openapi.TextResponse("Nothing to change, a source the daemon cannot act on, or a health check path that is not one."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such app, or no server of that name is in this cluster."),
						"409": openapi.TextResponse("This app cannot run on another machine: it is built here and this instance has no registry to push to, or the machine named to serve it is not one of the machines it runs on."),
					},
				},
				"get": {
					OperationID: "getApp",
					Summary:     "Get one app",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The app.", openapi.Ref("App")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "deleteApp",
					Summary:     "Delete an app",
					Description: "Stops and removes the container serving the app, then deletes it. **This cannot be undone.**\n\nImages already pushed stay in the registry; reclaiming that disk needs a registry garbage collection pass, which is a separate operation. Requires the member role.",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
					Responses: openapi.Responses{
						"200": openapi.Empty("The app is gone and its container is stopped."),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
						"500": openapi.TextResponse("The container could not be stopped, so the app was left in place rather than orphaning it."),
					},
				},
			},
			appPath + "/deploy": {
				"post": {
					OperationID: "deployApp",
					Summary:     "Redeploy an app",
					Description: "Deploys a tag already pushed to the app's registry path. Which image that tag names is the app's source's answer; the daemon pulls it, starts a container, waits for it to look healthy, and only then retires the previous one — so a bad image never takes down a working app.\n\n**Returns immediately.** The deploy runs detached from this request, so hanging up does not stop it. Poll the returned deployment — `GET .../deployments/{id}?wait=true` holds the response open until it finishes — to find out how it went.",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
					RequestBody: &openapi.RequestBody{
						Description: `Optional. Omit the body to deploy "latest", or, for a source that builds, its stored ref.`,
						Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
							"tag": openapi.String(`Image tag, or ref to build. Defaults to "latest" or the app's stored ref.`),
						})),
					},
					Responses: openapi.Responses{
						"202": openapi.JSONResponse("The deploy was accepted and is running.", openapi.Ref("Deployment")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The app's source cannot produce an image — for an app pushed to Cubeship's registry, that means the instance has no domain configured yet."),
					},
				},
			},
			appPath + "/deployments": {
				"get": {
					OperationID: "listDeployments",
					Summary:     "List an app's deploy history",
					Description: "Newest first, capped at the most recent " + strconv.Itoa(MaxDeploymentHistory) + ".",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The app's recent deploys.", openapi.Array(openapi.Ref("Deployment"))),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
			},
			appPath + "/deployments/{id}": {
				"get": {
					OperationID: "getDeployment",
					Summary:     "Check one deploy",
					Description: "How a deploy went, or how far it has got.\n\nWith `wait=true` the response is held open until the deploy finishes. If your own timeout runs out first you get the deployment as it stands and can ask again — the deploy is not affected either way.",
					Tags:        []string{"Apps"},
					Parameters: append(refParams,
						openapi.PathParam("id", "The deployment's id, from the deploy response."),
						openapi.QueryParam("wait", `"true" to hold the response open until the deploy finishes.`)),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The deployment.", openapi.Ref("Deployment")),
						"400": openapi.TextResponse("The id is not a number."),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "deleteDeployment",
					Summary:     "Remove one deploy's record",
					Description: "For a deploy the app is not running, this removes a record: the container it produced is long gone, and what goes with the row is the build log. A failed deploy is history nobody wants to keep looking at.\n\n**For the deploy the app *is* running — the one with `live` — this takes the app down.** Its container is stopped and removed and the app's status becomes `down`. That is the point of it: an app whose running version has to go now, because it is compromised or doing what it should not, should not force you to delete the app and lose its domains, its environment and everything attached to it. The app stays, with all of it, and comes back on the next deploy.\n\nOne is refused: a deploy that has not finished, because the daemon is still writing to that row. `deletable` says so in advance — and a deploy waiting on a machine that has stopped answering is the exception, since nothing is writing to that one. Deleting it has a consequence worth knowing: the machine that returns will then run the deployment below it.\n\nThe image stays in the registry either way, and reclaiming that disk is a garbage collection pass nothing schedules — the same thing that is true when an app itself is deleted. The role is the one that deploys this app: somebody who may replace what is running may take it off.",
					Tags:        []string{"Apps"},
					Parameters: append(refParams,
						openapi.PathParam("id", "The deployment's id.")),
					Responses: openapi.Responses{
						"204": openapi.Empty("The record is gone — and with it the container, if this was the deploy the app was running."),
						"400": openapi.TextResponse("The id is not a number."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The deploy has not finished."),
					},
				},
			},
			appPath + "/env": {
				"get": {
					OperationID: "getAppEnv",
					Summary:     "Read an app's environment variables",
					Description: "`vars` is what the app itself sets. `effective` is what its container actually runs with — the project's variables, overridden by the environment's, overridden by the app's — with each value labelled by the level that won it.\n\nRead this before changing anything: it is the only way to see what an app is configured with.",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The app's own and effective variables.", openapi.Ref("EnvVars")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"patch": {
					OperationID: "mergeAppEnv",
					Summary:     "Add, change or remove some of an app's variables",
					Description: "The safe way to change configuration: only the keys you name are touched, so you cannot delete a variable by forgetting to mention it." +
						envRole,
					Tags:       []string{"Apps"},
					Parameters: refParams,
					RequestBody: &openapi.RequestBody{
						Required:    true,
						Description: "Adds or overwrites the variables in `set` and removes those named in `unset`. Every other app-level variable is left exactly as it was.",
						Content:     openapi.JSON(openapi.Ref("MergeEnv")),
					},
					Responses: openapi.Responses{
						"200": openapi.Empty("The variables are stored. The running container picks them up on its next deploy."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("The app builds, and you lack the admin role its build takes."),
						"404": openapi.NotFound,
					},
				},
				"put": {
					OperationID: "setAppEnv",
					Summary:     "Set an app's environment variables",
					Description: "These are layered on top of, and override, the app's environment's and project's variables." +
						envRole,
					Tags:       []string{"Apps"},
					Parameters: refParams,
					RequestBody: &openapi.RequestBody{
						Required:    true,
						Description: "**Replaces** the full set of app-level variables: any key you omit is deleted. Use PATCH to change some without disturbing the rest.",
						Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
							"vars": openapi.StringMap("The complete set of app-level variables."),
						}, "vars")),
					},
					Responses: openapi.Responses{
						"200": openapi.Empty("The variables are stored. The running container picks them up on its next deploy."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("The app builds, and you lack the admin role its build takes."),
						"404": openapi.NotFound,
					},
				},
			},
			appPath + "/metrics": {
				"get": {
					OperationID: "getAppMetrics",
					Summary:     "Read an app's CPU and memory",
					Description: "What the container serving this app has been using.\n\n" + metrics.Description,
					Tags:        []string{"Apps"},
					Parameters:  append(append([]openapi.Parameter{}, refParams...), metrics.WindowParam()),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The series.", openapi.Ref("MetricSeries")),
						"400": openapi.TextResponse("No such window."),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
			},
			appPath + "/logs": {
				"get": {
					OperationID: "getAppLogs",
					Summary:     "Read an app's container logs",
					Description: "Stdout and stderr, already demultiplexed out of Docker's frame format. Returns the last " + DefaultLogTail + " lines unless `tail` says otherwise.",
					Tags:        []string{"Apps"},
					Parameters: append(refParams,
						openapi.QueryParam("tail", `Number of trailing lines, e.g. "1000", or "all" for the entire log. Defaults to `+DefaultLogTail+"."),
						openapi.QueryParam("server", "Which machine's copy to read, for an app that runs on more than one. A log belongs to one container and so to one machine, and there is no combined one — interleaving them would need a clock those machines do not share. Defaults to the machine serving the app's names.")),
					Responses: openapi.Responses{
						"200": {
							Description: "The log output.",
							Content:     map[string]openapi.MediaType{"text/plain": {Schema: openapi.String("")}},
						},
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The app has no container yet — nothing has been pushed to it."),
					},
				},
			},
			"/apps/{project}/{env}/{name}/domains": {
				"post": {
					OperationID: "addAppDomain",
					Summary:     "Give an app a name to answer at",
					Description: "An app can answer at several, and each names its own port — one image can expose more than one, and `api.example.com` and `admin.example.com` on one container are two of them.\n\n" +
						"`port` is what this name reaches inside the container; omitted or 0 falls back to 8080.\n\n" +
						"A name is unique across the instance, not per app: Traefik routes by host and nothing else, so two apps claiming one name would give it two answers.\n\n" +
						"**The container keeps the labels it was created with**, so a new name is not served until the app is redeployed. Organization admins only — where an app is served is a routing decision, the same weight as changing its source.",
					Tags:       []string{"Apps"},
					Parameters: refParams,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"host": openapi.String("The name to serve this app at, as a DNS name. It has to resolve to this host before a certificate can issue, and it cannot be the instance's own domain or its registry's."),
						"port": openapi.Integer("What it reaches inside the container. Omit to read it from the image."),
					}, "host")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The app, with the name added.", openapi.Ref("App")),
						"400": openapi.TextResponse("The name is not a DNS name, or it is one this instance answers at itself — the dashboard and the registry have Traefik routers of their own."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("Another app is already served at that name."),
					},
				},
			},
			"/apps/{project}/{env}/{name}/domains/{domainID}": {
				"patch": {
					OperationID: "setAppDomainPort",
					Summary:     "Change what one of an app's names reaches",
					Description: "0 puts it back to reading the port from the image. Takes effect on the next deploy — a container keeps the labels it was created with.",
					Tags:        []string{"Apps"},
					Parameters:  append(refParams, openapi.PathParam("domainID", "The domain's id, from the app.")),
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"port": openapi.Integer("The port inside the container, or 0 to read it from the image."),
					}, "port")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The app, with the port changed.", openapi.Ref("App")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "removeAppDomain",
					Summary:     "Take a name off an app",
					Description: "The container keeps the labels it was created with, so the name goes on being served until the app is redeployed. Organization admins only.",
					Tags:        []string{"Apps"},
					Parameters:  append(refParams, openapi.PathParam("domainID", "The domain's id, from the app.")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The app, with the name removed.", openapi.Ref("App")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
		},
	}
}

// arrayOf is openapi.Array with something to say about the list itself.
// The items carry their own description; this is what the field means.
func arrayOf(items *openapi.Schema, description string) *openapi.Schema {
	a := openapi.Array(items)
	a.Description = description
	return a
}
