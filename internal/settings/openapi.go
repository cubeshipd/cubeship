package settings

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "Instance",
			Description: "Configuration of the Cubeship instance itself, rather than anything inside it. " +
				"A fresh install has none of it: the daemon starts, is reached by IP, and is configured from here afterwards.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Settings": openapi.Object(map[string]*openapi.Schema{
				"domain":        openapi.String("Base domain. The API is served at api.<domain> and the registry at registry.<domain>; both must resolve to this host. Empty until configured."),
				"acme_email":    openapi.String("Contact address Let's Encrypt registers for expiry notices. Empty until configured."),
				"registry_host": openapi.String("Where a `docker push` goes. Absent while no domain is set — there is nowhere to push yet."),
				"tls_enabled":   openapi.Bool("Whether certificates can be issued, which needs both a domain and a contact address. While false, apps are served over plain HTTP."),
			}, "domain", "acme_email", "tls_enabled"),
		},
		Paths: map[string]openapi.PathItem{
			"/settings": {
				"get": {
					OperationID: "getSettings",
					Summary:     "Read the instance's configuration",
					Description: "Readable by any authenticated caller: a dashboard needs to know whether a domain is configured to tell someone where to push, and none of these values is a secret.",
					Tags:        []string{"Instance"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The instance's configuration.", openapi.Ref("Settings")),
						"401": openapi.Unauthorized,
					},
				},
				"put": {
					OperationID: "setSettings",
					Summary:     "Change the instance's configuration",
					Description: "Only the fields you send are changed; the rest are left alone.\n\n" +
						"Applying a change reconfigures the containers that depend on it — setting a domain brings the registry up, setting a contact address gives Traefik a certificate resolver — which replaces those containers and costs a few seconds of downtime for them.\n\n" +
						"Apps already running keep the routing they were deployed with. Redeploy them to serve over HTTPS.\n\n" +
						"Super-admin only: this is the VPS's configuration, not an organization's.",
					Tags: []string{"Instance"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"domain":     openapi.String("Base domain, e.g. example.com."),
						"acme_email": openapi.String("Contact address for Let's Encrypt."),
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The configuration as it now stands.", openapi.Ref("Settings")),
						"400": openapi.TextResponse("No recognized field was given."),
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("You are not a super-admin."),
					},
				},
			},
		},
	}
}
