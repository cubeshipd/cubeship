package app

import (
	"maps"

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
	const appPath = "/apps/{org}/{project}/{env}/{name}"

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Apps",
			Description: "A deployable service. Pushing a tag to an app's registry path is what deploys it; these endpoints register apps, configure them, and redeploy on demand.",
		}},
		Schemas: mergeSchemas(project.EnvSchemas(), map[string]*openapi.Schema{
			"App": openapi.Object(map[string]*openapi.Schema{
				"reference":   openapi.String("The app's identifier, org/project/environment/name — also its registry repository path."),
				"name":        openapi.String("Unique within its environment, not across the instance."),
				"domain":      openapi.String("The domain Traefik routes to this app, over HTTPS."),
				"image":       openapi.String("The registry path to push to. A push here deploys."),
				"status":      {Type: "string", Enum: []string{"pending", "running", "down"}, Description: `"pending" until the first image is pushed.`},
				"org":         openapi.String(""),
				"project":     openapi.String(""),
				"environment": openapi.String(""),
			}, "reference", "name", "domain", "image", "status", "org", "project", "environment"),
		}),
		Paths: map[string]openapi.PathItem{
			"/apps": {
				"post": {
					OperationID: "createApp",
					Summary:     "Register an app",
					Description: "Returns the registry path to push to. Nothing is deployed until an image lands there.\n\nThe name only has to be unique within its environment, so the same app can exist in `production` and `staging` at once — they get different registry paths and different containers.\n\nApp containers are expected to listen on port 8080. Requires the member role in the organization.",
					Tags:        []string{"Apps"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"name":        openapi.String("Lowercase letters, digits and dashes — it becomes a path component of the registry image."),
						"domain":      openapi.String("The domain to serve this app on. It must resolve to this host for a certificate to issue."),
						"org":         openapi.String("Organization slug."),
						"project":     openapi.String("Project slug."),
						"environment": openapi.String(`Environment slug. Defaults to "production".`),
					}, "name", "domain", "org", "project")),
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
					Description: "Deploys a tag already pushed to the app's registry path. The daemon pulls the image, starts a container, waits for it to look healthy, and only then retires the previous one — so a bad image never takes down a working app.\n\n**This request blocks for the whole deploy**, which includes several seconds of health checks. Use a client timeout of at least a few minutes.",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
					RequestBody: &openapi.RequestBody{
						Description: `Optional. Omit the body entirely to deploy "latest".`,
						Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
							"tag": openapi.String(`Image tag to deploy. Defaults to "latest".`),
						})),
					},
					Responses: openapi.Responses{
						"200": openapi.Empty("The new container is running and the old one is retired."),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
						"502": openapi.JSONResponse("The deploy failed — the image could not be pulled, or the container never became healthy. The app is untouched.",
							openapi.Object(map[string]*openapi.Schema{"error": openapi.String("What went wrong.")}, "error")),
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
					Description: "The safe way to change configuration: only the keys you name are touched, so you cannot delete a variable by forgetting to mention it. Requires the member role.",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
					RequestBody: &openapi.RequestBody{
						Required:    true,
						Description: "Adds or overwrites the variables in `set` and removes those named in `unset`. Every other app-level variable is left exactly as it was.",
						Content:     openapi.JSON(openapi.Ref("MergeEnv")),
					},
					Responses: openapi.Responses{
						"200": openapi.Empty("The variables are stored. The running container picks them up on its next deploy."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"put": {
					OperationID: "setAppEnv",
					Summary:     "Set an app's environment variables",
					Description: "These are layered on top of, and override, the app's environment's and project's variables. Requires the member role.",
					Tags:        []string{"Apps"},
					Parameters:  refParams,
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
					),
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
		},
	}
}
