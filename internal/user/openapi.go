package user

import "cubeship/internal/platform/openapi"

// OpenAPI describes this module's documented endpoints. It sits beside
// Routes on purpose: adding one without the other fails the parity test
// in internal/server.
//
// Managing your own API keys is absent by design — those routes are
// registered with HandleInternal. You create and rotate keys once, from
// the CLI or an MCP client; nobody integrates against them, and listing
// them in a public reference only invites someone to try.
func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Identity",
			Description: "Who the API key you are calling with belongs to, and who else may reach this instance.",
		}},
		Schemas: map[string]*openapi.Schema{
			"WhoAmI": openapi.Object(map[string]*openapi.Schema{
				"username": openapi.String("The account this API key belongs to."),
				"role":     openapi.String("Either `admin` or `member`."),
			}, "username", "role"),
			"User": openapi.Object(map[string]*openapi.Schema{
				"username":   openapi.String("The account."),
				"role":       openapi.String("Either `admin` or `member`."),
				"created_at": openapi.String("RFC 3339."),
			}, "username", "role", "created_at"),
			"Users": openapi.Object(map[string]*openapi.Schema{
				"users": openapi.Array(openapi.Ref("User")),
			}, "users"),
			"RevokedCredentials": openapi.Object(map[string]*openapi.Schema{
				"api_keys": openapi.Integer("How many keys were revoked."),
				"sessions": openapi.Integer("How many sessions were ended."),
			}, "api_keys", "sessions"),
			"NewUser": openapi.Object(map[string]*openapi.Schema{
				"username": openapi.String("The account that was created."),
				"role":     openapi.String("Either `admin` or `member`."),
				"api_key":  openapi.String("The key it authenticates with, shown exactly once."),
			}, "username", "role", "api_key"),
		},
		Paths: map[string]openapi.PathItem{
			"/users": {
				"get": {
					OperationID: "listUsers",
					Summary:     "List the accounts on this instance",
					Description: "Who can reach this instance at all. There is one instance and no tenant boundary, so this is the whole roster.\n\nAdmin only. Never returns a key or a hash: a key is shown once, when the account is created.",
					Tags:        []string{"Identity"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The accounts.", openapi.Ref("Users")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
				"post": {
					OperationID: "createUser",
					Summary:     "Create an account",
					Description: "Creates an account and returns the API key it authenticates with, **shown exactly once**. There is no second endpoint that reveals it, and no password: an account gets one when it sets one.\n\nAdmin only. An account is a way into this instance, so handing out the ability to mint them would hand out the instance.",
					Tags:        []string{"Identity"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"username": openapi.String("Lowercase letters, digits and dashes. Also the account's docker login user."),
						"role":     openapi.String("`admin` or `member`. Defaults to `member`."),
					}, "username")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The account and its key.", openapi.Ref("NewUser")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("That username is already taken."),
					},
				},
			},
			"/users/{username}": {
				"delete": {
					OperationID: "deleteUser",
					Summary:     "Delete an account",
					Description: "What a person leaving looks like: the account goes, and with it every API key and every session it holds — in one transaction, so nothing that authenticates outlives the account it belonged to.\n\nAdmin only. Deleting the account you are signed in as is refused, and so is deleting the only admin: an instance with no admin can never configure itself again, and nothing here could put one back.",
					Tags:        []string{"Identity"},
					Parameters:  []openapi.Parameter{openapi.PathParam("username", "The account to delete.")},
					Responses: openapi.Responses{
						"204": openapi.Empty("The account and its credentials are gone."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such account."),
						"409": openapi.TextResponse("That is the account you are signed in as, or the only admin on the instance."),
					},
				},
			},
			"/users/{username}/credentials": {
				"delete": {
					OperationID: "revokeUserCredentials",
					Summary:     "Revoke everything an account authenticates with",
					Description: "Ends every session and revokes every API key the account holds, and leaves the account itself. This is the answer to a laptop that walked off: what was on it stops working everywhere at once, without the account having to be deleted and made again.\n\nThe password is not touched — it is a secret in somebody's head, not a credential lying on the machine that was lost — so signing in again is how the account comes back. Admin only.",
					Tags:        []string{"Identity"},
					Parameters:  []openapi.Parameter{openapi.PathParam("username", "The account whose credentials to revoke.")},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("How much was revoked.", openapi.Ref("RevokedCredentials")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such account."),
					},
				},
			},
			"/users/me": {
				"get": {
					OperationID: "whoAmI",
					Summary:     "Identify the caller",
					Description: "Reports the account the API key belongs to. `cubeship registry login` uses this to learn the username to log Docker in as — the saved credentials file only ever stores the key itself.",
					Tags:        []string{"Identity"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The caller's identity.", openapi.Ref("WhoAmI")),
						"401": openapi.Unauthorized,
					},
				},
			},
		},
	}
}
