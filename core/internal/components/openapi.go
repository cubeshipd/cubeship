package components

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{Name: "Components", Description: "Cubeship's own services, by machine. Administrator only; user workloads are excluded."}},
		Schemas: map[string]*openapi.Schema{
			"Component": openapi.Object(map[string]*openapi.Schema{
				"id": openapi.String("Stable component identifier."), "name": openapi.String("Component name."),
				"description": openapi.String("Role on this machine."), "container": openapi.String("Fixed Docker container name."),
				"image": openapi.String("Container image reference."), "status": openapi.String("Docker state, not-installed or unavailable."),
				"health": openapi.String("Docker health check state, when configured."), "started_at": openapi.String("Container start time."),
				"restarts": openapi.Integer("Docker restart count."), "logs_available": openapi.Bool("Whether a container exists to read logs from."),
				"detail": openapi.String("Why the component is unavailable."),
			}, "id", "name", "container", "status", "logs_available"),
			"ComponentInventory": openapi.Object(map[string]*openapi.Schema{"server": openapi.String("Machine name."), "components": openapi.Array(openapi.Ref("Component"))}, "server", "components"),
		},
		Paths: map[string]openapi.PathItem{
			"/nodes/{name}/components": {"get": {OperationID: "listComponents", Summary: "List Cubeship services on a machine", Tags: []string{"Components"},
				Description: "Administrator only. Workers answer through their authenticated outbound channel. Unreachable workers return 503; their services are never guessed to be stopped.",
				Parameters:  []openapi.Parameter{openapi.PathParam("name", "Machine name from /nodes.")},
				Responses:   openapi.Responses{"200": openapi.JSONResponse("Component inventory.", openapi.Ref("ComponentInventory")), "401": openapi.Unauthorized, "403": openapi.Forbidden, "404": openapi.NotFound, "503": openapi.TextResponse("Machine or agent unavailable.")},
			}},
			"/nodes/{name}/components/{component}/logs": {"get": {OperationID: "getComponentLogs", Summary: "Read a Cubeship component's logs", Tags: []string{"Components"},
				Description: "Administrator only. Plain stdout and stderr, bounded to 2 MiB. Only fixed Cubeship component identifiers are accepted; no arbitrary container or shell access.",
				Parameters:  []openapi.Parameter{openapi.PathParam("name", "Machine name."), openapi.PathParam("component", "Identifier from the component inventory."), openapi.QueryParam("tail", "1 to 5000 lines. Defaults to 200.")},
				Responses:   openapi.Responses{"200": openapi.TextResponse("Component log output."), "400": openapi.BadRequest, "401": openapi.Unauthorized, "403": openapi.Forbidden, "404": openapi.NotFound, "503": openapi.TextResponse("Machine or logs unavailable.")},
			}},
		},
	}
}
