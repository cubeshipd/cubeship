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
			Description: "Who the API key you are calling with belongs to.",
		}},
		Schemas: map[string]*openapi.Schema{
			"WhoAmI": openapi.Object(map[string]*openapi.Schema{
				"username":       openapi.String("The account this API key belongs to."),
				"is_super_admin": openapi.Bool("Whether this account can act in every organization."),
			}, "username", "is_super_admin"),
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
		},
	}
}
