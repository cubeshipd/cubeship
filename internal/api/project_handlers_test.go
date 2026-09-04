package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateProjectComesWithProductionEnvironment(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"slug": "web", "name": "Web"})
	req := authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/projects", body, key)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Slug         string   `json:"slug"`
		Name         string   `json:"name"`
		Environments []string `json:"environments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Slug != "web" || len(got.Environments) != 1 || got.Environments[0] != "production" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestCreateProjectRequiresOrgAdmin(t *testing.T) {
	srv, _, org := newTestServer(t)
	ctx := context.Background()
	member, _ := srv.store.CreateUser(ctx, "member1", false)
	srv.store.AddMembership(ctx, member.ID, org.ID, "member")
	memberKey := testAPIKeyForExistingUser(t, srv.store, member.ID)

	body, _ := json.Marshal(map[string]string{"slug": "web", "name": "Web"})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/projects", body, memberKey))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateProjectDuplicateSlugWithinOrgConflicts(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"slug": "web", "name": "Web"})

	rec1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec1, authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/projects", body, key))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/projects", body, key))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d", rec2.Code)
	}
}

func TestListProjects(t *testing.T) {
	srv, key, org := newTestServer(t)
	// newTestServer already created testProjectSlug ("default"); add one more.
	body, _ := json.Marshal(map[string]string{"slug": "web", "name": "Web"})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/projects", body, key))

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodGet, "/orgs/"+org.Slug+"/projects", nil, key))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got []map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(got), got)
	}
}

func TestSetProjectEnv(t *testing.T) {
	srv, key, org := newTestServer(t)

	body, _ := json.Marshal(map[string]map[string]string{"vars": {"DATABASE_URL": "postgres://shared"}})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPut, "/orgs/"+org.Slug+"/projects/"+testProjectSlug+"/env", body, key))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	project, err := srv.store.GetProjectBySlug(context.Background(), org.ID, testProjectSlug)
	if err != nil {
		t.Fatalf("GetProjectBySlug: %v", err)
	}
	if project.Env["DATABASE_URL"] != "postgres://shared" {
		t.Fatalf("unexpected project env: %v", project.Env)
	}
}

func TestSetProjectEnvHidesUnknownProject(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]map[string]string{"vars": {"X": "1"}})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPut, "/orgs/"+org.Slug+"/projects/nope/env", body, key))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSetProjectEnvRequiresOrgAdmin(t *testing.T) {
	srv, _, org := newTestServer(t)
	ctx := context.Background()
	member, _ := srv.store.CreateUser(ctx, "member1", false)
	srv.store.AddMembership(ctx, member.ID, org.ID, "member")
	memberKey := testAPIKeyForExistingUser(t, srv.store, member.ID)

	body, _ := json.Marshal(map[string]map[string]string{"vars": {"X": "1"}})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPut, "/orgs/"+org.Slug+"/projects/"+testProjectSlug+"/env", body, memberKey))
	// A plain member is folded into the same "not found" response as an
	// outsider, exactly like handleGetApp — the project endpoints don't
	// leak the project's existence to someone who can't read it either.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
