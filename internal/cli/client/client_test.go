// The client declares its own copies of the API's wire types, which
// could drift from the server's. These tests are what stops that: they
// run the real client against a real daemon, so a renamed field, a moved
// route or a changed status code fails here rather than in someone's
// terminal.
package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/cli/client"
	"cubeship/internal/org"
	"cubeship/internal/server/servertest"
)

// connect returns a client pointed at a real server, authenticated as a
// member of its "acme" organization.
func connect(t *testing.T) (*client.Client, *servertest.Fixture) {
	t.Helper()
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleAdmin)
	return client.New(f.HTTPServer(t).URL, key), f
}

// One pass through the hierarchy the CLI actually drives, asserting the
// client reads back what the server wrote.
func TestClientRoundTripsTheWholeHierarchy(t *testing.T) {
	c, _ := connect(t)
	ctx := context.Background()

	projects, err := c.ListProjects(ctx, "acme")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].Slug != "web" {
		t.Fatalf("expected the fixture's web project, got %v", projects)
	}

	envs, err := c.ListEnvironments(ctx, "acme", "web")
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 1 || envs[0].Slug != "production" {
		t.Fatalf("expected only production, got %v", envs)
	}

	created, err := c.CreateApp(ctx, "myapp", "myapp.example.com", "acme", "web", "", "")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	// Every field the CLI prints has to survive the round trip.
	if created.Name != "myapp" || created.Org != "acme" ||
		created.Project != "web" || created.Environment != "production" {
		t.Errorf("app came back as %+v", created)
	}
	if created.Reference != "acme/web/production/myapp" {
		t.Errorf("reference is %q, want acme/web/production/myapp", created.Reference)
	}
	if created.Image != servertest.RegistryHost+"/"+created.Reference {
		t.Errorf("push path is %q, want %s/%s", created.Image, servertest.RegistryHost, created.Reference)
	}
	if created.Status == "" || created.Domain == "" {
		t.Errorf("status or domain is empty: %+v", created)
	}

	got, err := c.GetApp(ctx, created.Reference)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got != created {
		t.Errorf("GetApp returned %+v, CreateApp returned %+v", got, created)
	}

	apps, err := c.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0] != created {
		t.Errorf("ListApps returned %v", apps)
	}

	if err := c.SetAppEnv(ctx, created.Reference, map[string]string{"KEY": "value"}); err != nil {
		t.Fatalf("SetAppEnv: %v", err)
	}
	if err := c.SetProjectEnv(ctx, "acme", "web", map[string]string{"SHARED": "1"}); err != nil {
		t.Fatalf("SetProjectEnv: %v", err)
	}

	env, err := c.CreateEnvironment(ctx, "acme", "web", "staging", "Staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if env.Slug != "staging" {
		t.Errorf("environment came back as %+v", env)
	}
	if err := c.SetEnvironmentEnv(ctx, "acme", "web", "staging", map[string]string{"LOG": "debug"}); err != nil {
		t.Fatalf("SetEnvironmentEnv: %v", err)
	}
	if err := c.DeleteEnvironment(ctx, "acme", "web", "staging"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
}

// A refusal has to reach the user in the daemon's own words. This used
// to print "unexpected status 409", which tells them nothing about what
// to do next.
func TestErrorsCarryTheDaemonsMessage(t *testing.T) {
	c, _ := connect(t)
	ctx := context.Background()

	if _, err := c.CreateApp(ctx, "myapp", "myapp.example.com", "acme", "web", "", ""); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	_, err := c.CreateApp(ctx, "myapp", "other.example.com", "acme", "web", "", "")
	if err == nil {
		t.Fatal("expected the duplicate name to be refused")
	}
	if !strings.Contains(err.Error(), "app already exists") {
		t.Errorf("error is %q; it should carry the daemon's own message", err)
	}
	if got := client.Status(err); got != http.StatusConflict {
		t.Errorf("Status(err) is %d, want %d", got, http.StatusConflict)
	}

	var apiErr *client.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want a *client.Error a caller can inspect", err)
	}
	if apiErr.Op == "" {
		t.Error("the error does not say which call failed")
	}
}

