package audit

import "testing"

func TestAnEventReadsAsASentence(t *testing.T) {
	cases := []struct {
		event Event
		want  string
	}{
		{Event{Action: "POST /apps/{project}/{env}/{name}/deploy", Target: "/apps/web/production/api/deploy", Outcome: OutcomeOK},
			"Deployed web/production/api"},
		{Event{Action: "GET /apps/{project}/{env}/{name}/env", Target: "/apps/web/production/api/env", Outcome: OutcomeRefused},
			"Tried to read the variables of web/production/api"},
		{Event{Action: "DELETE /datastores/{name}", Target: "/datastores/cache", Outcome: OutcomeFailed},
			"Could not delete database cache"},
		{Event{Action: "mcp deploy_app", Target: "app=web/production/api tag=v2", Outcome: OutcomeOK},
			"Deployed web/production/api"},
		{Event{Action: "mcp attach_datastore", Target: "app=web/api datastore=main", Outcome: OutcomeOK},
			"Attached database main to web/api"},
		// An argument missing is the noun, never half a name.
		{Event{Action: "mcp deploy_app", Outcome: OutcomeRefused}, "Tried to deploy an app"},
		{Event{Action: "POST /projects", Target: "/projects", Outcome: OutcomeOK}, "Created a project"},
		{Event{Action: "GET /instance/containers", Target: "/instance/containers", Outcome: OutcomeRefused},
			"Tried to read /instance/containers"},
	}
	for _, c := range cases {
		if got := Summary(c.event); got != c.want {
			t.Errorf("%s %q: got %q, want %q", c.event.Action, c.event.Target, got, c.want)
		}
	}
}
