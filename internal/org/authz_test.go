package org_test

import (
	"net/http"
	"testing"

	"cubeship/internal/org"
	"cubeship/internal/server/servertest"
)

// The authorization matrix, exercised through the real router. These are
// the tests that stop a tenant boundary from quietly disappearing in a
// refactor.
func TestOrgRoutesEnforceRoles(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)
	_, orgAdminKey := f.AddMember(t, "org-admin", org.RoleAdmin)
	_, outsiderKey := servertest.CreateUser(t, f.DB, "outsider", false)

	tests := []struct {
		name   string
		method string
		path   string
		body   any
		key    string
		want   int
	}{
		{"super-admin creates an org", http.MethodPost, "/orgs",
			map[string]string{"slug": "globex", "name": "Globex"}, f.AdminKey, http.StatusCreated},
		{"org admin cannot create an org", http.MethodPost, "/orgs",
			map[string]string{"slug": "initech", "name": "Initech"}, orgAdminKey, http.StatusForbidden},
		{"member cannot create an org", http.MethodPost, "/orgs",
			map[string]string{"slug": "initech", "name": "Initech"}, memberKey, http.StatusForbidden},

		{"org admin adds a user", http.MethodPost, "/orgs/acme/users",
			map[string]string{"username": "newbie", "role": "member"}, orgAdminKey, http.StatusCreated},
		{"member cannot add a user", http.MethodPost, "/orgs/acme/users",
			map[string]string{"username": "nope", "role": "member"}, memberKey, http.StatusForbidden},
		{"outsider cannot add a user", http.MethodPost, "/orgs/acme/users",
			map[string]string{"username": "nope", "role": "member"}, outsiderKey, http.StatusNotFound},

		{"org admin creates a project", http.MethodPost, "/orgs/acme/projects",
			map[string]string{"slug": "api", "name": "API"}, orgAdminKey, http.StatusCreated},
		{"member cannot create a project", http.MethodPost, "/orgs/acme/projects",
			map[string]string{"slug": "nope", "name": "Nope"}, memberKey, http.StatusForbidden},

		{"member creates an app", http.MethodPost, "/apps",
			map[string]string{"name": "web-app", "domain": "web.example.com", "org": "acme", "project": "web"},
			memberKey, http.StatusCreated},
		{"outsider cannot create an app", http.MethodPost, "/apps",
			map[string]string{"name": "sneaky", "domain": "s.example.com", "org": "acme", "project": "web"},
			outsiderKey, http.StatusNotFound},

		{"unauthenticated is rejected", http.MethodGet, "/orgs", nil, "", http.StatusUnauthorized},
		{"an unknown key is rejected", http.MethodGet, "/orgs", nil, "not-a-real-key", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := f.Do(t, tt.method, tt.path, tt.body, tt.key)
			servertest.RequireStatus(t, rec, tt.want)
		})
	}
}

// An outsider must not be able to tell whether a resource exists. Every
// refusal on a specific resource is a 404, never a 403.
func TestOutsiderCannotLearnThatAnAppExists(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)
	_, outsiderKey := servertest.CreateUser(t, f.DB, "outsider", false)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": "secret-app", "domain": "secret.example.com", "org": "acme", "project": "web",
	}, memberKey), http.StatusCreated)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"get", http.MethodGet, "/apps/acme/web/production/secret-app", nil},
		{"logs", http.MethodGet, "/apps/acme/web/production/secret-app/logs", nil},
		{"set env", http.MethodPut, "/apps/acme/web/production/secret-app/env", map[string]any{"vars": map[string]string{"A": "1"}}},
		{"deploy", http.MethodPost, "/apps/acme/web/production/secret-app/deploy", map[string]string{"tag": "latest"}},
		{"delete", http.MethodDelete, "/apps/acme/web/production/secret-app", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.Do(t, tc.method, tc.path, tc.body, outsiderKey)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 so the app's existence stays hidden, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Listing must be scoped in the database, not filtered afterwards: a
// caller must never see another organization's apps.
func TestListAppsIsScopedToTheCallersOrganizations(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": "acme-app", "domain": "acme.example.com", "org": "acme", "project": "web",
	}, memberKey), http.StatusCreated)

	// A second organization, with its own app, that "member" has no part in.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs",
		map[string]string{"slug": "globex", "name": "Globex"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/globex/projects",
		map[string]string{"slug": "web", "name": "Web"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": "globex-app", "domain": "globex.example.com", "org": "globex", "project": "web",
	}, f.AdminKey), http.StatusCreated)

	var mine []struct {
		Name string `json:"name"`
		Org  string `json:"org"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps", nil, memberKey, &mine), http.StatusOK)

	if len(mine) != 1 || mine[0].Name != "acme-app" {
		t.Fatalf("a member of acme should see exactly their own app, got %v", mine)
	}
	if mine[0].Org != "acme" {
		t.Errorf("app response should name its organization, got %q", mine[0].Org)
	}

	// The super-admin sees both.
	var all []struct{ Name string }
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps", nil, f.AdminKey, &all), http.StatusOK)
	if len(all) != 2 {
		t.Fatalf("a super-admin should see every app, got %v", all)
	}
}

// A user belongs to as many organizations as they are added to, keeping
// one key throughout.
func TestExistingUserAddedToSecondOrgKeepsTheirKey(t *testing.T) {
	f := servertest.New(t)

	var first struct {
		APIKey string `json:"api_key"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/orgs/acme/users",
		map[string]string{"username": "employee", "role": "member"}, f.AdminKey, &first), http.StatusCreated)
	if first.APIKey == "" {
		t.Fatal("expected a new user to be issued a key")
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs",
		map[string]string{"slug": "globex", "name": "Globex"}, f.AdminKey), http.StatusCreated)

	var second map[string]string
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/orgs/globex/users",
		map[string]string{"username": "employee", "role": "admin"}, f.AdminKey, &second), http.StatusCreated)
	if _, ok := second["api_key"]; ok {
		t.Fatalf("an existing user must keep their key, not be issued another: %v", second)
	}

	// Their original key now reaches both organizations.
	var orgs []struct{ Slug string }
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/orgs", nil, first.APIKey, &orgs), http.StatusOK)
	if len(orgs) != 2 {
		t.Fatalf("expected the user to see both organizations, got %v", orgs)
	}
}

func TestAddingAnExistingMemberConflicts(t *testing.T) {
	f := servertest.New(t)
	body := map[string]string{"username": "employee", "role": "member"}

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/users", body, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/users", body, f.AdminKey), http.StatusConflict)
}