func TestUnknownAppIsNotFound(t *testing.T) {
	c, _ := connect(t)

	_, err := c.GetApp(context.Background(), "acme/web/production/no-such-app")
	if got := client.Status(err); got != http.StatusNotFound {
		t.Fatalf("Status(err) is %d, want 404 (err: %v)", got, err)
	}
}

// A name the daemon would never route must not be able to reach some
// other endpoint by escaping its path segment.
func TestPathSegmentsAreEscaped(t *testing.T) {
	c, _ := connect(t)

	// Unescaped, ".." would climb out of the app's path entirely.
	_, err := c.GetApp(context.Background(), "acme/web/production/../../../orgs")
	if err == nil {
		t.Fatal("a traversal in the app reference was accepted")
	}
	if got := client.Status(err); got != http.StatusNotFound && got != http.StatusBadRequest {
		t.Errorf("Status(err) is %d; the request should not have reached another route", got)
	}
}

// Key management is absent from the OpenAPI document but very much part
// of the client — it is what `cubeship user api-key` drives.
func TestAPIKeyLifecycle(t *testing.T) {
	c, f := connect(t)
	ctx := context.Background()

	id, key, err := c.CreateAPIKey(ctx, "mcp")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if key == "" || id == 0 {
		t.Fatalf("CreateAPIKey returned id %d, key %q", id, key)
	}

	keys, err := c.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected two keys, got %v", keys)
	}
	var current, mcp bool
	for _, k := range keys {
		if k.CurrentKey {
			current = true
		}
		if k.Name == "mcp" && k.CreatedAt.IsZero() {
			t.Error("the created_at timestamp did not decode")
		}
		if k.Name == "mcp" {
			mcp = true
		}
	}
	if !current || !mcp {
		t.Errorf("expected the current key and the new one, got %v", keys)
	}

	// The new key works on its own.
	other := client.New(f.HTTPServer(t).URL, key)
	if _, err := other.WhoAmI(ctx); err != nil {
		t.Fatalf("the new key does not authenticate: %v", err)
	}

	if err := c.RevokeAPIKey(ctx, id); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	if _, err := other.WhoAmI(ctx); err == nil {
		t.Fatal("the revoked key still authenticates")
	}
}

func TestWhoAmI(t *testing.T) {
	c, _ := connect(t)

	username, err := c.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	if username != "member" {
		t.Errorf("WhoAmI returned %q, want member", username)
	}
}

// An app with no container yet has no logs, and the client must surface
// that as the daemon's 409 rather than an empty stream.
func TestLogsOnAnAppThatWasNeverDeployed(t *testing.T) {
	c, _ := connect(t)
	ctx := context.Background()

	if _, err := c.CreateApp(ctx, "myapp", "myapp.example.com", "acme", "web", "", ""); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	rc, err := c.Logs(ctx, "acme/web/production/myapp", "100")
	if err == nil {
		io.Copy(io.Discard, rc)
		rc.Close()
		t.Fatal("expected logs to be refused for an app with no container")
	}
	if got := client.Status(err); got != http.StatusConflict {
		t.Errorf("Status(err) is %d, want 409 (err: %v)", got, err)
	}
}

func TestUnauthenticatedClientIsRejected(t *testing.T) {
	f := servertest.New(t)
	c := client.New(f.HTTPServer(t).URL, "not-a-real-key")

	_, err := c.ListOrgs(context.Background())
	if got := client.Status(err); got != http.StatusUnauthorized {
		t.Fatalf("Status(err) is %d, want 401 (err: %v)", got, err)
	}
}

