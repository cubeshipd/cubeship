package templateinstall

import (
	"context"
	"errors"
	"slices"
	"testing"

	"cubeship/internal/template"
	"cubeship/internal/user"
)

// republish makes source the template the catalog lists, at a new commit.
func (c fakeCatalog) republish(commit, source string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sources[commit] = source
	*c.releases = append([]CatalogRelease{{Tag: commit, Commit: commit, Status: "accepted"}}, *c.releases...)
}

func (f *fixture) update(t *testing.T, id int64, inputs map[string]string) Run {
	t.Helper()
	run, _, err := f.s.Update(context.Background(), admin, id, UpdateRequest{Inputs: inputs})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	f.s.Wait()
	return f.records.run(run.ID)
}

func TestOnlyAnAdminComparesAndUpdates(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, _ := f.install(t, umamiRequest())
	f.catalog.publish()
	member := &user.User{ID: 2, Role: user.RoleMember}
	if _, err := f.s.PreviewUpdate(context.Background(), member, in.ID); !errors.Is(err, user.ErrForbidden) {
		t.Errorf("preview: %v", err)
	}
	if _, _, err := f.s.Update(context.Background(), member, in.ID, UpdateRequest{}); !errors.Is(err, user.ErrForbidden) {
		t.Errorf("update: %v", err)
	}
}

