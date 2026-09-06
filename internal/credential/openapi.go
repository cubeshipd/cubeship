package credential

import (
	"cubeship/internal/platform/openapi"
)

func (h *Handler) OpenAPI() openapi.Spec {
	idParam := []openapi.Parameter{openapi.PathParam("id", "The credential's id.")}

	const secretNote = "\n\nThe secret is stored as given, because a provider takes the secret itself and a hash could not be sent to one. **No endpoint returns it**, and there is none that could: the daemon is what talks to the provider, so nothing outside it needs to read one back. A credential you cannot read is one that cannot leak through a screen somebody left open."

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Credentials",
			Description: "The accounts this instance is wired to, and the secrets that reach them. One secret is stored once: an AWS access key is the same key whether Route 53 writes a record with it or ECR is pulled from with it, so it is entered once and used by both. What a credential may be used for follows from its provider — there is nothing to tick.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Credential": openapi.Object(map[string]*openapi.Schema{
				"id":            openapi.Integer(""),
				"provider":      {Type: "string", Enum: []string{"aws", "cloudflare", "digitalocean", "generic"}, Description: "Whose account this reaches. Permanent: what a credential is for is what its secret is, and a provider changed under a stored secret would be that secret offered to somebody it was never issued for."},
				"provider_name": openapi.String("The provider as a person calls it, so a client needs no table of its own."),
				"label":         openapi.String("What tells two credentials apart, and the only thing here somebody chooses. Unique on the instance."),
				"username":      openapi.String("The first half of the secret, where the provider has one — an access key id, a registry login. Absent for a provider whose secret is a single value. Not a secret: the secret is the other half."),
				"capabilities":  openapi.Array(&openapi.Schema{Type: "string", Enum: []string{"dns", "registry"}, Description: "Something this credential can be used for."}),
				"in_use_by":     openapi.Array(openapi.String("Something currently depending on this credential — what a delete would refuse over.")),
				"created_at":    {Type: "string", Format: "date-time"},
				"updated_at":    {Type: "string", Format: "date-time"},
			}, "id", "provider", "provider_name", "label", "capabilities", "created_at", "updated_at"),

			"CredentialProvider": openapi.Object(map[string]*openapi.Schema{
				"provider":       openapi.String(""),
				"name":           openapi.String("The provider as a person calls it."),
				"capabilities":   openapi.Array(openapi.String("What a credential of this provider may be used for.")),
				"username_label": openapi.String("What to call the first field. Absent for a provider whose secret is a single value — then there is no first field, and asking for one would be asking for something that does not exist."),
				"password_label": openapi.String("What to call the secret field."),
				"hint":           openapi.String("Where to get one, and what it needs to be allowed to do."),
			}, "provider", "name", "capabilities", "password_label", "hint"),
		},
		Paths: map[string]openapi.PathItem{
			"/credentials": {
				"post": {
					OperationID: "createCredential",
					Summary:     "Store a credential",
					Description: "What is asked for depends on the provider, and the two refusals are symmetrical: a provider whose secret has two halves must have both, and one whose secret is a single value must not be given a name to go with it. Dropping the extra silently is how a credential comes out different from what somebody thought they stored." + secretNote,
					Tags:        []string{"Credentials"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"provider": {Type: "string", Enum: []string{"aws", "cloudflare", "digitalocean", "generic"}, Description: "GET /credentials/providers lists these, and what each one asks for."},
						"label":    openapi.String("How you will recognise it. \"the AWS one\" stops identifying anything the moment there are two."),
						"username": openapi.String("The first half, for a provider that has one. Refused for a provider whose secret is a single value."),
						"password": openapi.String("The secret."),
					}, "provider", "label", "password")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The stored credential, without its secret.", openapi.Ref("Credential")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("A credential with that label already exists."),
					},
				},
				"get": {
					OperationID: "listCredentials",
					Summary:     "List credentials",
					Description: "Every credential on the instance, or every one that can do a given job. `in_use_by` says what is currently depending on each — what a delete would refuse over, said before anybody tries.",
					Tags:        []string{"Credentials"},
					Parameters: []openapi.Parameter{
						openapi.QueryParam("capability", "Only credentials that can do this: `dns` or `registry`. This is what a feature's own screen asks for — the DNS page offers the accounts that can write records, not every secret on the instance."),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The credentials.", openapi.Array(openapi.Ref("Credential"))),
						"400": openapi.TextResponse("No such capability."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
			"/credentials/providers": {
				"get": {
					OperationID: "listCredentialProviders",
					Summary:     "List the providers a credential can be for",
					Description: "What to put in the create form: what each provider is called, what its two fields are called — or its one — and what it may be used for. Read it rather than hard-coding a list, since what a provider can do is decided by which clients this release actually has.",
					Tags:        []string{"Credentials"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The providers.", openapi.Array(openapi.Ref("CredentialProvider"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
			credentialPath: {
				"patch": {
					OperationID: "updateCredential",
					Summary:     "Rename a credential, or rotate its secret",
					Description: "A field left out is left alone, so renaming one cannot blank its password.\n\n**This is where a rotation happens, and it happens once.** Every registry authenticating with this credential follows — before credentials existed, the same token had to be re-entered once per thing that used it.\n\nNot the provider: what a credential is for is what its secret is, so moving one to another provider means adding a credential and deleting this.",
					Tags:        []string{"Credentials"},
					Parameters:  idParam,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"label":    openapi.String(""),
						"username": openapi.String("The first half, where the provider has one."),
						"password": openapi.String("The new secret."),
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The updated credential.", openapi.Ref("Credential")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("Another credential already has that label."),
					},
				},
				"delete": {
					OperationID: "deleteCredential",
					Summary:     "Delete a credential",
					Description: "Refused while anything still authenticates with it, and the refusal names what — \"in use\" that does not say by what is a refusal somebody has to go hunting to satisfy.\n\nRefused rather than cascaded, and rather than orphaned: a registry whose login vanished is a registry that cannot log in, and the way that surfaces is a deploy failing minutes later with nobody watching.",
					Tags:        []string{"Credentials"},
					Parameters:  idParam,
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("Something is still using it, named in the message."),
					},
				},
			},
		},
	}
}
