package registry

import "cubeship/internal/platform/openapi"

// OpenAPI documents what a person calls. The registry container's own
// two endpoints — the token realm and the push webhook — are not here:
// Docker and the registry call those, nobody else, and they are
// registered as internal for that reason.
func (h *Handler) OpenAPI() openapi.Spec {
	orgParam := openapi.PathParam("orgSlug", "Organization slug.")

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "Registry",
			Description: "What an organization has pushed to Cubeship's own registry. " +
				"It needs no credential of its own — a push authenticates with the pusher's API key — which is why it is not among the registry logins.",
		}},
		Schemas: map[string]*openapi.Schema{
			"PushedRepository": openapi.Object(map[string]*openapi.Schema{
				"name": openapi.String("The repository path, which is also an app's reference: org/project/environment/app."),
			}, "name"),
			"PushedImage": openapi.Object(map[string]*openapi.Schema{
				"tag": openapi.String(""),
			}, "tag"),
		},
		Paths: map[string]openapi.PathItem{
			"/registry/repositories": {
				"get": {
					OperationID: "listPushedRepositories",
					Summary:     "List what this organization has pushed",
					Description: "A repository path here is an app's reference, so an organization's own are exactly those under its slug — which is also what keeps one organization from reading another's out of a registry that has no idea organizations exist.\n\n" +
						"Empty before anything is pushed, and while the instance has no domain: the registry runs either way, but nothing can reach it to push.",
					Tags:       []string{"Registry"},
					Parameters: []openapi.Parameter{orgParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The repositories.", openapi.Array(openapi.Ref("PushedRepository"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"503": openapi.TextResponse("The registry is not reachable from the daemon."),
					},
				},
				"delete": {
					OperationID: "deletePushedRepository",
					Summary:     "Delete a repository and every tag in it",
					Description: "There is no delete-a-repository in the Registry API: a repository is whatever its manifests say it is, and it stops existing once they are gone. The empty name lingers in the catalogue until a collection clears it.\n\n" +
						"This frees no disk on its own — see the collection endpoint. Organization admins only.",
					Tags:       []string{"Registry"},
					Parameters: []openapi.Parameter{orgParam, openapi.QueryParam("repository", "Which repository.")},
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"503": openapi.TextResponse("The registry is not reachable from the daemon."),
					},
				},
			},
			"/registry/garbage-collect": {
				"post": {
					OperationID: "collectRegistryGarbage",
					Summary:     "Reclaim the disk deleted images left behind",
					Description: "Deleting a tag makes an image unreachable and frees nothing: the layers stay on disk, referenced by nothing, until something walks the storage. This is that walk.\n\n" +
						"**The registry is stopped for the duration.** The pass marks which blobs are referenced and then deletes the rest, so a push arriving in between can have its own blobs deleted underneath it — the registry is stopped rather than trusted to interleave. It is started again afterwards, including when the pass fails. Pushes during the pass are refused outright, which `docker push` retries.\n\n" +
						"Nothing schedules this. Organization admins only, and instance-wide: the storage has no notion of organizations.",
					Tags:       []string{"Registry"},
					Parameters: []openapi.Parameter{orgParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("What the pass did.", openapi.Object(map[string]*openapi.Schema{
							"blobs_deleted": openapi.Integer("How many blobs it removed."),
							"output":        openapi.String("The pass's transcript, which is the only record of what went."),
							"duration":      openapi.String("How long the registry was down for it."),
						}, "blobs_deleted", "output", "duration")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"503": openapi.TextResponse("This daemon cannot reach the registry container to run maintenance on it."),
					},
				},
			},
			"/registry/images": {
				"get": {
					OperationID: "listPushedImages",
					Summary:     "List one repository's tags",
					Description: "Naming a repository outside the organization is the same request as naming one that does not exist, and gets the same answer.",
					Tags:        []string{"Registry"},
					Parameters: []openapi.Parameter{
						orgParam,
						openapi.QueryParam("repository", "Which repository, as the listing above names it."),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The tags.", openapi.Array(openapi.Ref("PushedImage"))),
						"400": openapi.TextResponse("No repository was named."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"503": openapi.TextResponse("The registry is not reachable from the daemon."),
					},
				},
				"delete": {
					OperationID: "deletePushedImage",
					Summary:     "Delete one tag",
					Description: "The Registry API deletes by digest rather than by tag, so the tag is resolved first — which means every other tag pointing at the same image goes with it. That is the registry's behaviour rather than a choice here.\n\n" +
						"This frees no disk on its own — see the collection endpoint. Organization admins only.",
					Tags: []string{"Registry"},
					Parameters: []openapi.Parameter{
						orgParam,
						openapi.QueryParam("repository", "Which repository."),
						openapi.QueryParam("tag", "Which tag."),
					},
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"400": openapi.TextResponse("No tag was named."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"503": openapi.TextResponse("The registry is not reachable from the daemon."),
					},
				},
			},
		},
	}
}
