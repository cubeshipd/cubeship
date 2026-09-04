package app_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"cubeship/internal/org"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/project"
	"cubeship/internal/server/servertest"
)

// stubDocker is enough Docker for a delete: it records what it was asked
// to stop and remove, which is the whole assertion.
type stubDocker struct {
	stopped []string
	removed []string
	running bool
}

func (d *stubDocker) PullImage(context.Context, string) error { return nil }
func (d *stubDocker) CreateContainer(context.Context, dockerx.ContainerOpts) (string, error) {
	return "container-1", nil
}
func (d *stubDocker) StartContainer(context.Context, string) error { return nil }
func (d *stubDocker) StopContainer(_ context.Context, id string) error {
	d.stopped = append(d.stopped, id)
	return nil
}
func (d *stubDocker) RemoveContainer(_ context.Context, id string) error {
	d.removed = append(d.removed, id)
	return nil
}
func (d *stubDocker) IsRunning(context.Context, string) (bool, error) { return d.running, nil }
func (d *stubDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestDeletingAnAppStopsItsContainerFirst(t *testing.T) {
	docker := &stubDocker{running: true}
	f := servertest.NewWithDocker(t, docker)
	_, key := f.AddMember(t, "member", org.RoleMember)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": "acme", "project": "web",
	}, key, &created), http.StatusCreated)

	// Give it a container by deploying once. The deploy is accepted
	// immediately and runs detached, so wait for it before deleting.
	var deployment struct {
		ID int64 `json:"id"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost,
		"/apps/"+created.Reference+"/deploy", map[string]string{"tag": "v1"}, key, &deployment),
		http.StatusAccepted)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, fmt.Sprintf(
		"/apps/%s/deployments/%d?wait=true", created.Reference, deployment.ID), nil, key), http.StatusOK)

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/apps/"+created.Reference, nil, key), http.StatusOK)

	if !slices.Contains(docker.stopped, "container-1") || !slices.Contains(docker.removed, "container-1") {
		t.Errorf("the container was not retired: stopped %v, removed %v", docker.stopped, docker.removed)
	}
	// And the app is gone.
	if rec := f.Do(t, http.MethodGet, "/apps/"+created.Reference, nil, key); rec.Code != http.StatusNotFound {
		t.Errorf("the app is still reachable: %d", rec.Code)
	}
}

// Deleting an app that never deployed has no container to stop, and must
// not fail because of it.
func TestDeletingAnAppWithNoContainer(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleMember)
	createApp(t, f, key, "myapp")

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/apps/acme/web/production/myapp", nil, key), http.StatusOK)
}

// Once deleted, the name is free again — which it never was before,
// since it was taken instance-wide and forever.
func TestDeletingAnAppFreesItsName(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleMember)

	createApp(t, f, key, "myapp")
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/apps/acme/web/production/myapp", nil, key), http.StatusOK)
	createApp(t, f, key, "myapp")
}

// The hierarchy is deleted from the inside out, and each level refuses
// while the one below it is occupied.
func TestDeletesRefuseWhileSomethingLivesInside(t *testing.T) {
	f := servertest.New(t)
	createApp(t, f, f.AdminKey, "myapp")

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme/projects/web", nil, f.AdminKey), http.StatusConflict)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme", nil, f.AdminKey), http.StatusConflict)

	// Empty the app out and the project goes; empty the org and it goes.
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/apps/acme/web/production/myapp", nil, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme/projects/web", nil, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme", nil, f.AdminKey), http.StatusOK)

	// The organization is really gone, not just hidden.
	if rec := f.Do(t, http.MethodGet, "/orgs/acme/projects", nil, f.AdminKey); rec.Code != http.StatusNotFound {
		t.Errorf("the organization survived: %d", rec.Code)
	}
}

func TestDeletingAnOrganizationIsSuperAdminOnly(t *testing.T) {
	f := servertest.New(t)
	_, adminKey := f.AddMember(t, "org-admin", org.RoleAdmin)

	// An org admin may delete a project...
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme/projects/web", nil, adminKey), http.StatusOK)
	// ...but not the organization itself.
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/orgs/acme", nil, adminKey), http.StatusForbidden)
}

func TestDeletingAProjectRequiresAdmin(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme/projects/web", nil, memberKey), http.StatusForbidden)
}

// Deleting a project takes its environments with it, so no environment
// is left pointing at a project that is gone.
func TestDeletingAProjectRemovesItsEnvironments(t *testing.T) {
	f := servertest.New(t)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/projects/web/environments",
		map[string]string{"slug": "staging", "name": "Staging"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme/projects/web", nil, f.AdminKey), http.StatusOK)

	// Recreating the project gives a fresh production environment and
	// nothing else — the staging row did not survive.
	var recreated struct {
		Environments []string `json:"environments"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/orgs/acme/projects",
		map[string]string{"slug": "web", "name": "Web"}, f.AdminKey, &recreated), http.StatusCreated)

	var envs []struct{ Slug string }
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/orgs/acme/projects/web/environments", nil, f.AdminKey, &envs), http.StatusOK)
	if len(envs) != 1 || envs[0].Slug != project.ProductionEnvSlug {
		t.Fatalf("expected only a fresh production environment, got %v", envs)
	}
}
