package update

import "cubeship/internal/platform/openapi"

// OpenAPI describes moving this instance to a newer release.
func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Updates",
			Description: "What release this instance is on, what it could move to, and moving it.\n\nAn update replaces every container this instance runs, on every machine in the cluster, and it replaces the daemon serving this API — so the request that starts one returns as soon as the work has begun, and how it went is read back from `GET /updates`.\n\n**Nothing on the instance can be changed while one is running.** Every write answers 503 until it finishes, including from a client that has just reconnected: the moment worth protecting is exactly the one where the daemon has restarted underneath somebody.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Release2": openapi.Object(map[string]*openapi.Schema{
				"version":      openapi.String("The release, without a leading v."),
				"notes":        openapi.String("What changed, in Markdown, as written on the release. Carried here because it is the one thing about a version newer than this build that the build cannot already know."),
				"published_at": {Type: "string", Format: "date-time", Description: "When it went out."},
				"prerelease":   openapi.Bool("Whether it is a candidate. Never offered by a check — asking for one by name is the way to run one."),
			}, "version"),
			"UpdateRun": openapi.Object(map[string]*openapi.Schema{
				"version":     openapi.String("The release it is moving to."),
				"from":        openapi.String("What it was on."),
				"status":      {Type: "string", Enum: []string{"running", "done", "failed"}, Description: "Where it got to. While it is `running` this instance refuses every write."},
				"step":        openapi.String("What is happening now, in words: \"pulling the new images\", \"waiting for 2 machine(s) to come back\"."),
				"done":        {Type: "array", Items: openapi.String("A step that finished."), Description: "Every step that finished, oldest first — a count for a progress bar and a list for a person."},
				"error":       openapi.String("Why it failed, in the words of whatever failed."),
				"started_at":  {Type: "string", Format: "date-time"},
				"finished_at": {Type: "string", Format: "date-time", Description: "Absent while it is running."},
			}, "version", "status", "started_at"),
			"Updates": openapi.Object(map[string]*openapi.Schema{
				"version":   openapi.String("What this instance is running. Absent on a build with nothing stamped on it, which is a developer's — and one that cannot be updated."),
				"available": openapi.Ref("Release2"),
				"checked":   openapi.Bool("Whether the lookup happened. **False with no `available` means the instance could not ask**, which is a different thing to show than \"you are up to date\": an instance behind a firewall is a normal instance."),
				"run":       openapi.Ref("UpdateRun"),
			}, "checked"),
		},
		Paths: map[string]openapi.PathItem{
			"/updates": {
				"get": &openapi.Operation{
					OperationID: "getUpdates",
					Summary:     "What this instance is on, and what it could move to",
					Description: "Looks up the newest stable release each time it is asked. An instance that cannot reach the internet answers `checked: false` rather than claiming to be current.",
					Tags:        []string{"Updates"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("This instance's version and what is available.", openapi.Ref("Updates")),
						"401": openapi.Unauthorized,
					},
				},
				"post": &openapi.Operation{
					OperationID: "startUpdate",
					Summary:     "Move this instance to a release",
					Description: "Replaces every machine in the cluster and then this one.\n\n**The order is the design.** The other machines go first, because once this one restarts it can tell nobody anything; a machine that does not come back is carried on without, since one box being off must not freeze the whole cluster. This machine goes last, and hands its own replacement to a container that outlives it.\n\nIt answers as soon as the work has started. Nothing can hold a connection open across the daemon being replaced, so progress is read back from `GET /updates` — including by a client that reconnects afterwards, because the record is a file rather than anything held in memory.\n\nThe version is required rather than defaulted to \"the newest\": a button that says what it will install and a request that decides for itself are two different promises.",
					Tags:        []string{"Updates"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"version": openapi.String("The release to move to, e.g. `0.2.0`. A prerelease is reachable by naming it."),
					}, "version")),
					Responses: openapi.Responses{
						"202": openapi.JSONResponse("The update has started.", openapi.Ref("Updates")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No release by that name."),
						"409": openapi.TextResponse("An update is already running, the instance is already on that release or a newer one, or this daemon is not running as a container and so has nothing to replace."),
					},
				},
			},
		},
	}
}
