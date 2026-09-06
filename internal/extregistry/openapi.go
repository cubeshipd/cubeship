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
			"RegistryUsage": openapi.Object(map[string]*openapi.Schema{
				"total_bytes":          openapi.Integer("What every image adds up to."),
				"counts_shared_layers": openapi.Bool("Always true, and in the payload so the figure cannot be presented as exact without having seen it. Layers are shared between images: two tags built from one base count that base twice. It is an upper bound on what is stored, not what is billed."),
				"repositories": openapi.Array(openapi.Object(map[string]*openapi.Schema{
					"name":   openapi.String(""),
					"bytes":  openapi.Integer("The repository's images, counting each distinct image once however many tags point at it."),
					"images": openapi.Integer("Distinct images, not tags."),
				}, "name", "bytes", "images")),
			}, "total_bytes", "counts_shared_layers", "repositories"),
			"RegistryRepository": openapi.Object(map[string]*openapi.Schema{
				"name": openapi.String("The repository, as the registry names it."),
			}, "name"),
			"RegistryImage": openapi.Object(map[string]*openapi.Schema{
				"tag":       openapi.String("The tag, or <untagged> for an image no tag points at."),
				"digest":    openapi.String("Identifies the image itself. A tag can move; this does not."),
				"size":      openapi.Integer("Bytes, where the registry reports one."),
				"pushed_at": openapi.String("RFC 3339, where the registry reports it."),
			}, "tag"),
			"RegistryCredential": openapi.Object(map[string]*openapi.Schema{
				"id":            openapi.Integer("Identifies it in the paths below."),
				"credential_id": openapi.Integer("The stored account this registry authenticates as, and where its secret lives. Rotating that secret is one edit there, and every registry using it follows."),
				"provider":      {Type: "string", Enum: []string{"generic", "digitalocean", "aws"}, Description: "Which registry this is for, and how the daemon authenticates with it. The registry's own, not the credential's: a credential is a secret, and the same DigitalOcean token may be reaching two different things. Fixed once the row exists, like the host it decides."},
				"host":          openapi.String("The registry it is for, and its identity: one per host per organization. An image whose reference starts with this host pulls through it. Docker Hub is index.docker.io, which is also what an image with no host in its name resolves to."),
				"namespace":     openapi.String("The path segment between the host and the image, where the provider has one — DigitalOcean's registry name. Not part of matching."),
				"region":        openapi.String("AWS only."),
				"username":      openapi.String("The login's user, or an AWS access key id. Never the secret, whichever it is."),
				"created_at":    openapi.String("RFC 3339."),
				"updated_at":    openapi.String("RFC 3339. Changes when the login is replaced."),
			}, "id", "credential_id", "provider", "host", "username", "created_at", "updated_at"),
		},
		Paths: map[string]openapi.PathItem{
			"/registries": {
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
						"`provider` is always required — it says which registry this is, and a credential cannot answer it: a credential is a secret, not a kind of registry.\n\n" +
						"The login comes from one of two places. Either `credential_id` names a **stored account**, or a login is typed here (`username`, `password`) and the account is created from it, so it is there to pick next time. A credential is a convenience, not a prerequisite: adding a first registry does not mean going somewhere else to make an account first. Give one or the other, never both.\n\n" +
						"What else is asked for depends on the provider. A **generic** registry takes a host. **DigitalOcean** takes its registry name; the host never varies, so asking for a URL would be asking someone to retype a constant. **AWS** takes a region and nothing else: the host carries the account id and is discovered by the same call that proves the key can read a registry, so a key that cannot is refused here rather than at a deploy.\n\n" +
						"AWS is also the one whose stored secret is not a password. What Docker logs in with is a token fetched from the access key, good for hours; the key is what is kept, in the credential.\n\n" +
						"The host is normalized, so \"https://ghcr.io/\" and \"ghcr.io\" are the same registry.\n\n" +
						"Organization admins only.",
					Tags:       []string{"Registries"},
					Parameters: []openapi.Parameter{orgParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"credential_id": openapi.Integer("The stored account to authenticate as. Any credential on the instance — whether it works is the registry's to refuse. Omit it to type a login instead."),
						"provider":      {Type: "string", Enum: []string{"generic", "digitalocean", "aws"}, Description: "Which registry this is: what it takes for a login, and what else is asked for below."},
						"label":         openapi.String("With a typed login: what to call the account it creates. Derived from the registry when absent, since somebody adding a registry is not necessarily thinking about naming an account."),
						"username":      openapi.String("With a typed login: the user, or an AWS access key id. Not given for a provider whose secret is a single token."),
						"password":      openapi.String("With a typed login: the password, token or secret access key. Stored as given — a hash cannot be sent to a registry — and never returned."),
						"host":          openapi.String("Generic only: the registry, e.g. ghcr.io. Use docker.io for the Hub. Fixed for DigitalOcean and discovered for AWS."),
						"namespace":     openapi.String("DigitalOcean only: the registry's name, which is what follows registry.digitalocean.com/ in an image path."),
						"region":        openapi.String("AWS only: the region the ECR registry lives in. It cannot be guessed."),
					}, "provider")),
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
			"/registries/{id}/status": {
				"get": {
					OperationID: "probeRegistry",
					Summary:     "Ask whether this login still works",
					Description: "A live call to the registry, not something recorded when the login was stored. The interesting case is a credential that used to work — a revoked key, an expired token, a rotated password — none of which tell Cubeship anything: the first sign is a deploy failing to pull.\n\n" +
						"`unauthorized` is fixed by storing a new login; `unreachable` is someone else's registry being down. Organization admins only.",
					Tags:       []string{"Registries"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("What the probe found.", openapi.Object(map[string]*openapi.Schema{
							"state": {
								Type:        "string",
								Description: "available, unauthorized, or unreachable.",
								Enum:        []string{"available", "unauthorized", "unreachable"},
							},
							"detail": openapi.String("Why, for anything but available."),
						}, "state")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			"/registries/{id}/repositories": {
				"delete": {
					OperationID: "deleteRegistryRepository",
					Summary:     "Delete a repository and everything in it",
					Description: "Every tag goes with it. Apps pulling from it keep running — a container already exists — and their next deploy fails. Organization admins only.",
					Tags:        []string{"Registries"},
					Parameters:  []openapi.Parameter{orgParam, idParam, openapi.QueryParam("repository", "Which repository.")},
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"400": openapi.TextResponse("No repository was named."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"501": openapi.TextResponse("This registry does not support deleting through Cubeship."),
					},
				},
				"get": {
					OperationID: "listRegistryRepositories",
					Summary:     "List what a registry holds",
					Description: "Not every registry can say. The Registry v2 API defines a catalogue endpoint and the two biggest public registries — Docker Hub and GitHub's — disable it; those answer 501, which is their answer rather than a failure here, and beats an empty list that reads as \"you have nothing\".\n\n" +
						"Organization admins only.",
					Tags:       []string{"Registries"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The repositories.", openapi.Array(openapi.Ref("RegistryRepository"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"501": openapi.TextResponse("This registry does not list what it holds."),
					},
				},
			},
			"/registries/{id}/images": {
				"delete": {
					OperationID: "deleteRegistryImage",
					Summary:     "Delete one image",
					Description: "Name it with `tag` where it has one: deleting by tag leaves every other tag on the same image alone, and a tag is what was picked off a list.\n\n" +
						"`digest` is for an image with no tag at all. Those cannot be named any other way — a stand-in name would be one two of them share, and no delete could reach either.\n\n" +
						"Whether it frees anything is the registry's business — ECR reclaims the storage. What is promised is that nothing can pull that image afterwards. Organization admins only.",
					Tags: []string{"Registries"},
					Parameters: []openapi.Parameter{
						orgParam, idParam,
						openapi.QueryParam("repository", "Which repository."),
						openapi.QueryParam("tag", "Which tag, for an image that has one."),
						openapi.QueryParam("digest", "Which image, for one with no tag."),
					},
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"400": openapi.TextResponse("No repository, or neither a tag nor a digest, was named."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"501": openapi.TextResponse("This registry does not support deleting through Cubeship."),
					},
				},
				"get": {
					OperationID: "listRegistryImages",
					Summary:     "List one repository's images",
					Description: "An image with no tag comes back with an empty `tag` and a `digest`: it still occupies the registry, and hiding it would make this listing disagree with the registry's own. The digest is the only thing that identifies one, and the only way to delete one.\n\nOrganization admins only.",
					Tags:        []string{"Registries"},
					Parameters: []openapi.Parameter{
						orgParam, idParam,
						openapi.QueryParam("repository", "Which repository, as the listing above names it."),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The images.", openapi.Array(openapi.Ref("RegistryImage"))),
						"400": openapi.TextResponse("No repository was named."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"501": openapi.TextResponse("This registry does not list what it holds."),
					},
				},
			},
			"/registries/{id}/usage": {
				"get": {
					OperationID: "measureRegistryUsage",
					Summary:     "Measure what a registry's images add up to",
					Description: "Its own endpoint rather than part of the listing, because it is one call per repository — a listing that waited for this would wait for all of them, and there can be hundreds.\n\n" +
						"The figure double-counts layers shared between images, which is what `counts_shared_layers` says. Organization admins only.",
					Tags:       []string{"Registries"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("What it holds.", openapi.Ref("RegistryUsage")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"501": openapi.TextResponse("This registry does not report what it holds."),
					},
				},
			},
			"/registries/{id}": {
				"put": {
					OperationID: "replaceRegistryCredential",
					Summary:     "Re-point a registry at another account",
					Description: "Re-point a registry at a different account, or correct a registry name typed by hand.\n\n" +
						"The **host** is still not editable — an app's pulls are matched to a registry by host, so changing it in place would silently send them somewhere else. Delete it and add the new one.\n\n" +
						"`namespace` is DigitalOcean's registry name, typed by hand rather than derived, so a typo is worth correcting in place. Omit the field to leave it alone; sending an empty string is refused.\n\n" +
						"**Rotation is not here.** A secret is rotated on the credential, once, and every registry authenticating with it follows — which is what credentials were pulled out of here for.\n\n" +
						"Organization admins only.",
					Tags:       []string{"Registries"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"credential_id": openapi.Integer("The account to authenticate as instead. Not given together with a login: one moves this registry, the other moves every registry on the account it has."),
						"username":      openapi.String("Rotates the login **on the account this registry authenticates as**, so every registry on that account follows. Pointing only this registry somewhere else is `credential_id` instead."),
						"password":      openapi.String("The new secret. Stored as given and never returned, so replacing it means entering it in full."),
						"namespace":     openapi.String("DigitalOcean only: the registry's name, which is the path segment between the host and the image. Omit to leave it unchanged."),
					})),
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
