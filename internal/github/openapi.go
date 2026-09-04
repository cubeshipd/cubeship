package github

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	orgParam := openapi.PathParam("orgSlug", "Organization slug.")
	idParam := openapi.PathParam("id", "Installation id, as Cubeship numbers it.")

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "GitHub",
			Description: "Which GitHub accounts an organization has connected. " +
				"A connection is what lets Cubeship clone that account's private repositories, and what makes a push to one deploy the apps built from it.\n\n" +
				"The instance acts as a single GitHub App, registered by whoever runs it. Organizations install that App on their own accounts.",
		}},
		Schemas: map[string]*openapi.Schema{
			"GitHubInstallation": openapi.Object(map[string]*openapi.Schema{
				"id":              openapi.Integer("Identifies it in the paths below."),
				"installation_id": openapi.Integer("GitHub's own id for the installation."),
				"account":         openapi.String("The GitHub account it was installed on."),
				"created_at":      openapi.String("RFC 3339."),
			}, "id", "installation_id", "account", "created_at"),
			"GitHubConnections": openapi.Object(map[string]*openapi.Schema{
				"installations": openapi.Array(openapi.Ref("GitHubInstallation")),
				"install_url":   openapi.String("Where to send someone to install the App on an account. Empty until the instance is registered as a GitHub App."),
			}, "installations"),
		},
		Paths: map[string]openapi.PathItem{
			"/orgs/{orgSlug}/github": {
				"get": {
					OperationID: "listGitHubConnections",
					Summary:     "List an organization's connected GitHub accounts",
					Description: "Also returns where to send someone to connect another. Organization admins only.",
					Tags:        []string{"GitHub"},
					Parameters:  []openapi.Parameter{orgParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The organization's connections.", openapi.Ref("GitHubConnections")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"post": {
					OperationID: "connectGitHubAccount",
					Summary:     "Record a GitHub App installation",
					Description: "Called after someone finishes installing the App: GitHub sends them back with the installation's id, and this is what ties it to an organization.\n\n" +
						"Connecting an account decides what code this instance will build and run, so it takes the admin role — the same one building does.\n\n" +
						"Recording an installation that is already recorded moves it to this organization rather than failing: reinstalling reuses an id, and moving one between organizations is a real thing to do.",
					Tags:       []string{"GitHub"},
					Parameters: []openapi.Parameter{orgParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"installation_id": openapi.Integer("GitHub's id for the installation."),
						"account":         openapi.String("The account it was installed on, e.g. an organization login."),
					}, "installation_id", "account")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The connection.", openapi.Ref("GitHubInstallation")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			"/orgs/{orgSlug}/github/{id}": {
				"delete": {
					OperationID: "disconnectGitHubAccount",
					Summary:     "Forget a GitHub App installation",
					Description: "Apps built from that account's repositories keep running, but their next build clones anonymously and will fail if the repository is private. Pushes to it stop deploying.\n\n" +
						"This does not uninstall the App on GitHub — do that from GitHub. Organization admins only.",
					Tags:       []string{"GitHub"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					Responses: openapi.Responses{
						"204": openapi.Empty("Forgotten."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
		},
	}
}