// `env set` must not delete what it doesn't mention — the reason the
// merge endpoints exist. This drives the same calls the CLI does.
func TestClientMergeEnvKeepsOtherVariables(t *testing.T) {
	c, _ := connect(t)
	ctx := context.Background()

	if _, err := c.CreateApp(ctx, "myapp", "myapp.example.com", "acme", "web", "", ""); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	const ref = "acme/web/production/myapp"
	if err := c.MergeAppEnv(ctx, ref, map[string]string{"A": "1", "B": "2"}, nil); err != nil {
		t.Fatalf("MergeAppEnv: %v", err)
	}
	if err := c.MergeAppEnv(ctx, ref, map[string]string{"C": "3"}, []string{"B"}); err != nil {
		t.Fatalf("MergeAppEnv: %v", err)
	}

	got, err := c.AppEnv(ctx, ref)
	if err != nil {
		t.Fatalf("AppEnv: %v", err)
	}
	if got.Vars["A"] != "1" || got.Vars["C"] != "3" {
		t.Errorf("A and C should both survive, got %v", got.Vars)
	}
	if _, present := got.Vars["B"]; present {
		t.Errorf("B should have been unset, got %v", got.Vars)
	}

	// The effective view has to decode, source and all.
	if len(got.Effective) != 2 {
		t.Fatalf("expected two effective variables, got %v", got.Effective)
	}
	for _, v := range got.Effective {
		if v.Source != "app" {
			t.Errorf("%s came from %q, want app", v.Key, v.Source)
		}
	}
}

// Reading at a project shows only that level; an environment shows what
// an app there would inherit.
func TestClientReadsEnvAtEveryLevel(t *testing.T) {
	c, _ := connect(t)
	ctx := context.Background()

	if err := c.MergeProjectEnv(ctx, "acme", "web", map[string]string{"SHARED": "p", "P": "1"}, nil); err != nil {
		t.Fatalf("MergeProjectEnv: %v", err)
	}
	if err := c.MergeEnvironmentEnv(ctx, "acme", "web", "production", map[string]string{"SHARED": "e"}, nil); err != nil {
		t.Fatalf("MergeEnvironmentEnv: %v", err)
	}

	project, err := c.ProjectEnv(ctx, "acme", "web")
	if err != nil {
		t.Fatalf("ProjectEnv: %v", err)
	}
	if project.Vars["SHARED"] != "p" || project.Vars["P"] != "1" {
		t.Errorf("project vars are %v", project.Vars)
	}

	environment, err := c.EnvironmentEnv(ctx, "acme", "web", "production")
	if err != nil {
		t.Fatalf("EnvironmentEnv: %v", err)
	}
	if environment.Vars["SHARED"] != "e" || len(environment.Vars) != 1 {
		t.Errorf("the environment's own vars are %v, want only SHARED", environment.Vars)
	}
	effective := map[string]string{}
	for _, v := range environment.Effective {
		effective[v.Key] = v.Source
	}
	if effective["SHARED"] != "environment" || effective["P"] != "project" {
		t.Errorf("effective sources are %v; SHARED should win at the environment", effective)
	}
}

// A deploy is accepted, not performed, by the request that asks for it.
// The client has to reflect that: Deploy returns as soon as the daemon
// has recorded the attempt, and waiting is a separate call that can be
// abandoned without abandoning the deploy.
func TestClientDeployReturnsADeploymentToFollow(t *testing.T) {
	c, _ := connect(t)
	ctx := context.Background()

	created, err := c.CreateApp(ctx, "myapp", "myapp.example.com", "acme", "web", "", "")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	deployment, err := c.Deploy(ctx, created.Reference, "v1")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if deployment.ID == 0 {
		t.Fatal("Deploy returned no deployment id to follow")
	}

	// The fixture has no Docker, so the deploy fails — which is the
	// point: the failure is reported through the deployment, not by the
	// request that started it.
	finished, err := c.WaitForDeployment(ctx, created.Reference, deployment.ID)
	if err != nil {
		t.Fatalf("WaitForDeployment: %v", err)
	}
	if !finished.Done() {
		t.Fatalf("the deploy is %q after waiting for it", finished.Status)
	}
	if finished.Status == client.DeploymentFailed && finished.Error == "" {
		t.Error("a failed deploy came back with no reason")
	}

	history, err := c.Deployments(ctx, created.Reference)
	if err != nil {
		t.Fatalf("Deployments: %v", err)
	}
	if len(history) != 1 || history[0].ID != deployment.ID {
		t.Fatalf("the history should hold exactly the deploy just made, got %v", history)
	}
}