// The catalog's commit moves with every template it publishes: the same
// file at a new commit is no update.
func TestTheSameTemplateAtANewCommitIsUpToDate(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, _ := f.install(t, umamiRequest())
	f.catalog.republish("fff0000", umami)

	preview, err := f.s.PreviewUpdate(context.Background(), admin, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Available || len(preview.Changes) != 0 || preview.To != "fff0000" {
		t.Errorf("preview = %+v", preview)
	}
	if _, _, err := f.s.Update(context.Background(), admin, in.ID, UpdateRequest{}); !errors.Is(err, ErrUpToDate) {
		t.Errorf("update: %v", err)
	}
}

func TestAPinnedSourceComparesByItsDirectory(t *testing.T) {
	at := func(ref string) *template.Normalized {
		return &template.Normalized{Apps: []template.NormalizedApp{{Key: "web", Source: template.NormalizedSource{
			Type: "dockerfile", Repo: "https://github.com/cubeshipd/cubeship", Ref: &ref,
		}}}}
	}
	if !sameJSON(unpinned(at("abc1234:templates/x")), unpinned(at("def5678:templates/x"))) {
		t.Error("two commits of one directory differ")
	}
	if sameJSON(unpinned(at("abc1234:templates/x")), unpinned(at("abc1234:templates/y"))) {
		t.Error("two directories compare equal")
	}
	if m := at("abc1234:templates/x"); *unpinned(m).Apps[0].Source.Ref == *m.Apps[0].Source.Ref || *m.Apps[0].Source.Ref != "abc1234:templates/x" {
		t.Error("unpinned changed the manifest it was given")
	}
}

func TestAPreviewSaysWhatAnUpdateWouldDoAndChangesNothing(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, _ := f.install(t, umamiRequest())
	f.catalog.publish()

	before := len(f.w.events())
	preview, err := f.s.PreviewUpdate(context.Background(), admin, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.w.events()) != before {
		t.Errorf("a preview did %v", f.w.events()[before:])
	}
	if !preview.Available || preview.From != "v1.1.0" || preview.To != "v1.3.0" {
		t.Errorf("preview = %+v", preview)
	}
	for _, want := range []Change{
		{ActionChange, KindApp, "umami/production/cubeship-umami-template-web", "ghcr.io/umami-software/umami:3.3.1 → ghcr.io/umami-software/umami:3.4.0"},
		{ActionCreate, KindDatabase, "cubeship-umami-template-umami-cache", "redis 7.4"},
		{ActionChange, "variable", "SESSION_KEY", "set on umami/production/cubeship-umami-template-web"},
		{ActionKeep, "variable", "SELF", "no longer in the template; left on umami/production/cubeship-umami-template-web"},
	} {
		if !slices.Contains(preview.Changes, want) {
			t.Errorf("no %+v in %+v", want, preview.Changes)
		}
	}
	if len(preview.Inputs) != 1 || preview.Inputs[0].Key != "timezone" {
		t.Errorf("questions = %+v", preview.Inputs)
	}
}

func TestAnUpdateAppliesTheTemplateAndRecordsIt(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, secrets := f.install(t, umamiRequest())
	f.catalog.publish()

	if _, _, err := f.s.Update(context.Background(), admin, in.ID, UpdateRequest{}); err == nil {
		t.Fatal("an update with a question unanswered started")
	}
	done := f.update(t, in.ID, map[string]string{"timezone": "Europe/Lisbon"})
	updated, _ := f.records.Install(context.Background(), in.ID)
	if done.Status != RunSucceeded || done.Kind != RunUpdate || updated.Release != "v1.3.0" ||
		updated.Commit != "def5678" || updated.Answers["timezone"] != "Europe/Lisbon" {
		t.Fatalf("run = %+v, installation = %+v", done, updated)
	}
	web := f.w.app("umami/production/cubeship-umami-template-web")
	if web.SourceTag != "3.4.0" || web.Env["SESSION_KEY"] != secrets["appSecret"] || web.Env["TZ"] != "Europe/Lisbon" ||
		web.Env["SELF"] == "" {
		t.Errorf("web = %s %v", web.SourceTag, web.Env)
	}
	if !slices.Contains(updated.Resources, Resource{KindDatabase, "cache", "cubeship-umami-template-umami-cache"}) {
		t.Errorf("resources = %v", updated.Resources)
	}
	if _, _, err := f.s.Update(context.Background(), admin, in.ID, UpdateRequest{}); !errors.Is(err, ErrUpToDate) {
		t.Errorf("a second update: %v", err)
	}
}

func TestAFailedUpdatePutsTheInstallationBack(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, secrets := f.install(t, umamiRequest())
	f.catalog.publish()
	f.w.mu.Lock()
	f.w.deployFails = true
	f.w.mu.Unlock()

	done := f.update(t, in.ID, map[string]string{"timezone": "UTC"})
	after, _ := f.records.Install(context.Background(), in.ID)
	if done.Status != RunFailed || after.Release != "v1.1.0" || after.Status != StatusInstalled {
		t.Fatalf("run = %+v, installation = %+v", done, after)
	}
	web := f.w.app("umami/production/cubeship-umami-template-web")
	if web.SourceTag != "3.3.1" || web.Env["APP_SECRET"] != secrets["appSecret"] {
		t.Errorf("web was not put back: %s %v", web.SourceTag, web.Env)
	}
	if _, set := web.Env["SESSION_KEY"]; set {
		t.Errorf("a variable the update added is still set: %v", web.Env)
	}
	if !slices.Contains(f.w.events(), "delete database cubeship-umami-template-umami-cache") {
		t.Errorf("the database the update created was left: %v", f.w.events())
	}
}

// umamiSplit moves the domain off web onto a new app in front of it, the
// way Jian 1.0.0 moved its panel out of the gateway.
const umamiSplit = `version: 1
minCubeship: "0.6.0"
project: umami
inputs:
  - key: domain
    type: domain
    label: Where it answers
  - key: appSecret
    type: secret
    label: Session secret
    generate: 32
databases:
  - key: db
    name: umami-db
    engine: postgres
    version: "18"
    database: umami
apps:
  - key: web
    name: web
    image: ghcr.io/umami-software/umami
    tag: "3.3.1"
    port: 3000
    health: /api/heartbeat
    attach:
      - database: db
    limits: { cpu: 1, memory: 1Gi }
    env:
      APP_SECRET: ${input.appSecret}
      DB_HOST: ${db.db.host}
      SELF: http://${app.web.internal}:${app.web.port}
  - key: front
    name: front
    image: ghcr.io/example/front
    tag: "1.0.0"
    port: 8080
    domains:
      - host: ${input.domain}
    env:
      UPSTREAM: http://${app.web.internal}:${app.web.port}
`

func TestAnUpdateMovesADomainToANewApp(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, _ := f.install(t, umamiRequest())
	f.catalog.republish("aaa1111", umamiSplit)

	preview, err := f.s.PreviewUpdate(context.Background(), admin, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := Change{ActionRemove, KindDomain, "analytics.example.com", "from umami/production/cubeship-umami-template-web; no longer in the template"}
	if !preview.Available || !slices.Contains(preview.Changes, want) || len(preview.Inputs) != 0 {
		t.Fatalf("preview = %+v", preview)
	}

	done := f.update(t, in.ID, nil)
	if done.Status != RunSucceeded {
		t.Fatalf("run = %+v", done)
	}
	web, front := f.w.app("umami/production/cubeship-umami-template-web"), f.w.app("umami/production/cubeship-umami-template-front")
	if len(web.Domains) != 0 || len(front.Domains) != 1 || front.Domains[0].Host != "analytics.example.com" {
		t.Errorf("web = %v, front = %v", web.Domains, front.Domains)
	}
	if front.Env["UPSTREAM"] != "http://cubeship-umami-production-cubeship-umami-template-web:3000" {
		t.Errorf("front env = %v", front.Env)
	}
}

func TestAFailedUpdateGivesAMovedDomainBack(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, _ := f.install(t, umamiRequest())
	f.catalog.republish("aaa1111", umamiSplit)
	f.w.mu.Lock()
	f.w.deployFails = true
	f.w.mu.Unlock()

	done := f.update(t, in.ID, nil)
	if done.Status != RunFailed {
		t.Fatalf("run = %+v", done)
	}
	web := f.w.app("umami/production/cubeship-umami-template-web")
	if len(web.Domains) != 1 || web.Domains[0].Host != "analytics.example.com" {
		t.Errorf("web domains = %v, events = %v", web.Domains, f.w.events())
	}
	if !slices.Contains(f.w.events(), "delete app umami/production/cubeship-umami-template-front") {
		t.Errorf("the app the update created was left: %v", f.w.events())
	}
}
