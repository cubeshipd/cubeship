package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

type deployHook struct {
	URL   *string `json:"url"`
	Token string  `json:"token"`
}

type hookDeploy struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	App    string `json:"app"`
	Tag    string `json:"tag"`
	Ref    string `json:"ref"`
	Error  string `json:"error"`
}

func readHook(t *testing.T, f *servertest.Fixture, key, ref string) deployHook {
	t.Helper()
	var hook deployHook
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+ref+"/deploy-hook", nil, key, &hook), http.StatusOK)
	return hook
}

// postHook calls the webhook the way a CI does: no key, at the root.
func postHook(t *testing.T, f *servertest.Fixture, token, query, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/hooks/deploy/"+token+query, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return f.Serve(t, req)
}

func createExternal(t *testing.T, f *servertest.Fixture, key, name string) string {
	t.Helper()
	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": name, "project": "web", "source": "external", "image": "ghcr.io/acme/" + name,
	}, key, &created), http.StatusCreated)
	servertest.AddDomain(t, f, f.AdminKey, created.Reference, name+".example.com")
	return created.Reference
}

// Every app is born with a webhook, each its own, at the instance's
// own address.
func TestEveryAppHasItsOwnDeployHook(t *testing.T) {
	f := servertest.New(t)
	createApp(t, f, f.AdminKey, "one")
	createApp(t, f, f.AdminKey, "two")

	one := readHook(t, f, f.AdminKey, "web/production/one")
	two := readHook(t, f, f.AdminKey, "web/production/two")
	if len(one.Token) != 64 || strings.Trim(one.Token, "0123456789abcdef") != "" {
		t.Fatalf("token %q is not 64 hex characters", one.Token)
	}
	if one.Token == two.Token {
		t.Fatal("two apps share a deploy token")
	}
	want := "https://" + servertest.Domain + "/hooks/deploy/" + one.Token
	if one.URL == nil || *one.URL != want {
		t.Errorf("url = %v, want %s", one.URL, want)
	}
	// And it is nowhere in the app itself, which every listing reads.
	rec := f.Do(t, http.MethodGet, "/apps", nil, f.AdminKey)
	if strings.Contains(rec.Body.String(), one.Token) {
		t.Error("the app listing carries the deploy token")
	}
}

// A token that names nothing is an address nothing is served at, and
// a GET never deploys — a link preview would otherwise be a deploy.
func TestAnUnknownDeployTokenIsNotFound(t *testing.T) {
	f := servertest.New(t)
	createApp(t, f, f.AdminKey, "api")
	hook := readHook(t, f, f.AdminKey, "web/production/api")

	servertest.RequireStatus(t, postHook(t, f, strings.Repeat("0", 64), "", ""), http.StatusNotFound)
	servertest.RequireStatus(t, postHook(t, f, "nope", "?tag=v1", ""), http.StatusNotFound)
	// A GET is not the hook's at all: it falls through to the
	// dashboard, like any other page address.
	if rec := f.DoRoot(t, http.MethodGet, "/hooks/deploy/"+hook.Token); rec.Code < 300 {
		t.Errorf("GET answered %d", rec.Code)
	}
	var deployments []any
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, appRef("api")+"/deployments", nil, f.AdminKey, &deployments), http.StatusOK)
	if len(deployments) != 0 {
		t.Errorf("a GET deployed: %v", deployments)
	}
}

// The case the webhook exists for: a CI pushed ghcr.io/acme/api:1.2.3
// and says so. The tag is saved, so the next deploy from anywhere
// stays on it, and the call is in the audit log without its token.
func TestTheHookSavesTheTagOfAnExternalApp(t *testing.T) {
	f := servertest.NewWithDocker(t, &stubDocker{running: true})
	ref := createExternal(t, f, f.AdminKey, "api")
	hook := readHook(t, f, f.AdminKey, ref)

	// The query wins over the body.
	rec := postHook(t, f, hook.Token, "?tag=1.2.3&wait=true", `{"tag":"0.0.1"}`)
	servertest.RequireStatus(t, rec, http.StatusOK)
	var got hookDeploy
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" || got.Tag != "1.2.3" || got.App != ref || got.ID == 0 {
		t.Fatalf("answered %+v", got)
	}
	if strings.Contains(rec.Body.String(), hook.Token) {
		t.Error("the answer carries the token")
	}

	var app struct {
		Tag string `json:"tag"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+ref, nil, f.AdminKey, &app), http.StatusOK)
	if app.Tag != "1.2.3" {
		t.Errorf("the app's tag is %q, want the one the hook sent", app.Tag)
	}

	// The body alone works too.
	servertest.RequireStatus(t, postHook(t, f, hook.Token, "", `{"tag":"1.2.4"}`), http.StatusAccepted)

	var page struct {
		Events []struct {
			Username string `json:"username"`
			Via      string `json:"via"`
			Target   string `json:"target"`
			Detail   string `json:"detail"`
			Summary  string `json:"summary"`
			Outcome  string `json:"outcome"`
		} `json:"events"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/audit?via=webhook", nil, f.AdminKey, &page), http.StatusOK)
	events := page.Events
	if len(events) != 2 {
		t.Fatalf("%d webhook events, want 2: %+v", len(events), events)
	}
	e := events[1]
	if e.Username != "deploy webhook" || e.Via != "webhook" || e.Outcome != "ok" ||
		e.Target != "/apps/"+ref+"/deploy" || e.Summary != "Deployed "+ref {
		t.Errorf("recorded %+v", e)
	}
	for _, e := range events {
		if strings.Contains(e.Target+e.Detail, hook.Token) {
			t.Errorf("the audit log carries the token: %+v", e)
		}
	}
}

