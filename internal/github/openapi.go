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
			"GitHubRepository": openapi.Object(map[string]*openapi.Schema{
				"full_name":      openapi.String("owner/name, which is how GitHub names it and how a branch listing asks for it."),
				"private":        openapi.Bool("Whether cloning it needs the installation's token."),
				"default_branch": openapi.String("What it builds when an app names no ref."),
			}, "full_name", "private", "default_branch"),
			"GitHubBranch": openapi.Object(map[string]*openapi.Schema{
				"name": openapi.String(""),
			}, "name"),
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
					Description: "Called after someone finishes installing the App: GitHub sends them back with the installation's id **and a code**, and this is what ties the installation to an organization.\n\n" +
						"**The code is required, and it is what makes this safe.** The App is public — a private one can only be installed on the account that owns it, so no organization could use it — which means anyone can install it and every installation id is somebody's real id. The code comes from the OAuth round trip GitHub runs on install; the daemon spends it, asks GitHub which installations that person administers, and refuses any id that is not among them. Without that, connecting an installation would mint tokens for a stranger's installation, which is read access to their private code.\n\n" +
						"The account is **not** taken from the request. It comes back from GitHub with the installation, because it is what every repository lookup matches against and a mismatched one would silently stop matching.\n\n" +
						"Connecting an account decides what code this instance will build and run, so it takes the admin role — the same one building does.\n\n" +
						"Recording an installation that is already recorded moves it to this organization rather than failing: reinstalling reuses an id, and moving one between organizations is a real thing to do.",
					Tags:       []string{"GitHub"},
					Parameters: []openapi.Parameter{orgParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"installation_id": openapi.Integer("GitHub's id for the installation."),
						"code":            openapi.String("The OAuth code GitHub redirects back with after the install. Proof the installation is yours."),
					}, "installation_id", "code")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The connection.", openapi.Ref("GitHubInstallation")),
						"400": openapi.TextResponse("No installation id, or no code to prove it is yours."),
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("You lack the admin role, or GitHub does not list that installation among the ones you can reach."),
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This instance's App has no OAuth credentials — it was registered before they were asked for. Re-register it."),
					},
				},
			},
			"/settings/github/manifest": {
				"post": {
					OperationID: "registerGitHubAppFromManifest",
					Summary:     "Register this instance as a GitHub App from a manifest",
					Description: "The end of the flow that spares someone creating an App by hand: the dashboard sends them to GitHub with a manifest, GitHub creates the App and redirects back with a code, and this exchanges it for the App's id, slug, private key and webhook secret — all four written straight into the settings.\n\n" +
						"The code is single-use and expires in an hour. Super-admin only: this is the instance's configuration, not an organization's.",
					Tags: []string{"GitHub"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"code": openapi.String("What GitHub redirected back with."),
					}, "code")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The instance's configuration, with the App now registered.", openapi.Ref("Settings")),
						"400": openapi.TextResponse("No code was given, or GitHub refused it."),
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("You are not a super-admin."),
					},
				},
			},
			"/orgs/{orgSlug}/github/repositories": {
				"get": {
					OperationID: "listGitHubRepositories",
					Summary:     "List the repositories this organization can build",
					Description: "Exactly what its installations were granted, across all of them — someone who installed the App on three repositories sees three, not everything they own.\n\n" +
						"This is what a dashboard offers instead of asking for a URL: picking from a list cannot mistype an owner, and cannot name a repository this instance has no way to clone.\n\n" +
						"Organization admins only.",
					Tags:       []string{"GitHub"},
					Parameters: []openapi.Parameter{orgParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The repositories.", openapi.Array(openapi.Ref("GitHubRepository"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This instance is not registered as a GitHub App."),
					},
				},
			},
			"/orgs/{orgSlug}/github/branches": {
				"get": {
					OperationID: "listGitHubBranches",
					Summary:     "List a repository's branches",
					Description: "For the same reason the repository listing exists: a branch is chosen, not spelled. Organization admins only.",
					Tags:        []string{"GitHub"},
					Parameters: []openapi.Parameter{
						orgParam,
						openapi.QueryParam("repo", "The repository, as owner/name."),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The branches.", openapi.Array(openapi.Ref("GitHubBranch"))),
						"400": openapi.TextResponse("No repository was named."),
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("The App was not granted access to that repository."),
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This organization has not connected that repository's account."),
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
