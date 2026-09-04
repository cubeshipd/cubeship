package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
	"cubeship/internal/storetest"
)

// newFaultInjectableTestServer is newTestServer plus a raw connection
// into the same database, so a test can break the schema underneath the
// server (see breakAPIKeyInserts). It returns the server, a super-admin
// key, the "acme" organization and that connection.
func newFaultInjectableTestServer(t *testing.T) (*Server, string, *store.Organization, *sql.DB) {
	t.Helper()
	s, db := storetest.NewWithRawDB(t)

	org, err := s.CreateOrganization(context.Background(), "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	key := testAPIKeyFor(t, s, true)
	return NewServer(s, nil, "webhook-secret", "registry.example.com"), key, org, db
}

// breakAPIKeyInserts makes every future INSERT into api_keys fail, to
// stand in for a database error striking partway through a multi-step
// write. Reads keep working, so the caller can still authenticate.
func breakAPIKeyInserts(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE FUNCTION no_new_keys() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'api_keys is unavailable'; END;
		$$ LANGUAGE plpgsql;

		CREATE TRIGGER no_new_keys BEFORE INSERT ON api_keys
		FOR EACH ROW EXECUTE FUNCTION no_new_keys();`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

func createOrgUser(t *testing.T, srv *Server, orgSlug, username, role, key string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "role": role})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPost, "/orgs/"+orgSlug+"/users", body, key))
	return rec
}

// Global constraint: a user belongs to as many organizations as they are
// added to. Posting an existing username used to hit the users.username
// unique index and return 500.
func TestCreateOrgUserAddsExistingUserToSecondOrg(t *testing.T) {
	srv, adminKey, acme := newTestServer(t)
	ctx := context.Background()
	globex, err := srv.store.CreateOrganization(ctx, "globex", "Globex Corp")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	first := createOrgUser(t, srv, acme.Slug, "employee1", "member", adminKey)
	if first.Code != http.StatusCreated {
		t.Fatalf("first org: expected 201, got %d: %s", first.Code, first.Body.String())
	}
	var created map[string]string
	json.Unmarshal(first.Body.Bytes(), &created)
	userKey := created["api_key"]
	if userKey == "" {
		t.Fatal("expected the new user's API key")
	}

	second := createOrgUser(t, srv, globex.Slug, "employee1", "admin", adminKey)
	if second.Code != http.StatusCreated {
		t.Fatalf("second org: expected 201, got %d: %s", second.Code, second.Body.String())
	}
	var added map[string]string
	json.Unmarshal(second.Body.Bytes(), &added)
	if _, ok := added["api_key"]; ok {
		t.Fatalf("an existing user must keep their key, not be issued another: %v", added)
	}

	// One user, two memberships, with the role each org was given.
	user, err := srv.store.GetUserByUsername(ctx, "employee1")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if role, err := srv.store.GetMembership(ctx, user.ID, acme.ID); err != nil || role != store.RoleMember {
		t.Fatalf("expected member in acme, got %q (%v)", role, err)
	}
	if role, err := srv.store.GetMembership(ctx, user.ID, globex.ID); err != nil || role != store.RoleAdmin {
		t.Fatalf("expected admin in globex, got %q (%v)", role, err)
	}

	// Their original key now reaches both organizations.
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodGet, "/orgs", nil, userKey))
	var orgs []map[string]string
	json.Unmarshal(rec.Body.Bytes(), &orgs)
	if len(orgs) != 2 {
		t.Fatalf("expected the user to see both orgs, got %v", orgs)
	}
}

func TestCreateOrgUserAlreadyMemberConflicts(t *testing.T) {
	srv, adminKey, org := newTestServer(t)

	if rec := createOrgUser(t, srv, org.Slug, "employee1", "member", adminKey); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	rec := createOrgUser(t, srv, org.Slug, "employee1", "member", adminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a user already in the org, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A failure after the user row is inserted used to leave an orphaned
// user: no membership, no key, and a username permanently taken.
func TestCreateOrgUserRollsBackOnFailure(t *testing.T) {
	srv, adminKey, org, db := newFaultInjectableTestServer(t)
	breakAPIKeyInserts(t, db)

	rec := createOrgUser(t, srv, org.Slug, "employee1", "member", adminKey)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the key can't be issued, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := srv.store.GetUserByUsername(context.Background(), "employee1"); err == nil {
		t.Fatal("expected no half-created user to be left behind")
	}
}

// Rotation revokes before it issues; a failure in between used to lock
// the user out for good.
func TestRotateAPIKeyKeepsOldKeyWorkingWhenReissueFails(t *testing.T) {
	srv, adminKey, _, db := newFaultInjectableTestServer(t)
	breakAPIKeyInserts(t, db)

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPost, "/users/me/api-key/rotate", nil, adminKey))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the new key can't be issued, got %d: %s", rec.Code, rec.Body.String())
	}

	stillWorks := httptest.NewRecorder()
	srv.Router().ServeHTTP(stillWorks, authedRequest(http.MethodGet, "/orgs", nil, adminKey))
	if stillWorks.Code != http.StatusOK {
		t.Fatalf("a failed rotation must not revoke the caller's only key, got %d", stillWorks.Code)
	}
}
