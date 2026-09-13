package templateinstall

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	owner := openapi.PathParam("owner", "The template repository's owner on GitHub.")
	repo := openapi.PathParam("repo", "The template repository's name.")

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "Templates",
			Description: "Ready-made apps from the catalog at cubeship.dev, with the databases and stores they need. " +
				"The instance reads the catalog and, to install one, the template's file on GitHub at the release's commit, which it validates itself before creating anything.",
		}},
		Schemas: map[string]*openapi.Schema{
			"TemplateInstall": openapi.Object(map[string]*openapi.Schema{
				"id":          openapi.Integer("Identifies the install."),
				"owner":       openapi.String(""),
				"repo":        openapi.String(""),
				"release":     openapi.String("The release tag installed."),
				"commit":      openapi.String("The commit its file was read at."),
				"project":     openapi.String(""),
				"environment": openapi.String(""),
				"status": {Type: "string", Enum: []string{StatusRunning, StatusSucceeded, StatusFailed},
					Description: "A failed install has already been undone: everything in resources was deleted."},
				"step":  openapi.String("What a running install is doing, in a sentence."),
				"error": openapi.String("Why it failed."),
				"resources": openapi.Array(openapi.Object(map[string]*openapi.Schema{
					"kind": {Type: "string", Enum: []string{KindProject, KindEnvironment, KindDatabase, KindStore, KindApp}},
					"name": openapi.String("A slug; project/environment for an environment; an app's full reference."),
				}, "kind", "name")),
				"created_at":  openapi.String("RFC 3339."),
				"finished_at": openapi.String("RFC 3339."),
			}, "id", "owner", "repo", "release", "commit", "project", "environment", "status", "resources", "created_at"),
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
						"502": openapi.TextResponse("The catalog could not be reached."),
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
						"502": openapi.TextResponse("The catalog could not be reached."),
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
						"502": openapi.TextResponse("The catalog could not be reached."),
					},
				},
			},
			"/templates/{owner}/{repo}/installs": {
				"post": {
					OperationID: "installTemplate",
					Summary:     "Install a template",
					Description: "Checks everything first — the file, the names, the answers — and creates nothing unless all of it passes. " +
						"Then records the install and runs it in the background: project and environment when they do not exist, databases, stores, apps, domains, attachments, variables, and a deploy of each app. " +
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
						"502": openapi.TextResponse("The catalog or GitHub could not be reached."),
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
					Summary:     "List recent installs",
					Tags:        []string{"Templates"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The fifty most recent installs.", openapi.Array(openapi.Ref("TemplateInstall"))),
						"401": openapi.Unauthorized,
					},
				},
			},
			"/template-installs/{id}": {
				"get": {
					OperationID: "getTemplateInstall",
					Summary:     "Get an install",
					Description: "How it is going. Poll it while status is running.",
					Tags:        []string{"Templates"},
					Parameters:  []openapi.Parameter{openapi.PathParam("id", "The install's id.")},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The install.", openapi.Ref("TemplateInstall")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
			},
		},
	}
}
