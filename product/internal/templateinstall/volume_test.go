package templateinstall

import (
	"slices"
	"testing"
)

const rabbit = `version: 1
minCubeship: "0.7.0"
project: rabbit
apps:
  - key: broker
    image: rabbitmq
    tag: "4"
    port: 5672
    volumes:
      - path: /var/lib/rabbitmq
`

// An install gives an app the volumes its template declares, and undoing
// the install takes their data with the app — it is only what the install
// wrote.
func TestAnInstallGivesAnAppItsVolumes(t *testing.T) {
	f := newFixture("0.7.0")
	*f.catalog.releases = []CatalogRelease{{Tag: "v1.0.0", Commit: "fab1234", Status: "accepted"}}
	f.catalog.sources["fab1234"] = rabbit

	in, run, _ := f.install(t, Request{Owner: "cubeshipd", Repo: "cubeship-rabbitmq-template"})
	if in.Status != StatusInstalled || run.Status != RunSucceeded {
		t.Fatalf("installation = %+v, run = %+v", in, run)
	}
	if !slices.Contains(f.w.events(), "add volume /var/lib/rabbitmq to rabbit/production/broker") {
		t.Errorf("no volume was added: %v", f.w.events())
	}
	if v := f.w.app("rabbit/production/broker").Volumes; len(v) != 1 || v[0].Path != "/var/lib/rabbitmq" {
		t.Errorf("the app's volumes are %+v", v)
	}
}

func TestUndoingAnInstallDeletesTheVolumeDataItWrote(t *testing.T) {
	f := newFixture("0.7.0")
	*f.catalog.releases = []CatalogRelease{{Tag: "v1.0.0", Commit: "fab1234", Status: "accepted"}}
	f.catalog.sources["fab1234"] = rabbit
	f.w.deployFails = true

	in, _, _ := f.install(t, Request{Owner: "cubeshipd", Repo: "cubeship-rabbitmq-template"})
	if in.Status != StatusFailed {
		t.Fatalf("installation = %+v", in)
	}
	if !slices.Contains(f.w.events(), "delete app rabbit/production/broker and its volume data") {
		t.Errorf("the undo kept data only the install wrote: %v", f.w.events())
	}
}
