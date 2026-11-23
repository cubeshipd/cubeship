package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/platform/buildkit"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/server/servertest"
)

func appTag(t *testing.T, f *servertest.Fixture, ref string) string {
	t.Helper()
	var app struct {
		Tag string `json:"tag"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+ref, nil, f.AdminKey, &app), http.StatusOK)
	return app.Tag
}

// A key the hook does not know is refused, not ignored: ?tags= would
// otherwise deploy the tag the app is already on and say it had done
// what it was asked.
func TestTheHookRefusesAParameterItDoesNotTake(t *testing.T) {
	f := servertest.NewWithDocker(t, &stubDocker{running: true})
	ref := createExternal(t, f, f.AdminKey, "api")
	hook := readHook(t, f, f.AdminKey, ref)

	for _, tc := range []struct{ query, says string }{
		{"?tags=1.2.3", `"tags"`},
		{"?tag=1.2.3&force=1", `"force"`},
		{"?wait=yes", "wait is true or false"},
		{"?wait=", "wait is true or false"},
	} {
		rec := postHook(t, f, hook.Token, tc.query, "")
		servertest.RequireStatus(t, rec, http.StatusBadRequest)
		if !strings.Contains(rec.Body.String(), tc.says) {
			t.Errorf("%s said %q", tc.query, rec.Body.String())
		}
	}
	if tag := appTag(t, f, ref); tag != "" {
		t.Errorf("a refused call saved tag %q", tag)
	}
	var deployments []any
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+ref+"/deployments", nil, f.AdminKey, &deployments), http.StatusOK)
	if len(deployments) != 0 {
		t.Errorf("a refused call deployed: %v", deployments)
	}

	// wait takes what strconv.ParseBool does.
	servertest.RequireStatus(t, postHook(t, f, hook.Token, "?tag=1.2.3&wait=1", ""), http.StatusOK)
	servertest.RequireStatus(t, postHook(t, f, hook.Token, "?wait=false", ""), http.StatusAccepted)
}

// Whatever a sender posts — GitHub's push event, Docker Hub's, a form —
// is not read for what it says. The query string is the whole interface.
func TestTheHookIgnoresWhatTheSenderPosts(t *testing.T) {
	f := servertest.NewWithDocker(t, &stubDocker{running: true})
	ref := createExternal(t, f, f.AdminKey, "api")
	hook := readHook(t, f, f.AdminKey, ref)

	for _, tc := range []struct{ query, contentType, body string }{
		{"?tag=1.0.0", "application/json", `{"push_data":{"tag":"9.9.9"},"repository":{"name":"api"}}`},
		{"", "application/x-www-form-urlencoded", "tag=9.9.9&ref=main"},
		{"", "text/plain", "not json at all {"},
	} {
		servertest.RequireStatus(t, postHookAs(t, f, hook.Token, tc.query, tc.contentType, tc.body), http.StatusAccepted)
	}
	if tag := appTag(t, f, ref); tag != "1.0.0" {
		t.Errorf("the app's tag is %q, want the query's 1.0.0", tag)
	}
}

// blockingDocker holds every pull until release is closed, so a deploy
// is still running for as long as a test needs it to be.
type blockingDocker struct {
	*stubDocker
	release chan struct{}
}

func (d *blockingDocker) PullImage(ctx context.Context, _ string, _ *dockerx.RegistryAuth) error {
	select {
	case <-d.release:
	case <-ctx.Done():
	}
	return nil
}

// The route has no login, so a wait has a ceiling of its own. Reaching
// it answers 202 with where the deploy had got to; the deploy goes on.
func TestAWaitThatRunsOutAnswersWhereTheDeployHadGot(t *testing.T) {
	docker := &blockingDocker{stubDocker: &stubDocker{running: true}, release: make(chan struct{})}
	f := servertest.NewWithDocker(t, docker)
	// Registered after the fixture, so it runs before the fixture waits
	// for the deploy it unblocks.
	t.Cleanup(func() { close(docker.release) })
	f.Server.Apps.HookWait = 50 * time.Millisecond
	ref := createExternal(t, f, f.AdminKey, "api")
	hook := readHook(t, f, f.AdminKey, ref)

	rec := postHook(t, f, hook.Token, "?tag=1.2.3&wait=true", "")
	servertest.RequireStatus(t, rec, http.StatusAccepted)
	var got hookDeploy
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" || got.ID == 0 || got.Tag != "1.2.3" {
		t.Errorf("answered %+v", got)
	}
}

// recordingBuilder succeeds at every build and remembers what it was
// asked to clone.
type recordingBuilder struct {
	mu      sync.Mutex
	targets []string
}

func (b *recordingBuilder) Build(_ context.Context, req buildkit.Request, logs io.Writer) error {
	b.mu.Lock()
	b.targets = append(b.targets, req.ContextGit)
	b.mu.Unlock()
	_, _ = io.WriteString(logs, "built\n")
	return nil
}

func (b *recordingBuilder) BuildPlanned(context.Context, buildkit.PlannedRequest, io.Writer) error {
	return nil
}

// A building app deploys the ref the hook names, for that deploy only.
func TestTheHookBuildsTheRefItIsGiven(t *testing.T) {
	builder := &recordingBuilder{}
	f := servertest.NewWithBuilder(t, &stubDocker{running: true}, builder)
	var built struct {
		Reference string `json:"reference"`
		Ref       string `json:"ref"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", dockerfileApp("built"), f.AdminKey, &built), http.StatusCreated)
	servertest.AddDomain(t, f, f.AdminKey, built.Reference, "built.example.com")
	hook := readHook(t, f, f.AdminKey, built.Reference)

	rec := postHook(t, f, hook.Token, "?ref=release-1&wait=true", "")
	servertest.RequireStatus(t, rec, http.StatusOK)
	var got hookDeploy
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" || got.Ref != "release-1" {
		t.Fatalf("answered %+v", got)
	}
	builder.mu.Lock()
	targets := builder.targets
	builder.mu.Unlock()
	if len(targets) != 1 || !strings.HasSuffix(targets[0], "#release-1") {
		t.Errorf("built %v, want the repository at release-1", targets)
	}

	var app struct {
		Ref string `json:"ref"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+built.Reference, nil, f.AdminKey, &app), http.StatusOK)
	if app.Ref != built.Ref {
		t.Errorf("the hook saved ref %q on the app, which had %q", app.Ref, built.Ref)
	}
}
