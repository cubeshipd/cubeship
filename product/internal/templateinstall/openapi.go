package templateinstall

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	owner := openapi.PathParam("owner", "The template repository's owner on GitHub.")
	repo := openapi.PathParam("repo", "The template repository's name.")
	id := openapi.PathParam("id", "The installation's id.")
	unreachable := openapi.TextResponse("The catalog or GitHub could not be reached.")
	resource := openapi.Object(map[string]*openapi.Schema{
		"kind": {Type: "string", Enum: []string{KindProject, KindEnvironment, KindDatabase, KindStore, KindApp, KindDomain, KindAttachment}},
		"key":  openapi.String("The template's key for it. Absent for a project or an environment."),
		"name": openapi.String("A slug; project/environment for an environment; an app's full reference; \"reference host\" for a domain; \"reference database name\" or \"reference store name bucket\" for an attachment."),
	}, "kind", "name")

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "Templates",
			Description: "Ready-made apps from the catalog at cubeship.dev, with the databases and stores they need. " +
				"The instance reads the catalog and, to install or update one, the template's file on GitHub at the release's commit, which it validates itself before changing anything.",
		}},
		Schemas: map[string]*openapi.Schema{
			"TemplateRun": openapi.Object(map[string]*openapi.Schema{
				"id":           openapi.Integer(""),
				"kind":         {Type: "string", Enum: []string{RunInstall, RunUpdate, RunUninstall}},
				"from_release": openapi.String("The release an update or an uninstall started from."),
				"to_release":   openapi.String("The release an install or an update is moving to."),
				"status": {Type: "string", Enum: []string{RunRunning, RunSucceeded, RunFailed},
					Description: "A failed run has been undone: a failed install deleted what it created, a failed update put the installation back as it was."},
				"step":        openapi.String("What a running run is doing, in a sentence."),
				"error":       openapi.String("Why it failed."),
				"created":     openapi.Array(resource),
				"keep_data":   openapi.Bool("An uninstall that leaves databases and object stores in place."),
				"created_at":  openapi.String("RFC 3339."),
				"finished_at": openapi.String("RFC 3339."),
			}, "id", "kind", "status", "created", "created_at"),
			"TemplateInstall": openapi.Object(map[string]*openapi.Schema{
				"id":          openapi.Integer("Identifies the installation."),
				"owner":       openapi.String(""),
				"repo":        openapi.String(""),
				"release":     openapi.String("The release it is on."),
				"commit":      openapi.String("The commit that release's file was read at."),
				"project":     openapi.String(""),
				"environment": openapi.String(""),
				"status": {Type: "string", Enum: []string{StatusInstalling, StatusInstalled, StatusFailed, StatusUninstalled},
					Description: "failed is an install that did not finish and was undone."},
				"resources":        openapi.Array(resource),
				"runs":             openapi.Array(openapi.Ref("TemplateRun")),
				"busy":             openapi.Bool("A run is changing it now; nothing else may start until it ends."),
				"update_available": openapi.String("The newest release the catalog accepted, when the installation is not on it. Null otherwise."),
				"created_at":       openapi.String("RFC 3339."),
				"updated_at":       openapi.String("RFC 3339."),
			}, "id", "owner", "repo", "release", "commit", "project", "environment", "status", "resources", "runs", "busy", "update_available", "created_at", "updated_at"),
			"TemplateUpdatePreview": openapi.Object(map[string]*openapi.Schema{
				"from": openapi.String(""),
				"to":   openapi.String(""),
				"changes": openapi.Array(openapi.Object(map[string]*openapi.Schema{
					"action": {Type: "string", Enum: []string{ActionCreate, ActionChange, ActionKeep},
						Description: "keep is something the release no longer has, or cannot change: it is left as it is."},
					"kind":   openapi.String("app, database, store, bucket, variable, domain or attachment."),
					"name":   openapi.String(""),
					"detail": openapi.String("The change, in a sentence."),
				}, "action", "kind", "name")),
				"inputs": openapi.Array(openapi.Object(nil)),
			}, "from", "to", "changes", "inputs"),
		},
		Paths: map[string]openapi.PathItem{
			"/templates": {
				"get": {
					OperationID: "listTemplates",
					Summary:     "Search the template catalog",
					Description: "The catalog's own listing, read by the instance. q matches a name, a description or a tag.",
					Tags:        []string{"Templates"},
					Parameters: []openapi.Parameter{
						openapi.QueryParam("q", "Search text."),
						openapi.QueryParam("tag", "Only templates with this tag."),
						openapi.QueryParam("sort", "recent (newest release first, the default) or stars."),
						openapi.QueryParam("cursor", "next_cursor from the previous page."),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("A page of templates.", openapi.Object(nil)),
						"401": openapi.Unauthorized,
						"502": unreachable,
					},
				},
			},
			"/template-tags": {
				"get": {
					OperationID: "listTemplateTags",
					Summary:     "List the catalog's tags",
					Description: "Every tag a listed template carries, for filtering the catalog.",
					Tags:        []string{"Templates"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The tags.", openapi.Object(map[string]*openapi.Schema{
							"tags": openapi.Array(openapi.String("")),
						}, "tags")),
						"401": openapi.Unauthorized,
						"502": unreachable,
					},
				},
			},
			"/templates/{owner}/{repo}": {
				"get": {
					OperationID: "getTemplate",
					Summary:     "Get a template",
					Description: "Its listing, README, file and normalized manifest, as the catalog serves them.",
					Tags:        []string{"Templates"},
					Parameters:  []openapi.Parameter{owner, repo},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The template.", openapi.Object(nil)),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
						"502": unreachable,
					},
				},
			},
			"/templates/{owner}/{repo}/installs": {
				"post": {
					OperationID: "installTemplate",
					Summary:     "Install a template",
					Description: "Checks everything first — the file, the names, the answers — and creates nothing unless all of it passes. " +
						"Then records the installation and runs the install in the background: project and environment when they do not exist, databases, stores, apps, domains, attachments, variables, and a deploy of each app. " +
						"Anything that fails is undone.\n\n" +
						"`secrets` holds every secret the instance generated, **and this response is the only place they appear**.",
					Tags:       []string{"Templates"},
					Parameters: []openapi.Parameter{owner, repo},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"release":     openapi.String("A release tag the catalog accepted. Defaults to the newest."),
						"project":     openapi.String("Where the apps go. Defaults to the template's suggestion; created when missing."),
						"environment": openapi.String("Defaults to the template's; created when missing."),
						"names": openapi.Object(map[string]*openapi.Schema{
							"databases": openapi.StringMap("Database names, by the template's key."),
							"stores":    openapi.StringMap("Object store names, by key."),
							"apps":      openapi.StringMap("App names, by key."),
						}),
						"inputs": openapi.StringMap("Answers to the template's inputs, by key."),
					})),
					Responses: openapi.Responses{
						"202": openapi.JSONResponse("The install has started.", openapi.Object(map[string]*openapi.Schema{
							"install": openapi.Ref("TemplateInstall"),
							"secrets": openapi.StringMap("Generated secrets, by input key. Shown once."),
						}, "install", "secrets")),
						"400": openapi.TextResponse("An answer, a name, the project or the environment cannot be used."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such template, or no accepted release by that tag."),
						"409": openapi.TextResponse("Something the install would create already exists, or the template needs a newer Cubeship."),
						"422": openapi.TextResponse("The template's file did not validate on this instance."),
						"502": unreachable,
					},
				},
			},
			"/template-icons/{repository}/{file}": {
				"get": {
					OperationID: "getTemplateIcon",
					Summary:     "Get a template's icon",
					Description: "The icon a listing names, fetched through the instance.",
					Tags:        []string{"Templates"},
					Parameters: []openapi.Parameter{
						openapi.PathParam("repository", "The repository's id in the catalog."),
						openapi.PathParam("file", "<commit>.png"),
					},
					Responses: openapi.Responses{
						"200": {Description: "A PNG."},
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
			},
			"/template-installs": {
				"get": {
					OperationID: "listTemplateInstalls",
					Summary:     "List installed templates",
					Description: "What is installed or being installed, newest first, each with its most recent run and whether the catalog has a newer release.",
					Tags:        []string{"Templates"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The installations.", openapi.Array(openapi.Ref("TemplateInstall"))),
						"401": openapi.Unauthorized,
					},
				},
			},
			"/template-installs/{id}": {
				"get": {
					OperationID: "getTemplateInstall",
					Summary:     "Get an installation",
					Description: "What it created and its recent runs. Poll it while busy.",
					Tags:        []string{"Templates"},
					Parameters:  []openapi.Parameter{id},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The installation.", openapi.Ref("TemplateInstall")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
			},
			"/template-installs/{id}/update": {
				"get": {
					OperationID: "previewTemplateUpdate",
					Summary:     "Preview an update",
					Description: "What updating to a release would do, changing nothing: what it creates, what it changes, what it leaves in place because the release no longer has it, and the questions it needs answered.",
					Tags:        []string{"Templates"},
					Parameters:  []openapi.Parameter{id, openapi.QueryParam("release", "A release tag. Defaults to the newest the catalog accepted.")},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The preview.", openapi.Ref("TemplateUpdatePreview")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The installation is not installed, or a run is changing it."),
						"422": openapi.TextResponse("The release's file did not validate on this instance."),
						"502": unreachable,
					},
				},
				"post": {
					OperationID: "updateTemplateInstall",
					Summary:     "Update an installation",
					Description: "Applies what the release changed, in the background, and deletes nothing. Every app it changes is recorded first; if any step fails, what the update created is deleted, those apps are set back and deployed as they were, and the installation stays on its release.\n\n" +
						"`secrets` holds every secret the release adds that the instance generated, shown this once.",
					Tags:       []string{"Templates"},
					Parameters: []openapi.Parameter{id},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"release": openapi.String("A release tag. Defaults to the newest the catalog accepted."),
						"inputs":  openapi.StringMap("Answers to the questions the preview listed, by key."),
					})),
					Responses: openapi.Responses{
						"202": openapi.JSONResponse("The update has started.", openapi.Object(map[string]*openapi.Schema{
							"run":     openapi.Ref("TemplateRun"),
							"secrets": openapi.StringMap("Generated secrets, by input key. Shown once."),
						}, "run", "secrets")),
						"400": openapi.TextResponse("An answer is missing or cannot be used."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("Already on that release, not installed, busy, or something the update would create already exists."),
						"422": openapi.TextResponse("The release's file did not validate on this instance."),
						"502": unreachable,
					},
				},
			},
			"/template-installs/{id}/uninstall": {
				"post": {
					OperationID: "uninstallTemplateInstall",
					Summary:     "Uninstall an installation",
					Description: "Deletes its apps in the background, then the project and environment it created once they are empty. " +
						"Its databases and object stores are kept unless `keep_data` is false, **which deletes them and everything in them permanently**.",
					Tags:       []string{"Templates"},
					Parameters: []openapi.Parameter{id},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"keep_data": openapi.Bool("Leave its databases and object stores in place. Defaults to true."),
					})),
					Responses: openapi.Responses{
						"202": openapi.JSONResponse("The uninstall has started.", openapi.Object(map[string]*openapi.Schema{
							"run": openapi.Ref("TemplateRun"),
						}, "run")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The installation is not installed, or a run is changing it."),
					},
				},
			},
		},
	}
}
