package registry

import "cubeship/internal/platform/openapi"

// SecurityScheme names for the two credentials this module accepts,
// neither of which is the API's usual bearer key.
const (
	BasicAuthScheme    = "registryBasicAuth"
	WebhookTokenScheme = "registryWebhookToken"
)

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Registry",
			Description: "The embedded container registry's own two endpoints. You do not call these; `docker` and the registry container do. They are documented because they are part of the daemon's surface, and because knowing what authenticates them matters when you configure a firewall.",
		}},
		Paths: map[string]openapi.PathItem{
			"/v2/token": {
				"get": {
					OperationID: "issueRegistryToken",
					Summary:     "Exchange an API key for a registry token",
					Description: "The token realm the registry's config points at. `docker login`, `docker push` and `docker pull` call this with your username and API key as HTTP Basic credentials, plus the scope they want.\n\nThe returned token grants only what your organization membership allows, and only the `pull` and `push` actions — an action Cubeship does not grant is dropped from the token rather than failing the request, which is how the registry spec expresses a denial. A scope naming an organization you do not belong to comes back with no access at all.",
					Tags:        []string{"Registry"},
					Security:    openapi.Requires(BasicAuthScheme),
					Parameters: []openapi.Parameter{
						openapi.QueryParam("scope", `What the client wants, e.g. "repository:acme/myapp:pull,push". May be repeated.`),
						openapi.QueryParam("service", "The registry service name. Sent by Docker; ignored."),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("A short-lived bearer token for the registry.", openapi.Object(map[string]*openapi.Schema{
							"token":        openapi.String("The JWT."),
							"access_token": openapi.String("The same JWT, under the name some Docker versions expect."),
							"expires_in":   openapi.Integer("Seconds until the token expires."),
						}, "token", "access_token", "expires_in")),
						"401": openapi.TextResponse("The Basic credentials are missing, or the username does not match the API key."),
						"503": openapi.TextResponse("The daemon has no registry signing key yet."),
					},
				},
			},
			"/hooks/registry": {
				"post": {
					OperationID: "registryWebhook",
					Summary:     "Receive a push notification from the registry",
					Description: "The registry calls this when an image is pushed, and it is what makes a push deploy. It is authenticated by the daemon's own system token, which the registry's config sends as a static header — not by any user's API key.\n\nThe deploy runs in the background: this returns as soon as the notification is accepted, because the registry gives up after 5 seconds and a real deploy takes longer.\n\nA malformed body is answered 200, since retrying would not fix it.",
					Tags:        []string{"Registry"},
					Security:    openapi.Requires(WebhookTokenScheme),
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"events": openapi.Array(openapi.Object(map[string]*openapi.Schema{
							"action": openapi.String(`Only "push" triggers a deploy.`),
							"target": openapi.Object(map[string]*openapi.Schema{
								"repository": openapi.String("e.g. acme/myapp"),
								"tag":        openapi.String("e.g. latest"),
							}),
						})),
					})),
					Responses: openapi.Responses{
						"200": openapi.Empty("The notification was accepted. Any deploy it triggered runs in the background."),
						"401": openapi.TextResponse("The Authorization header does not carry the daemon's system token."),
					},
				},
			},
		},
	}
}
