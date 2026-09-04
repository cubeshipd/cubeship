package project

import "cubeship/internal/platform/openapi"

// envVarsBody is the body every "set env" endpoint takes. Naming it once
// keeps the replace-not-merge warning identical everywhere it applies.
func envVarsBody(level string) *openapi.RequestBody {
	return &openapi.RequestBody{
		Required:    true,
		Description: "Replaces the full set of " + level + "-level variables. Keys you omit are removed.",
		Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
			"vars": openapi.StringMap("The complete set of variables at this level."),
		}, "vars")),
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
		Schemas: map[string]*openapi.Schema{
			"Project": openapi.Object(map[string]*openapi.Schema{
				"slug":         openapi.String(""),
				"name":         openapi.String(""),
				"environments": openapi.Array(openapi.String("Environment slug. Only returned when the project is created.")),
			}, "slug", "name"),

			"Environment": openapi.Object(map[string]*openapi.Schema{
				"slug": openapi.String(""),
				"name": openapi.String(""),
			}, "slug", "name"),
		},
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
						"name": openapi.String(""),
					}, "slug", "name")),
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
			"/orgs/{orgSlug}/projects/{projectSlug}/env": {
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
						"name": openapi.String(""),
					}, "slug", "name")),
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
