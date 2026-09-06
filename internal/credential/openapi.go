package credential

import (
	"cubeship/internal/platform/openapi"
)

func (h *Handler) OpenAPI() openapi.Spec {
	idParam := []openapi.Parameter{openapi.PathParam("id", "The credential's id.")}

	const secretNote = "\n\nThe secret is stored as given, because whatever uses it takes the secret itself and a hash could not be sent to one. **No endpoint returns it**, and there is none that could: the daemon is what talks to the provider, so nothing outside it needs to read one back. A credential you cannot read is one that cannot leak through a screen somebody left open."

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Credentials",
			Description: "The secrets this instance holds. A credential is a label, an optional first half and a secret — and nothing else. It carries no provider, because most API tokens can only be read at the moment they are issued: a secret filed under the one job it may ever do would have to be issued again for the second. Which API is spoken with it belongs to the use — a registry, a DNS provider — and one credential may be named by any number of them.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Credential": openapi.Object(map[string]*openapi.Schema{
				"id":         openapi.Integer(""),
				"label":      openapi.String("What tells two credentials apart, and the only thing here somebody chooses. Unique on the instance."),
				"username":   openapi.String("The first half of the secret, where it has one — an access key id, a registry login. Absent for a secret that is a single value. Not a secret: the secret is the other half."),
				"in_use_by":  openapi.Array(openapi.String("Something currently depending on this credential — what a delete would refuse over.")),
				"created_at": {Type: "string", Format: "date-time"},
				"updated_at": {Type: "string", Format: "date-time"},
			}, "id", "label", "created_at", "updated_at"),
		},
		Paths: map[string]openapi.PathItem{
			"/credentials": {
				"post": {
					OperationID: "createCredential",
					Summary:     "Store a credential",
					Description: "A label and a secret, with a first half where the secret has one. Nothing here decides what the credential is for: that is the use's question, asked when the credential is wired to a registry or a DNS provider." + secretNote,
					Tags:        []string{"Credentials"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"label":    openapi.String("How you will recognise it. \"the AWS one\" stops identifying anything the moment there are two."),
						"username": openapi.String("The first half, for a secret that has one — an access key id, a registry login. Leave it out for a bare token."),
						"password": openapi.String("The secret."),
					}, "label", "password")),
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
					Description: "Every credential on the instance. Unfiltered, because a credential is not for one job. `in_use_by` says what is currently depending on each — what a delete would refuse over, said before anybody tries.",
					Tags:        []string{"Credentials"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The credentials.", openapi.Array(openapi.Ref("Credential"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
			credentialPath: {
				"patch": {
					OperationID: "updateCredential",
					Summary:     "Rename a credential, or rotate its secret",
					Description: "A field left out is left alone, so renaming one cannot blank its password.\n\n**This is where a rotation happens, and it happens once.** Everything authenticating with this credential follows — before credentials existed, the same token had to be re-entered once per thing that used it.",
					Tags:        []string{"Credentials"},
					Parameters:  idParam,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"label":    openapi.String(""),
						"username": openapi.String("The first half, where the secret has one."),
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
