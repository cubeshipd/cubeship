package user

import "cubeship/internal/platform/openapi"

// OpenAPI describes this module's endpoints. It sits beside Routes on
// purpose: adding one without the other fails the parity test in
// internal/server.
func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Identity",
			Description: "Who you are, and the API keys you authenticate with.",
		}},
		Schemas: map[string]*openapi.Schema{
			"WhoAmI": openapi.Object(map[string]*openapi.Schema{
				"username":       openapi.String("The account this API key belongs to."),
				"is_super_admin": openapi.Bool("Whether this account can act in every organization."),
			}, "username", "is_super_admin"),

			"APIKey": openapi.Object(map[string]*openapi.Schema{
				"id":           openapi.Integer("Pass this to DELETE /users/me/api-keys/{id} to revoke the key."),
				"name":         openapi.String(`A label chosen at creation, e.g. "mcp" or "laptop".`),
				"created_at":   {Type: "string", Format: "date-time"},
				"last_used_at": {Type: "string", Format: "date-time", Nullable: true, Description: "Absent until the key authenticates a request."},
				"current_key":  openapi.Bool("True for the key this very request authenticated with."),
			}, "id", "name", "created_at", "current_key"),

			"NewAPIKey": openapi.Object(map[string]*openapi.Schema{
				"id":      openapi.Integer(""),
				"name":    openapi.String(""),
				"api_key": openapi.String("The key itself. This is the only time it is ever returned — store it now."),
			}, "id", "name", "api_key"),
		},
		Paths: map[string]openapi.PathItem{
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
			"/users/me/api-key/rotate": {
				"post": {
					OperationID: "rotateCurrentAPIKey",
					Summary:     "Replace the key you are using right now",
					Description: "Revokes exactly the key this request authenticated with and issues a replacement carrying the same name. **Every other key you hold is untouched** — that is the point of holding more than one: rotating your terminal's key must never invalidate an agent's.\n\nThe old key stops working immediately, so save the new one before the next call.",
					Tags:        []string{"Identity"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The replacement key.", openapi.Object(map[string]*openapi.Schema{
							"api_key": openapi.String("The new key, shown once."),
						}, "api_key")),
						"401": openapi.Unauthorized,
					},
				},
			},
			"/users/me/api-keys": {
				"post": {
					OperationID: "createAPIKey",
					Summary:     "Issue an additional API key",
					Description: "Creates a key independent of every key you already hold. This is how an MCP client gets a credential of its own, separate from the one your terminal uses.",
					Tags:        []string{"Identity"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"name": openapi.String(`A label to recognize the key by later, e.g. "mcp".`),
					}, "name")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The new key, shown once.", openapi.Ref("NewAPIKey")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
					},
				},
				"get": {
					OperationID: "listAPIKeys",
					Summary:     "List your API keys",
					Description: "Metadata only. Key values are never returned again after creation.",
					Tags:        []string{"Identity"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("Every key you hold.", openapi.Array(openapi.Ref("APIKey"))),
						"401": openapi.Unauthorized,
					},
				},
			},
			"/users/me/api-keys/{id}": {
				"delete": {
					OperationID: "revokeAPIKey",
					Summary:     "Revoke one of your API keys",
					Description: "Refused if it is your last remaining key: revoking it would lock you out with no way back.",
					Tags:        []string{"Identity"},
					Parameters:  []openapi.Parameter{openapi.PathParam("id", "The key's id, from GET /users/me/api-keys.")},
					Responses: openapi.Responses{
						"200": openapi.Empty("The key is revoked."),
						"400": openapi.TextResponse("The id is not a number."),
						"401": openapi.Unauthorized,
						"404": openapi.TextResponse("No key with that id belongs to you."),
						"409": openapi.TextResponse("That is your only remaining key."),
					},
				},
			},
		},
	}
}
