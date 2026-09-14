package templateinstall

import (
	"context"
	"errors"
	"slices"
	"testing"
)

const gitlab = `version: 1
minCubeship: "0.7.2"
project: gitlab
inputs:
  - key: sshPort
    type: number
    label: The port SSH answers on
    default: 2222
    min: 1024
    max: 65535
apps:
  - key: web
    name: gitlab
    image: gitlab/gitlab-ce
    port: 80
    tcp:
      - port: 22
        host: ${input.sshPort}
      - port: 9418
`

// An install publishes the ports its template declares: on the host port
// the input answered, or on one the instance picks.
func TestAnInstallPublishesAnAppsTCPPorts(t *testing.T) {
	f := newFixture("0.7.2")
	*f.catalog.releases = []CatalogRelease{{Tag: "v1.0.0", Commit: "fab1234", Status: "accepted"}}
	f.catalog.sources["fab1234"] = gitlab

	in, run, _ := f.install(t, Request{Owner: "cubeshipd", Repo: "cubeship-gitlab-template",
		Inputs: map[string]string{"sshPort": "2200"}})
	if in.Status != StatusInstalled || run.Status != RunSucceeded {
		t.Fatalf("installation = %+v, run = %+v", in, run)
	}
	for _, want := range []string{
		"publish port 22 of gitlab/production/gitlab on 2200",
		"publish port 9418 of gitlab/production/gitlab on 0",
	} {
		if !slices.Contains(f.w.events(), want) {
			t.Errorf("no %q: %v", want, f.w.events())
		}
	}
}

// A host port something on the instance already publishes is refused before
// anything is created.
func TestAnInstallRefusesATakenHostPortBeforeCreatingAnything(t *testing.T) {
	f := newFixture("0.7.2")
	*f.catalog.releases = []CatalogRelease{{Tag: "v1.0.0", Commit: "fab1234", Status: "accepted"}}
	f.catalog.sources["fab1234"] = gitlab
	f.w.hostPorts = map[int]bool{2200: true}

	_, _, err := f.s.Install(context.Background(), admin, Request{Owner: "cubeshipd", Repo: "cubeship-gitlab-template",
		Inputs: map[string]string{"sshPort": "2200"}})
	var taken *TakenError
	if !errors.As(err, &taken) || taken.Name != "2200" {
		t.Fatalf("install: %v", err)
	}
	if events := f.w.events(); len(events) != 0 {
		t.Errorf("something was created: %v", events)
	}
}