// An app on this instance's registry that follows pushes deploys the
// tag without saving it: saving would pin the app, and pinning is how
// autodeploy is turned off.
func TestTheHookDoesNotPinARegistryAppThatFollowsPushes(t *testing.T) {
	f := servertest.NewWithDocker(t, &stubDocker{running: true})
	createApp(t, f, f.AdminKey, "api")
	servertest.AddDomain(t, f, f.AdminKey, "web/production/api", "api.example.com")
	hook := readHook(t, f, f.AdminKey, "web/production/api")

	rec := postHook(t, f, hook.Token, "?tag=v7", "")
	servertest.RequireStatus(t, rec, http.StatusAccepted)
	var got hookDeploy
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Tag != "v7" {
		t.Errorf("deployed %q, want v7", got.Tag)
	}
	var app struct {
		Tag        string `json:"tag"`
		Autodeploy bool   `json:"autodeploy"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/web/production/api", nil, f.AdminKey, &app), http.StatusOK)
	if app.Tag != "" || !app.Autodeploy {
		t.Errorf("the hook pinned the app: %+v", app)
	}
}

// A tag belongs to an app that runs an image and a ref to one that
// builds; each refusal says which.
func TestTheHookRefusesWhatTheAppCannotRun(t *testing.T) {
	f := servertest.New(t)
	image := createExternal(t, f, f.AdminKey, "api")
	var built struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", dockerfileApp("built"), f.AdminKey, &built), http.StatusCreated)

	imageHook := readHook(t, f, f.AdminKey, image)
	builtHook := readHook(t, f, f.AdminKey, built.Reference)
	for _, tc := range []struct {
		name, token, query, says string
	}{
		{"malformed tag", imageHook.Token, "?tag=bad%20tag!", "a tag is"},
		{"ref on an image", imageHook.Token, "?ref=main", "names a ref"},
		{"tag on a build", builtHook.Token, "?tag=1.2.3", "names a tag"},
		{"malformed ref", builtHook.Token, "?ref=--upload-pack=x", "a ref is"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postHook(t, f, tc.token, tc.query, "")
			servertest.RequireStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), tc.says) {
				t.Errorf("said %q", rec.Body.String())
			}
		})
	}
	var app struct {
		Tag string `json:"tag"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+image, nil, f.AdminKey, &app), http.StatusOK)
	if app.Tag != "" {
		t.Errorf("a refused call saved tag %q", app.Tag)
	}
}

// Regenerating is what a leaked URL is for: the old one is dead the
// moment the new one is handed back.
func TestRotatingTheHookRetiresTheOldToken(t *testing.T) {
	f := servertest.NewWithDocker(t, &stubDocker{running: true})
	ref := createExternal(t, f, f.AdminKey, "api")
	old := readHook(t, f, f.AdminKey, ref)

	var rotated deployHook
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps/"+ref+"/deploy-hook/rotate", nil, f.AdminKey, &rotated), http.StatusOK)
	if rotated.Token == old.Token || len(rotated.Token) != 64 {
		t.Fatalf("rotated to %q from %q", rotated.Token, old.Token)
	}
	if again := readHook(t, f, f.AdminKey, ref); again.Token != rotated.Token {
		t.Errorf("read %q after rotating to %q", again.Token, rotated.Token)
	}
	servertest.RequireStatus(t, postHook(t, f, old.Token, "", ""), http.StatusNotFound)
	servertest.RequireStatus(t, postHook(t, f, rotated.Token, "", ""), http.StatusAccepted)
}

// Seeing the URL is being able to deploy with it, so it takes what
// deploying takes: a member manages an app that runs an image, and
// only an admin one that builds.
func TestAMemberCannotSeeTheHookOfABuildingApp(t *testing.T) {
	f := servertest.New(t)
	_, member := f.AddMember(t, "member", user.RoleMember)
	image := createExternal(t, f, member, "api")
	var built struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", dockerfileApp("built"), f.AdminKey, &built), http.StatusCreated)

	readHook(t, f, member, image)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/apps/"+built.Reference+"/deploy-hook", nil, member), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+built.Reference+"/deploy-hook/rotate", nil, member), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/apps/"+image+"/deploy-hook", nil, ""), http.StatusUnauthorized)
}
