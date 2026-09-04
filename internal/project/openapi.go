package project

import (
	"maps"

	"cubeship/internal/platform/openapi"
)

// mergeSchemas folds b into a and returns a.
func mergeSchemas(a, b map[string]*openapi.Schema) map[string]*openapi.Schema {
	maps.Copy(a, b)
	return a
}

// envVarsBody is the body every "set env" endpoint takes. Naming it once
// keeps the replace-not-merge warning identical everywhere it applies.
func envVarsBody(level string) *openapi.RequestBody {
	return &openapi.RequestBody{
		Required:    true,
		Description: "**Replaces** the full set of " + level + "-level variables: any key you omit is deleted. Use PATCH to change some without disturbing the rest.",
		Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
			"vars": openapi.StringMap("The complete set of variables at this level."),
		}, "vars")),
	}
}

// EnvSchemas describes reading and merging variables at any level.
// internal/app includes them too, so neither module depends on the other
// having been mounted for its $refs to resolve.
func EnvSchemas() map[string]*openapi.Schema {
	return map[string]*openapi.Schema{
		"ResolvedVar": openapi.Object(map[string]*openapi.Schema{
			"key":    openapi.String(""),
			"value":  openapi.String(""),
			"source": {Type: "string", Enum: []string{"project", "environment", "app"}, Description: "The level that set this value, after inheritance."},
		}, "key", "value", "source"),

		"EnvVars": openapi.Object(map[string]*openapi.Schema{
			"vars":      openapi.StringMap("The variables set at this level, and only this level."),
			"effective": openapi.Array(openapi.Ref("ResolvedVar")),
		}, "vars"),

		"MergeEnv": openapi.Object(map[string]*openapi.Schema{
			"set":   openapi.StringMap("Variables to add or overwrite."),
			"unset": openapi.Array(openapi.String("A variable name to remove.")),
		}),
	}
}

// mergeEnvBody is the PATCH body, with the wording that matters most:
// what it does NOT touch.
func mergeEnvBody(level string) *openapi.RequestBody {
	return &openapi.RequestBody{
		Required:    true,
		Description: "Adds or overwrites the variables in `set` and removes those named in `unset`. Every other " + level + "-level variable is left exactly as it was.",
		Content:     openapi.JSON(openapi.Ref("MergeEnv")),
	}
}

