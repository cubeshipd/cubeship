package audit

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "Audit",
			Description: "Who changed what on this instance, and through which door: the dashboard, the API, or an agent over MCP.\n\n" +
				"Every change is recorded, and every refused attempt — a read included, so a key trying to read a secret shows up. " +
				"Request bodies are never kept. Events are kept for 90 days.",
		}},
		Schemas: map[string]*openapi.Schema{
			"AuditEvent": openapi.Object(map[string]*openapi.Schema{
				"id":       openapi.Integer("Pass as `before` to read what came earlier."),
				"at":       openapi.String("RFC 3339."),
				"username": openapi.String("Who. Kept after the account is deleted."),
				"via":      {Type: "string", Enum: []string{"dashboard", "api", "mcp"}, Description: "The door."},
				"key_name": openapi.String("The API key, when one was used."),
				"action":   openapi.String("The route pattern, e.g. `POST /apps/{project}/{env}/{name}/deployments`, or `mcp <tool>`."),
				"target":   openapi.String("What it named: the path, or a tool's identifying arguments."),
				"outcome":  {Type: "string", Enum: []string{"ok", "refused", "failed"}, Description: "`refused` is a role, a key's access, or a project outside a key's scope."},
				"status":   openapi.Integer("The HTTP status. Absent for a tool."),
				"detail":   openapi.String("The error the caller was given, cut to 300 characters."),
				"ip":       openapi.String("The forwarded address behind Traefik, the socket's otherwise."),
			}, "id", "at", "username", "via", "action", "outcome"),
			"AuditPage": openapi.Object(map[string]*openapi.Schema{
				"events": openapi.Array(openapi.Ref("AuditEvent")),
				"next":   openapi.Integer("The `before` for the next page. Absent on the last."),
			}, "events"),
		},
		Paths: map[string]openapi.PathItem{
			"/audit": {
				"get": {
					OperationID: "listAuditEvents",
					Summary:     "Read the audit log",
					Description: "Newest first. Admin only.",
					Tags:        []string{"Audit"},
					Parameters: []openapi.Parameter{
						openapi.QueryParam("user", "Only this username."),
						openapi.QueryParam("via", "`dashboard`, `api` or `mcp`."),
						openapi.QueryParam("outcome", "`ok`, `refused` or `failed`."),
						openapi.QueryParam("target", "Only events whose target contains this, e.g. an app's reference."),
						openapi.QueryParam("before", "An event id; only older events."),
						openapi.QueryParam("limit", "At most this many, up to 500. Default 100."),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("A page of events.", openapi.Ref("AuditPage")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
		},
	}
}
