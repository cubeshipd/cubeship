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
			"NewUser": openapi.Object(map[string]*openapi.Schema{
				"username": openapi.String("The account that was created."),
				"role":     openapi.String("Either `admin` or `member`."),
				"api_key":  openapi.String("The key it authenticates with, shown exactly once."),
			}, "username", "role", "api_key"),
		},
		Paths: map[string]openapi.PathItem{
			"/users": {
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