func (h *Handler) OpenAPI() openapi.Spec {
	orgParam := openapi.PathParam("orgSlug", "The organization's slug.")
	projectParam := openapi.PathParam("projectSlug", "The project's slug.")
	envParam := openapi.PathParam("envSlug", "The environment's slug.")

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Projects & environments",
			Description: "Apps live in an environment, inside a project, inside an organization. Environment variables can be set at each level, and an app inherits the levels above it: project, then environment, then the app's own — each overriding the last.",
		}},
		Schemas: mergeSchemas(EnvSchemas(), map[string]*openapi.Schema{
			"Project": openapi.Object(map[string]*openapi.Schema{
				"slug":         openapi.String(""),
				"name":         openapi.String(""),
				"description":  openapi.String("What the project is for. Empty unless someone set it."),
				"environments": openapi.Array(openapi.String("Environment slug. Only returned when the project is created.")),
			}, "slug", "name", "description"),

			"Environment": openapi.Object(map[string]*openapi.Schema{
				"slug":        openapi.String(""),
				"name":        openapi.String(""),
				"description": openapi.String("What this stage of the project is for. Empty unless someone set it."),
			}, "slug", "name", "description"),
		}),
		Paths: map[string]openapi.PathItem{
			"/orgs/{orgSlug}/projects": {
				"post": {
					OperationID: "createProject",
					Summary:     "Create a project",
					Description: `Comes with a "production" environment, which can never be deleted — so an app can be created in the project immediately. Requires the admin role in the organization.`,
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"slug": openapi.String("Lowercase letters, digits and dashes."),
						"name": openapi.String("Optional. Left out, it is derived from the slug — `public-api` becomes `Public Api` — and can be edited afterwards."),
					}, "slug")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse(`The project and its "production" environment.`, openapi.Ref("Project")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("A project with that slug already exists in this organization."),
					},
				},
				"get": {
					OperationID: "listProjects",
					Summary:     "List an organization's projects",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The organization's projects.", openapi.Array(openapi.Ref("Project"))),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
			},
			"/orgs/{orgSlug}/projects/{projectSlug}": {
				"patch": {
					OperationID: "updateProject",
					Summary:     "Rename a project or describe it",
					Description: "Changes the name, the description, or both. **A field you leave out is left as it was**, so one can be edited without sending the other back. Requires the admin role.\n\nThe slug is not editable, and no slug in Cubeship is once its resource exists: every one of them is a path component of an app's registry reference, which is derived on read rather than stored. Renaming one would move every app under it, breaking pushes configured against the old path and stranding images already pushed there.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam},
					RequestBody: &openapi.RequestBody{
						Required:    true,
						Description: "Name, description, or both. Omit a field to leave it alone; send an empty description to clear it.",
						Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
							"name":        openapi.String("Cannot be empty."),
							"description": openapi.String("May be empty."),
						})),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The project as it now stands.", openapi.Ref("Project")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "deleteProject",
					Summary:     "Delete a project",
					Description: "Removes the project and every environment in it. **Refused while any app still lives in the project** — delete those first, since removing an app means stopping its container. Requires the admin role.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam},
					Responses: openapi.Responses{
						"200": openapi.Empty("The project and its environments are gone."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The project still has apps in it."),
					},
				},
			},
			"/orgs/{orgSlug}/projects/{projectSlug}/env": {
				"get": {
					OperationID: "getProjectEnv",
					Summary:     "Read a project's environment variables",
					Description: "What is set on the project itself. Every environment and every app below inherits these.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The project's variables.", openapi.Ref("EnvVars")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"patch": {
					OperationID: "mergeProjectEnv",
					Summary:     "Add, change or remove some of a project's variables",
					Description: "The safe way to change configuration: only the keys you name are touched. Requires the admin role.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam},
					RequestBody: mergeEnvBody("project"),
					Responses: openapi.Responses{
						"200": openapi.Empty("The variables are stored. Running containers pick them up on their next deploy."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"put": {
					OperationID: "setProjectEnv",
					Summary:     "Set a project's environment variables",
					Description: "Shared by every environment, and every app, in the project. Requires the admin role.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam},
					RequestBody: envVarsBody("project"),
					Responses: openapi.Responses{
						"200": openapi.Empty("The variables are stored. Running containers pick them up on their next deploy."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			"/orgs/{orgSlug}/projects/{projectSlug}/environments": {
				"post": {
					OperationID: "createEnvironment",
					Summary:     "Create an environment",
					Description: "An additional environment within a project — staging, preview, whatever the project needs. Requires the admin role.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"slug": openapi.String("Lowercase letters, digits and dashes."),
						"name": openapi.String("Optional. Left out, it is derived from the slug — `public-api` becomes `Public Api` — and can be edited afterwards."),
					}, "slug")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The new environment.", openapi.Ref("Environment")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("An environment with that slug already exists in this project."),
					},
				},
				"get": {
					OperationID: "listEnvironments",
					Summary:     "List a project's environments",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The project's environments.", openapi.Array(openapi.Ref("Environment"))),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
			},
			"/orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}/env": {
				"get": {
					OperationID: "getEnvironmentEnv",
					Summary:     "Read an environment's variables",
					Description: "`vars` is what this environment sets; `effective` is what an app here inherits — the project's, overridden by this environment's — with each value labelled by the level that won it.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam, envParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The environment's own and effective variables.", openapi.Ref("EnvVars")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"patch": {
					OperationID: "mergeEnvironmentEnv",
					Summary:     "Add, change or remove some of an environment's variables",
					Description: "Only the keys you name are touched. Requires the admin role.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam, envParam},
					RequestBody: mergeEnvBody("environment"),
					Responses: openapi.Responses{
						"200": openapi.Empty("The variables are stored. Running containers pick them up on their next deploy."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"put": {
					OperationID: "setEnvironmentEnv",
					Summary:     "Set an environment's variables",
					Description: "Shared by every app in this one environment, and overriding the project's. Requires the admin role.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam, envParam},
					RequestBody: envVarsBody("environment"),
					Responses: openapi.Responses{
						"200": openapi.Empty("The variables are stored. Running containers pick them up on their next deploy."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			"/orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}": {
				"patch": {
					OperationID: "updateEnvironment",
					Summary:     "Rename an environment or describe it",
					Description: "Changes the name, the description, or both. **A field you leave out is left as it was.** The slug is not editable here: it is the third component of every app reference in the environment. Requires the admin role.",
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam, envParam},
					RequestBody: &openapi.RequestBody{
						Required:    true,
						Description: "Name, description, or both. Omit a field to leave it alone; send an empty description to clear it.",
						Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
							"name":        openapi.String("Cannot be empty."),
							"description": openapi.String("May be empty."),
						})),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The environment as it now stands.", openapi.Ref("Environment")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "deleteEnvironment",
					Summary:     "Delete an environment",
					Description: `Refused for "production", which every project must keep, and refused while any app still lives in it. Requires the admin role.`,
					Tags:        []string{"Projects & environments"},
					Parameters:  []openapi.Parameter{orgParam, projectParam, envParam},
					Responses: openapi.Responses{
						"200": openapi.Empty("The environment is deleted."),
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse(`Either "production", which can never be deleted, or you lack the admin role.`),
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The environment still has apps in it."),
					},
				},
			},
		},
	}
}
