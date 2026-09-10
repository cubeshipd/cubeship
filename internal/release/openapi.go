package release

import "cubeship/internal/platform/openapi"

// OpenAPI describes what this instance is running and what changed in
// it.
//
// **Documented rather than internal**, though its one client is the
// dashboard's dialog: what version an instance is on and what that
// version changed is a question anybody integrating against it has, and
// answering it needs no knowledge of how this daemon is built.
func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Releases",
			Description: "What version this instance is running and what changed in it. The notes are in the binary rather than fetched from anywhere: an instance on somebody's own VPS may be behind a firewall, and a changelog that is sometimes empty is worse than none. So an instance can only ever tell you about releases up to the one it is on, which is the question somebody has after an upgrade.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Release": openapi.Object(map[string]*openapi.Schema{
				"version":    openapi.String("The release, without a leading v."),
				"date":       openapi.String("When it went out, as YYYY-MM-DD."),
				"summary":    openapi.String("One sentence, for a list where the body would be too much."),
				"body":       openapi.String("The notes, in Markdown."),
				"prerelease": openapi.Bool("Whether this is not a stable version. Derived from the version itself — semver says one with a hyphen in it is a prerelease."),
			}, "version", "date", "summary", "body"),
			"Releases": openapi.Object(map[string]*openapi.Schema{
				"version": openapi.String("What this instance is running. Absent on a build with nothing stamped on it, which is a developer's — and then there is nothing to show."),
				"notes":   arrayOf(openapi.Ref("Release"), "Every release up to that version, newest first. Never one this instance is not on: a build carrying notes for a version ahead of itself would advertise something nobody can use."),
				"unseen":  arrayOf(openapi.Ref("Release"), "The ones the caller has not been shown, newest first. Empty is the ordinary answer, and it is what a dialog reads to decide not to appear.\n\nWhat counts as seen is per person, not per instance: two admins on one box should each read the notes once, rather than whichever opened the dashboard first taking the notice away from the other."),
			}, "notes", "unseen"),
		},
		Paths: map[string]openapi.PathItem{
			"/releases": {
				"get": &openapi.Operation{
					OperationID: "listReleases",
					Summary:     "What this instance is running, and what changed in it",
					Tags:        []string{"Releases"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The releases this build knows about.", openapi.Ref("Releases")),
						"401": openapi.Unauthorized,
					},
				},
			},
			"/releases/seen": {
				"post": &openapi.Operation{
					OperationID: "markReleasesSeen",
					Summary:     "Mark the notes as read",
					Description: "Records that the caller has read up to the version this instance is running, so `unseen` is empty until it is upgraded again.\n\nIt only ever moves forward: a second tab, or a request that arrives late, would otherwise write an older version over a newer one and show the same notes again.",
					Tags:        []string{"Releases"},
					Responses: openapi.Responses{
						"204": {Description: "Recorded."},
						"401": openapi.Unauthorized,
					},
				},
			},
		},
	}
}

func arrayOf(items *openapi.Schema, description string) *openapi.Schema {
	return &openapi.Schema{Type: "array", Items: items, Description: description}
}
