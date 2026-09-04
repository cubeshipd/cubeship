package extregistry

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	orgParam := openapi.PathParam("orgSlug", "Organization slug.")
	idParam := openapi.PathParam("id", "Credential id.")

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "Registries",
			Description: "Logins for registries Cubeship does not run — Docker Hub, GitHub, DigitalOcean, ECR. " +
				"An app with source \"external\" pulls through whichever of these matches the registry its image lives in. " +
				"Cubeship's own registry needs none of this: it authenticates each user with their API key.",
		}},
		Schemas: map[string]*openapi.Schema{
			"RegistryCredential": openapi.Object(map[string]*openapi.Schema{
				"id":         openapi.Integer("Identifies it in the paths below."),
				"name":       openapi.String("What you call it. Unique within the organization."),
				"host":       openapi.String("The registry it is for, normalized — an image whose reference starts with this host pulls through it. Docker Hub is index.docker.io, which is also what an image with no host in its name resolves to."),
				"username":   openapi.String("The login's user."),
				"created_at": openapi.String("RFC 3339."),
				"updated_at": openapi.String("RFC 3339. Changes when the login is replaced."),
			}, "id", "name", "host", "username", "created_at", "updated_at"),
		},
		Paths: map[string]openapi.PathItem{
			"/orgs/{orgSlug}/registries": {
				"get": {
					OperationID: "listRegistryCredentials",
					Summary:     "List an organization's registry logins",
					Description: "Passwords are never returned, here or anywhere. Organization admins only.",
					Tags:        []string{"Registries"},
					Parameters:  []openapi.Parameter{orgParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The organization's registry logins.", openapi.Array(openapi.Ref("RegistryCredential"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"post": {
					OperationID: "createRegistryCredential",
					Summary:     "Add a registry login",
					Description: "One login per registry per organization: two would make \"which one does this pull use\" a question with no answer.\n\n" +
						"The host is normalized, so \"https://registry.digitalocean.com/\" and \"registry.digitalocean.com\" are the same registry.\n\n" +
						"Organization admins only.",
					Tags:       []string{"Registries"},
					Parameters: []openapi.Parameter{orgParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"name":     openapi.String("What you call it, e.g. \"DigitalOcean\"."),
						"host":     openapi.String("The registry, e.g. registry.digitalocean.com. Use docker.io for the Hub."),
						"username": openapi.String(""),
						"password": openapi.String("An access token wherever the registry offers one. Stored as given — it has to be sent to the registry — and never returned."),
					}, "name", "host", "username", "password")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The new login.", openapi.Ref("RegistryCredential")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This organization already has a login with that name, or for that registry."),
					},
				},
			},
			"/orgs/{orgSlug}/registries/{id}": {
				"put": {
					OperationID: "replaceRegistryCredential",
					Summary:     "Replace a registry login",
					Description: "Rotation: the host stays, the username and password are replaced.\n\n" +
						"A credential cannot be re-pointed at a different registry — that is a different credential, and changing it in place would silently start authenticating an app's pulls somewhere else. Delete it and add the new one.\n\n" +
						"Organization admins only.",
					Tags:       []string{"Registries"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"username": openapi.String(""),
						"password": openapi.String(""),
					}, "username", "password")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The login as it now stands.", openapi.Ref("RegistryCredential")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "deleteRegistryCredential",
					Summary:     "Delete a registry login",
					Description: "Apps that pulled through it keep running — a container already exists — but their next deploy pulls anonymously and will fail if the image is private.\n\n" +
						"Organization admins only.",
					Tags:       []string{"Registries"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
		},
	}
}
