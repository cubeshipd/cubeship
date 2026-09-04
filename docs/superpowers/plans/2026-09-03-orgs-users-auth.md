# Organizations, Users, and API-Key Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the daemon's single shared bearer token with organizations,
users, per-user API keys, and per-org roles (admin/member) plus an
instance-level super-admin — so multiple companies' apps can live on one
VPS with real per-user access control on the daemon's own API.

**Architecture:** Four new SQLite tables (organizations, users,
memberships, api_keys) alongside the existing app/deployment tables. The
daemon's auth middleware resolves an incoming API key to a user instead
of comparing against a fixed string; every app-acting handler then checks
that user's role in the app's owning organization before proceeding.
Bootstrap creates one super-admin user on first boot, seeded from the
same env-var/persisted-token mechanism sub-project 1 already has.

**Tech Stack:** Go (matches the existing daemon/CLI), `database/sql` +
`modernc.org/sqlite` (already a dependency), `crypto/sha256` +
`crypto/rand` (stdlib, no new dependency for this plan).

**Spec:** [docs/superpowers/specs/2026-09-03-orgs-users-auth-design.md](../specs/2026-09-03-orgs-users-auth-design.md)

## Global Constraints

- Users can belong to multiple organizations, with a role (`admin` or
  `member`) per organization — a `memberships` join table, not a single
  `org_id` on `users`.
- One super-admin level above organizations, implicitly authorized
  everywhere; the first user created (at daemon bootstrap) is the
  super-admin.
- No self-signup, no email/invite flow — only an admin (of an org) or the
  super-admin creates users, via CLI/API.
- One active API key per user; rotation is revoke-then-reissue, not
  multiple named keys.
- No web UI in this plan — CLI/API only.
- No billing/usage metering.

## Ruling carried from plan-writing (deviates from one spec sentence)

The spec's Data Model section says app-name uniqueness becomes
`UNIQUE(org_id, name)` (org-scoped) instead of globally unique, and that
the registry path becomes the app's sole external identifier. Implementing
that would force every existing app-scoped route
(`/apps/{name}/deploy`, `/apps/{name}/logs`, `/apps/{name}/env`, the
registry webhook's app lookup) to become org-scoped
(`/orgs/{org}/apps/{name}/...`) — a much larger, riskier rewrite of
sub-project 1's API surface for marginal benefit in this plan alone (org
isolation on the registry itself doesn't land until the follow-up JWT
plan anyway).

**This plan keeps app names globally unique**, exactly as sub-project 1
already has it (`store.GetAppByName(ctx, name)` unchanged). Apps gain an
`OrgID` field used purely for **ownership and authorization** — every
app-acting handler now checks the caller's role in that `OrgID` before
proceeding, which delivers the spec's actual goal (per-org access
control) without restructuring routing. The registry image path still
gets an `<org-slug>/` prefix (`registry.<domain>/<org-slug>/<app-name>`),
so the follow-up JWT-scoping plan has a real per-org namespace to enforce
against, even though nothing enforces it yet in this plan.

---

## File Structure

```
cubeship/
  internal/
    store/
      organizations.go      # Organization CRUD
      organizations_test.go
      users.go               # User CRUD, CountUsers (bootstrap check)
      users_test.go
      memberships.go         # Membership CRUD, role lookup
      memberships_test.go
      apikeys.go             # APIKey CRUD, lookup-by-hash
      apikeys_test.go
      apps.go                 # MODIFY: App.OrgID, CreateApp(ctx, orgID, ...)
      apps_test.go             # MODIFY
      store.go                 # MODIFY: schema gains 4 tables + apps.org_id
    authkey/
      authkey.go             # Generate() / Hash() — pure, no store dep
      authkey_test.go
    api/
      server.go               # MODIFY: authMiddleware resolves API key -> user
      server_test.go           # MODIFY
      authz.go                # NEW: role-check helper, shared by handlers
      authz_test.go
      apps_handlers.go        # MODIFY: org required on create, authz on all
      apps_handlers_test.go    # MODIFY
      deploy_handlers.go      # MODIFY: authz before deploy/env/logs
      deploy_handlers_test.go  # MODIFY
      logs_handler_test.go     # MODIFY (shares deploy_handlers.go)
      webhook_handler_test.go  # MODIFY (shared fakes/helpers)
      org_handlers.go         # NEW: POST/GET /orgs
      org_handlers_test.go
      user_handlers.go        # NEW: POST /orgs/{slug}/users, POST /users/me/api-key/rotate
      user_handlers_test.go
    bootstrap/
      bootstrap.go             # MODIFY: registry image path gains org-slug
    apiclient/
      client.go                # MODIFY: CreateApp takes org slug; new CreateOrg/CreateUser/RotateAPIKey
      client_test.go            # MODIFY
  cmd/
    cubeshipd/
      main.go                  # MODIFY: create super-admin on first boot
    cubeship/
      app.go                    # MODIFY: app create gains --org
      org.go                   # NEW: org create/list
      user.go                  # NEW: user create, user api-key rotate
  test/
    integration/
      deploy_test.go            # MODIFY: org-scoped app create + login flow
```

## Task Right-Sizing Note

Tasks 1-5 build the new data model and crypto helper in isolation (pure
SQLite + stdlib, no dependency on the API layer). Task 6 extends the
existing `apps` table. Tasks 7-11 rewrite and extend the daemon API.
Task 12 wires bootstrap. Tasks 13-15 build the CLI. Task 16 sweeps the
remaining test breakage this ripples into and updates the Docker
integration test.

---

### Task 1: Store — organizations

**Files:**
- Modify: `internal/store/store.go` (schema: add `organizations` table)
- Create: `internal/store/organizations.go`
- Test: `internal/store/organizations_test.go`

**Interfaces:**
- Produces:
  - `type store.Organization struct { ID int64; Slug string; Name string; CreatedAt time.Time }`
  - `(*Store).CreateOrganization(ctx, slug, name string) (*Organization, error)`
  - `(*Store).GetOrganizationBySlug(ctx, slug string) (*Organization, error)`
  - `(*Store).ListOrganizations(ctx) ([]*Organization, error)`

- [ ] **Step 1: Add the schema**

In `internal/store/store.go`, add to the `schema` constant (alongside the
existing `apps`/`deployments` tables):
```sql
CREATE TABLE IF NOT EXISTS organizations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 2: Write the failing test**

`internal/store/organizations_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestCreateAndGetOrganization(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	created, err := s.CreateOrganization(ctx, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := s.GetOrganizationBySlug(ctx, "acme")
	if err != nil {
		t.Fatalf("GetOrganizationBySlug: %v", err)
	}
	if got.Name != "Acme Inc" {
		t.Fatalf("expected name Acme Inc, got %q", got.Name)
	}
}

func TestGetOrganizationBySlugNotFound(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	if _, err := s.GetOrganizationBySlug(context.Background(), "nope"); err == nil {
		t.Fatal("expected an error for an unknown slug")
	}
}

func TestListOrganizations(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateOrganization(ctx, "globex", "Globex Corp")

	orgs, err := s.ListOrganizations(ctx)
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if len(orgs) != 2 {
		t.Fatalf("expected 2 organizations, got %d", len(orgs))
	}
}
```

- [ ] **Step 3: Run to confirm it fails**

Run: `go test ./internal/store/... -run TestCreateAndGetOrganization`
Expected: FAIL (`CreateOrganization` undefined)

- [ ] **Step 4: Implement**

`internal/store/organizations.go`:
```go
package store

import (
	"context"
	"fmt"
	"time"
)

type Organization struct {
	ID        int64
	Slug      string
	Name      string
	CreatedAt time.Time
}

func (s *Store) CreateOrganization(ctx context.Context, slug, name string) (*Organization, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO organizations (slug, name) VALUES (?, ?)`, slug, name); err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}
	return s.GetOrganizationBySlug(ctx, slug)
}

func (s *Store) GetOrganizationBySlug(ctx context.Context, slug string) (*Organization, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at FROM organizations WHERE slug = ?`, slug)
	var o Organization
	if err := row.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt); err != nil {
		return nil, fmt.Errorf("get organization %q: %w", slug, err)
	}
	return &o, nil
}

func (s *Store) ListOrganizations(ctx context.Context) ([]*Organization, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name, created_at FROM organizations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []*Organization
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, &o)
	}
	return orgs, rows.Err()
}
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "Add organizations to the store"
```

---

### Task 2: Store — users

**Files:**
- Modify: `internal/store/store.go` (schema: add `users` table)
- Create: `internal/store/users.go`
- Test: `internal/store/users_test.go`

**Interfaces:**
- Produces:
  - `type store.User struct { ID int64; Username string; IsSuperAdmin bool; CreatedAt time.Time }`
  - `(*Store).CreateUser(ctx, username string, isSuperAdmin bool) (*User, error)`
  - `(*Store).GetUserByUsername(ctx, username string) (*User, error)`
  - `(*Store).GetUserByID(ctx, id int64) (*User, error)`
  - `(*Store).CountUsers(ctx) (int, error)` — used by bootstrap to decide whether to create the super-admin

- [ ] **Step 1: Add the schema**

In `internal/store/store.go`, add to `schema`:
```sql
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	is_super_admin INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 2: Write the failing test**

`internal/store/users_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestCreateAndGetUser(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "lucas", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !created.IsSuperAdmin {
		t.Fatal("expected IsSuperAdmin true")
	}

	byName, err := s.GetUserByUsername(ctx, "lucas")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	byID, err := s.GetUserByID(ctx, byName.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if byID.Username != "lucas" {
		t.Fatalf("expected username lucas, got %q", byID.Username)
	}
}

func TestCountUsers(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 users, got %d", n)
	}

	s.CreateUser(ctx, "lucas", true)
	s.CreateUser(ctx, "employee1", false)

	n, err = s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 users, got %d", n)
	}
}
```

- [ ] **Step 3: Run to confirm it fails**

Run: `go test ./internal/store/... -run 'TestCreateAndGetUser|TestCountUsers'`
Expected: FAIL

- [ ] **Step 4: Implement**

`internal/store/users.go`:
```go
package store

import (
	"context"
	"fmt"
	"time"
)

type User struct {
	ID           int64
	Username     string
	IsSuperAdmin bool
	CreatedAt    time.Time
}

func (s *Store) CreateUser(ctx context.Context, username string, isSuperAdmin bool) (*User, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, is_super_admin) VALUES (?, ?)`, username, isSuperAdmin); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return s.GetUserByUsername(ctx, username)
}

func (s *Store) scanUser(row interface{ Scan(dest ...any) error }) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.IsSuperAdmin, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, is_super_admin, created_at FROM users WHERE username = ?`, username)
	u, err := s.scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user %q: %w", username, err)
	}
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, is_super_admin, created_at FROM users WHERE id = ?`, id)
	u, err := s.scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user %d: %w", id, err)
	}
	return u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "Add users to the store"
```

---

### Task 3: Store — memberships

**Files:**
- Modify: `internal/store/store.go` (schema: add `memberships` table)
- Create: `internal/store/memberships.go`
- Test: `internal/store/memberships_test.go`

**Interfaces:**
- Consumes: `store.Organization`, `store.User` (Tasks 1-2).
- Produces:
  - `type store.Role string` with `store.RoleAdmin = "admin"`, `store.RoleMember = "member"`
  - `(*Store).AddMembership(ctx, userID, orgID int64, role Role) error`
  - `(*Store).GetMembership(ctx, userID, orgID int64) (Role, error)` — returns an error if no membership row exists
  - `(*Store).ListMembershipsForUser(ctx, userID int64) ([]OrgMembership, error)`
  - `type store.OrgMembership struct { OrgID int64; OrgSlug string; OrgName string; Role Role }`

- [ ] **Step 1: Add the schema**

In `internal/store/store.go`, add to `schema`:
```sql
CREATE TABLE IF NOT EXISTS memberships (
	user_id INTEGER NOT NULL REFERENCES users(id),
	org_id INTEGER NOT NULL REFERENCES organizations(id),
	role TEXT NOT NULL,
	PRIMARY KEY (user_id, org_id)
);
```

- [ ] **Step 2: Write the failing test**

`internal/store/memberships_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestAddAndGetMembership(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := s.CreateUser(ctx, "lucas", false)

	if err := s.AddMembership(ctx, user.ID, org.ID, RoleAdmin); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}

	role, err := s.GetMembership(ctx, user.ID, org.ID)
	if err != nil {
		t.Fatalf("GetMembership: %v", err)
	}
	if role != RoleAdmin {
		t.Fatalf("expected admin, got %q", role)
	}
}

func TestGetMembershipNotFound(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := s.CreateUser(ctx, "lucas", false)

	if _, err := s.GetMembership(ctx, user.ID, org.ID); err == nil {
		t.Fatal("expected an error for a user with no membership in the org")
	}
}

func TestListMembershipsForUser(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	acme, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	globex, _ := s.CreateOrganization(ctx, "globex", "Globex Corp")
	user, _ := s.CreateUser(ctx, "lucas", false)
	s.AddMembership(ctx, user.ID, acme.ID, RoleAdmin)
	s.AddMembership(ctx, user.ID, globex.ID, RoleMember)

	memberships, err := s.ListMembershipsForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListMembershipsForUser: %v", err)
	}
	if len(memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(memberships))
	}
	bySlug := map[string]Role{}
	for _, m := range memberships {
		bySlug[m.OrgSlug] = m.Role
	}
	if bySlug["acme"] != RoleAdmin || bySlug["globex"] != RoleMember {
		t.Fatalf("unexpected memberships: %+v", memberships)
	}
}
```

- [ ] **Step 3: Run to confirm it fails**

Run: `go test ./internal/store/... -run 'TestAddAndGetMembership|TestGetMembershipNotFound|TestListMembershipsForUser'`
Expected: FAIL

- [ ] **Step 4: Implement**

`internal/store/memberships.go`:
```go
package store

import (
	"context"
	"fmt"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type OrgMembership struct {
	OrgID   int64
	OrgSlug string
	OrgName string
	Role    Role
}

func (s *Store) AddMembership(ctx context.Context, userID, orgID int64, role Role) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO memberships (user_id, org_id, role) VALUES (?, ?, ?)`,
		userID, orgID, string(role)); err != nil {
		return fmt.Errorf("add membership: %w", err)
	}
	return nil
}

func (s *Store) GetMembership(ctx context.Context, userID, orgID int64) (Role, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE user_id = ? AND org_id = ?`, userID, orgID).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("get membership: %w", err)
	}
	return Role(role), nil
}

func (s *Store) ListMembershipsForUser(ctx context.Context, userID int64) ([]OrgMembership, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.org_id, o.slug, o.name, m.role
		FROM memberships m
		JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = ?
		ORDER BY o.slug`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrgMembership
	for rows.Next() {
		var m OrgMembership
		var role string
		if err := rows.Scan(&m.OrgID, &m.OrgSlug, &m.OrgName, &role); err != nil {
			return nil, err
		}
		m.Role = Role(role)
		out = append(out, m)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "Add memberships to the store"
```

---

### Task 4: Store — API keys

**Files:**
- Modify: `internal/store/store.go` (schema: add `api_keys` table)
- Create: `internal/store/apikeys.go`
- Test: `internal/store/apikeys_test.go`

**Interfaces:**
- Consumes: `store.User` (Task 2).
- Produces:
  - `type store.APIKey struct { ID int64; UserID int64; KeyHash string; CreatedAt time.Time; LastUsedAt *time.Time }`
  - `(*Store).CreateAPIKey(ctx, userID int64, keyHash string) (*APIKey, error)`
  - `(*Store).GetUserByAPIKeyHash(ctx, keyHash string) (*User, error)` — joins through to the owning user; error if no key matches
  - `(*Store).RevokeAPIKeysForUser(ctx, userID int64) error` — deletes all of a user's keys (rotation = revoke all, then create one)
  - `(*Store).TouchAPIKeyLastUsed(ctx, keyHash string) error`

- [ ] **Step 1: Add the schema**

In `internal/store/store.go`, add to `schema`:
```sql
CREATE TABLE IF NOT EXISTS api_keys (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	key_hash TEXT NOT NULL UNIQUE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_used_at DATETIME
);
```

- [ ] **Step 2: Write the failing test**

`internal/store/apikeys_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestCreateAPIKeyAndLookupByHash(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)

	if _, err := s.CreateAPIKey(ctx, user.ID, "hash-of-secret"); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	got, err := s.GetUserByAPIKeyHash(ctx, "hash-of-secret")
	if err != nil {
		t.Fatalf("GetUserByAPIKeyHash: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("expected user %d, got %d", user.ID, got.ID)
	}
}

func TestGetUserByAPIKeyHashUnknown(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	if _, err := s.GetUserByAPIKeyHash(context.Background(), "no-such-hash"); err == nil {
		t.Fatal("expected an error for an unknown key hash")
	}
}

func TestRevokeAPIKeysForUser(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	s.CreateAPIKey(ctx, user.ID, "old-hash")

	if err := s.RevokeAPIKeysForUser(ctx, user.ID); err != nil {
		t.Fatalf("RevokeAPIKeysForUser: %v", err)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, "old-hash"); err == nil {
		t.Fatal("expected the revoked key to no longer resolve")
	}
}

func TestTouchAPIKeyLastUsed(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	s.CreateAPIKey(ctx, user.ID, "a-hash")

	if err := s.TouchAPIKeyLastUsed(ctx, "a-hash"); err != nil {
		t.Fatalf("TouchAPIKeyLastUsed: %v", err)
	}

	var lastUsed *string
	s.db.QueryRow(`SELECT last_used_at FROM api_keys WHERE key_hash = ?`, "a-hash").Scan(&lastUsed)
	if lastUsed == nil {
		t.Fatal("expected last_used_at to be set")
	}
}
```

- [ ] **Step 3: Run to confirm it fails**

Run: `go test ./internal/store/... -run 'TestCreateAPIKeyAndLookupByHash|TestGetUserByAPIKeyHashUnknown|TestRevokeAPIKeysForUser|TestTouchAPIKeyLastUsed'`
Expected: FAIL

- [ ] **Step 4: Implement**

`internal/store/apikeys.go`:
```go
package store

import (
	"context"
	"fmt"
	"time"
)

type APIKey struct {
	ID         int64
	UserID     int64
	KeyHash    string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

func (s *Store) CreateAPIKey(ctx context.Context, userID int64, keyHash string) (*APIKey, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (user_id, key_hash) VALUES (?, ?)`, userID, keyHash)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &APIKey{ID: id, UserID: userID, KeyHash: keyHash}, nil
}

func (s *Store) GetUserByAPIKeyHash(ctx context.Context, keyHash string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_super_admin, u.created_at
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = ?`, keyHash)
	u, err := s.scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user by api key: %w", err)
	}
	return u, nil
}

func (s *Store) RevokeAPIKeysForUser(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("revoke api keys: %w", err)
	}
	return nil
}

func (s *Store) TouchAPIKeyLastUsed(ctx context.Context, keyHash string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE key_hash = ?`, keyHash); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "Add API keys to the store"
```

---

### Task 5: API key generation and hashing

**Files:**
- Create: `internal/authkey/authkey.go`
- Test: `internal/authkey/authkey_test.go`

**Interfaces:**
- Produces:
  - `authkey.Generate() (string, error)` — a new random API key (64 hex chars, same shape as sub-project 1's daemon token)
  - `authkey.Hash(key string) string` — SHA-256 hex digest, used as `store.APIKey.KeyHash`

This package has no dependency on `store` or anything else — pure
crypto, easy to unit test in isolation, used by both the bootstrap
(Task 12) and the API's rotate-key handler (Task 11).

- [ ] **Step 1: Write the failing test**

`internal/authkey/authkey_test.go`:
```go
package authkey

import "testing"

func TestGenerateProducesUniqueKeys(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(a) != 64 {
		t.Fatalf("expected a 64-hex-char key, got %d chars: %q", len(a), a)
	}
	if a == b {
		t.Fatal("expected two calls to Generate to produce different keys")
	}
}

func TestHashIsDeterministicAndDistinct(t *testing.T) {
	h1 := Hash("my-secret-key")
	h2 := Hash("my-secret-key")
	if h1 != h2 {
		t.Fatalf("expected Hash to be deterministic, got %q and %q", h1, h2)
	}
	if Hash("a-different-key") == h1 {
		t.Fatal("expected different inputs to hash differently")
	}
	if h1 == "my-secret-key" {
		t.Fatal("expected Hash to actually transform the input, not return it verbatim")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/authkey/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/authkey/authkey.go`:
```go
package authkey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/authkey/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/authkey
git commit -m "Add API key generation and hashing"
```

---

### Task 6: Apps gain an owning organization

**Files:**
- Modify: `internal/store/store.go` (schema: `apps.org_id`)
- Modify: `internal/store/apps.go` (`App.OrgID`, `CreateApp` signature, `scanApp`)
- Modify: `internal/store/apps_test.go` (update existing `CreateApp` calls, add an org-scoping test)

**Interfaces:**
- Consumes: `store.Organization` (Task 1).
- Produces (changed from sub-project 1):
  - `type store.App struct { ID int64; OrgID int64; Name string; Domain string; Image string; ContainerID string; Status string; Env map[string]string; CreatedAt time.Time }`
    (adds `OrgID`, otherwise unchanged)
  - `(*Store).CreateApp(ctx, orgID int64, name, domain, image string) (*App, error)`
    (gains the leading `orgID` parameter — every other App method's
    signature, including `GetAppByName`, is unchanged; see the plan's
    "Ruling carried from plan-writing" — app names stay globally unique)

- [ ] **Step 1: Add the schema column**

In `internal/store/store.go`, change the `apps` table definition:
```sql
CREATE TABLE IF NOT EXISTS apps (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	org_id INTEGER NOT NULL REFERENCES organizations(id),
	name TEXT NOT NULL UNIQUE,
	domain TEXT NOT NULL,
	image TEXT NOT NULL UNIQUE,
	container_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	env TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 2: Update the existing tests to pass an org**

In `internal/store/apps_test.go`, every existing test creates an org
first and passes its ID. For example, `TestCreateAndGetApp` becomes:
```go
func TestCreateAndGetApp(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	created, err := s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if created.OrgID != org.ID {
		t.Fatalf("expected OrgID %d, got %d", org.ID, created.OrgID)
	}

	got, err := s.GetAppByName(ctx, "myapp")
	if err != nil {
		t.Fatalf("GetAppByName: %v", err)
	}
	if got.Domain != "myapp.example.com" || got.Image != "registry.example.com/acme/myapp" {
		t.Fatalf("unexpected app: %+v", got)
	}
	if got.Status != "pending" {
		t.Fatalf("expected initial status 'pending', got %q", got.Status)
	}
}
```

Apply the same change (create an org, pass `org.ID` as the new first
argument to `CreateApp`) to every other existing test in
`apps_test.go`, `deployments_test.go` (if it calls `CreateApp`), and
`apps_handlers_test.go`/`webhook_handler_test.go`/`deploy_handlers_test.go`/
`logs_handler_test.go` in `internal/api` — **do not touch the `internal/api`
test files in this task**, they're covered by Tasks 7-9; just fix
`internal/store`'s own tests here so this task's `go test
./internal/store/...` is green on its own.

- [ ] **Step 3: Run to confirm it fails**

Run: `go test ./internal/store/...`
Expected: FAIL (`CreateApp` signature mismatch — too many/few arguments)

- [ ] **Step 4: Implement**

In `internal/store/apps.go`, update `App` and `CreateApp`:
```go
type App struct {
	ID          int64
	OrgID       int64
	Name        string
	Domain      string
	Image       string
	ContainerID string
	Status      string
	Env         map[string]string
	CreatedAt   time.Time
}

func (s *Store) CreateApp(ctx context.Context, orgID int64, name, domain, image string) (*App, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO apps (org_id, name, domain, image) VALUES (?, ?, ?, ?)`,
		orgID, name, domain, image); err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	return s.GetAppByName(ctx, name)
}
```

Update `scanApp` and every `SELECT` in `apps.go`
(`GetAppByName`, `GetAppByImage`, `ListApps`) to include `org_id` in the
column list, in this order:
`id, org_id, name, domain, image, container_id, status, env, created_at`,
and scan it into `&a.OrgID` in `scanApp` (right after `&a.ID`).

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "Add owning organization to apps"
```

---

## Ruling: 403 vs 404 for app-scoped access

Since app names stay a flat, globally-unique namespace (`/apps/{name}`,
not `/orgs/{org}/apps/{name}`), every app-scoped handler (get, deploy,
logs, env) returns **404** — not 403 — when the caller lacks membership
in the app's org, exactly like "doesn't exist." Distinguishing "exists
but you can't see it" from "doesn't exist" would let any authenticated
user enumerate other orgs' app names by probing for 403 vs 404. Org- and
user-*management* endpoints (create app in an org, add a member) DO
return 403 for a membership failure — there the org's existence isn't
being hidden (the caller already named it), only the action is denied.

---

### Task 7: Rewrite auth — API keys resolve to users

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`

**Interfaces:**
- Consumes: `store.GetUserByAPIKeyHash`, `store.TouchAPIKeyLastUsed`
  (Task 4), `authkey.Hash` (Task 5).
- Produces:
  - `userFromContext(ctx context.Context) *store.User` — every later
    handler task uses this inside `authMiddleware`-protected routes to
    get the caller.
  - `authMiddleware` now populates that context value; requests without
    a valid `Bearer <api-key>` get `401` exactly as before, but the
    comparison is now a store lookup, not a fixed-string compare.
  - `NewServer`'s signature and the `token`/`registryHost` fields are
    **unchanged** — `token` keeps its sub-project-1 meaning (the
    registry webhook's shared secret) and is untouched by this task.

- [ ] **Step 1: Write the failing tests**

Replace `internal/api/server_test.go` entirely:
```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

func TestHealthzIsUnauthenticated(t *testing.T) {
	s := NewServer(nil, nil, "webhook-secret", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func newAuthMiddlewareTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	user, err := st.CreateUser(ctx, "test-user", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("authkey.Generate: %v", err)
	}
	if _, err := st.CreateAPIKey(ctx, user.ID, authkey.Hash(key)); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	return NewServer(st, nil, "webhook-secret", "registry.example.com"), key
}

func TestAuthMiddlewareRejectsMissingToken(t *testing.T) {
	s, _ := newAuthMiddlewareTestServer(t)
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareAcceptsValidAPIKey(t *testing.T) {
	s, key := newAuthMiddlewareTestServer(t)
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRejectsUnknownAPIKey(t *testing.T) {
	s, _ := newAuthMiddlewareTestServer(t)
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-key")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewarePutsUserInContext(t *testing.T) {
	s, key := newAuthMiddlewareTestServer(t)
	var gotUser *store.User
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = userFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	protected.ServeHTTP(httptest.NewRecorder(), req)

	if gotUser == nil || gotUser.Username != "test-user" {
		t.Fatalf("expected the authenticated user in context, got %+v", gotUser)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/api/... -run TestAuthMiddleware`
Expected: FAIL (`userFromContext` undefined; old tests used a fixed
`"secret-token"` compare that no longer applies)

- [ ] **Step 3: Implement**

Replace `internal/api/server.go` entirely:
```go
package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"cubeship/internal/authkey"
	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

// localRegistryHost is where the daemon pulls app images from. The
// registry container publishes 127.0.0.1:5000 precisely so the daemon's
// own pulls stay on loopback: pulling the public registry.<domain> name
// would hairpin out to the VPS's public IP and require a valid ACME
// certificate to already exist, which the spec forbids as a
// precondition for deploying.
const localRegistryHost = "127.0.0.1:5000"

// webhookDeployTimeout bounds a deploy kicked off by a registry push.
// The webhook itself acks immediately, so this is not the registry's
// notification timeout — it just stops a wedged deploy running forever.
const webhookDeployTimeout = 10 * time.Minute

type Server struct {
	store *store.Store
	orch  *deploy.Orchestrator
	// token is the shared secret the registry's own push-notification
	// webhook authenticates with — a system-to-system credential,
	// unrelated to per-user API keys. See handleRegistryWebhook.
	token        string
	registryHost string
	mux          *http.ServeMux

	// deployWG tracks deploys started by the registry webhook, which run
	// in the background after the response is sent. Tests wait on it.
	deployWG sync.WaitGroup
}

func NewServer(s *store.Store, orch *deploy.Orchestrator, token, registryHost string) *Server {
	srv := &Server{
		store:        s,
		orch:         orch,
		token:        token,
		registryHost: registryHost,
		mux:          http.NewServeMux(),
	}
	srv.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv.mux.HandleFunc("POST /hooks/registry", srv.handleRegistryWebhook)
	srv.handleAuth("POST /apps", srv.handleCreateApp)
	srv.handleAuth("GET /apps", srv.handleListApps)
	srv.handleAuth("GET /apps/{name}", srv.handleGetApp)
	srv.handleAuth("POST /apps/{name}/deploy", srv.handleManualDeploy)
	srv.handleAuth("PUT /apps/{name}/env", srv.handleSetEnv)
	srv.handleAuth("GET /apps/{name}/logs", srv.handleGetLogs)
	return srv
}

func (s *Server) Router() http.Handler {
	return s.mux
}

type contextKey string

const userContextKey contextKey = "cubeship-user"

// userFromContext returns the authenticated caller set by authMiddleware.
// Only valid inside a handler registered via handleAuth.
func userFromContext(ctx context.Context) *store.User {
	u, _ := ctx.Value(userContextKey).(*store.User)
	return u
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		keyHash := authkey.Hash(strings.TrimPrefix(authHeader, prefix))
		user, err := s.store.GetUserByAPIKeyHash(r.Context(), keyHash)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.store.TouchAPIKeyLastUsed(r.Context(), keyHash)
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleAuth registers a handler on the mux behind authMiddleware.
func (s *Server) handleAuth(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.authMiddleware(h))
}

// localPullRef rewrites a public image reference
// (registry.<domain>/<org-slug>/<app>) into the loopback-published
// reference the daemon actually pulls. Only the repository part is
// kept; the host is replaced. See localRegistryHost.
func localPullRef(image, tag string) string {
	repo := image
	if i := strings.Index(image, "/"); i >= 0 {
		repo = image[i+1:]
	}
	return localRegistryHost + "/" + repo + ":" + tag
}
```

(`localPullRef`'s doc comment is the only substantive edit inside that
function's neighborhood — its body is unchanged, since it already just
strips the host and keeps whatever comes after the first `/`, which
continues to work whether that's `<app>` or `<org-slug>/<app>`.)

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/api/... -run 'TestHealthz|TestAuthMiddleware'`
Expected: PASS. The rest of the `api` package will not compile yet
(`apps_handlers_test.go` and friends still call the old `newTestServer`/
`authedRequest` shape) — that's expected and fixed in Task 8. Confirm
just this file's tests with `-run`, not the whole package, for this step.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/server_test.go
git commit -m "Resolve API keys to users in the auth middleware"
```

---

### Task 8: Authorization helper + org-scoped app CRUD

**Files:**
- Create: `internal/api/authz.go`
- Test: `internal/api/authz_test.go`
- Modify: `internal/api/apps_handlers.go`
- Modify: `internal/api/apps_handlers_test.go` (this is where the shared
  `newTestServer`/`authedRequest` helpers live — their signatures change
  here; Task 9 updates the other files that call them)

**Interfaces:**
- Consumes: `userFromContext` (Task 7), `store.GetMembership`,
  `store.Role`/`RoleAdmin`/`RoleMember` (Task 3), `store.App.OrgID`
  (Task 6).
- Produces:
  - `(*Server).authorizeOrg(r *http.Request, orgID int64, minRole store.Role) bool`
  - `(*Server).authorizeApp(r *http.Request, app *store.App, minRole store.Role) bool`
  - **Changed shared test helpers** (Tasks 9+ depend on this exact
    shape): `newTestServer(t) (*Server, string, *store.Organization)`
    returns a server, a super-admin's API key, and an org named `acme`;
    `authedRequest(method, path string, body []byte, apiKey string) *http.Request`
    now takes the key explicitly instead of a fixed string.
  - `POST /apps` request body gains a required `"org"` field (the org's
    slug); the response is unchanged (`name`, `domain`, `image`, `status`).

- [ ] **Step 1: Write the failing authz test**

`internal/api/authz_test.go`:
```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
)

func TestAuthorizeOrgSuperAdminAlwaysPasses(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	admin, _ := s.CreateUser(ctx, "root", true)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), userContextKey, admin))

	if !srv.authorizeOrg(req, org.ID, store.RoleAdmin) {
		t.Fatal("expected super-admin to be authorized regardless of membership")
	}
}

func TestAuthorizeOrgMemberPassesMemberButFailsAdmin(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := s.CreateUser(ctx, "employee1", false)
	s.AddMembership(ctx, user.ID, org.ID, store.RoleMember)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), userContextKey, user))

	if !srv.authorizeOrg(req, org.ID, store.RoleMember) {
		t.Fatal("expected a member to pass the member-level check")
	}
	if srv.authorizeOrg(req, org.ID, store.RoleAdmin) {
		t.Fatal("expected a member to fail the admin-level check")
	}
}

func TestAuthorizeOrgNoMembershipFails(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := s.CreateUser(ctx, "outsider", false)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), userContextKey, user))

	if srv.authorizeOrg(req, org.ID, store.RoleMember) {
		t.Fatal("expected a user with no membership to be unauthorized")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/api/... -run TestAuthorizeOrg`
Expected: FAIL (`authorizeOrg` undefined)

- [ ] **Step 3: Implement the authz helper**

`internal/api/authz.go`:
```go
package api

import (
	"net/http"

	"cubeship/internal/store"
)

// authorizeOrg reports whether the caller authenticated by
// authMiddleware may act on orgID at at least minRole. Super-admins are
// always authorized. An org admin satisfies both RoleAdmin and
// RoleMember checks; a member only satisfies RoleMember.
func (s *Server) authorizeOrg(r *http.Request, orgID int64, minRole store.Role) bool {
	user := userFromContext(r.Context())
	if user == nil {
		return false
	}
	if user.IsSuperAdmin {
		return true
	}
	role, err := s.store.GetMembership(r.Context(), user.ID, orgID)
	if err != nil {
		return false
	}
	if minRole == store.RoleMember {
		return true
	}
	return role == store.RoleAdmin
}

// authorizeApp is authorizeOrg for an app's owning organization.
func (s *Server) authorizeApp(r *http.Request, app *store.App, minRole store.Role) bool {
	return s.authorizeOrg(r, app.OrgID, minRole)
}
```

- [ ] **Step 4: Run authz tests to confirm they pass**

Run: `go test ./internal/api/... -run TestAuthorizeOrg`
Expected: PASS

- [ ] **Step 5: Update the shared test helpers and app-handler tests**

Replace `internal/api/apps_handlers_test.go` entirely:
```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

// newTestServer returns a server backed by a fresh in-memory store, an
// organization "acme", and an API key for a super-admin user — enough
// for tests that don't care about role boundaries. Tests that DO care
// about roles create their own additional users/memberships against
// srv.store directly.
func newTestServer(t *testing.T) (*Server, string, *store.Organization) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	org, err := s.CreateOrganization(ctx, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	user, err := s.CreateUser(ctx, "test-admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("authkey.Generate: %v", err)
	}
	if _, err := s.CreateAPIKey(ctx, user.ID, authkey.Hash(key)); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	return NewServer(s, nil, "webhook-secret", "registry.example.com"), key, org
}

func authedRequest(method, path string, body []byte, apiKey string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestCreateAppReturnsImagePath(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug})
	req := authedRequest(http.MethodPost, "/apps", body, key)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["image"] != "registry.example.com/acme/myapp" {
		t.Fatalf("expected image registry.example.com/acme/myapp, got %q", got["image"])
	}
}

func TestCreateAppMissingFields(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "org": org.Slug})
	req := authedRequest(http.MethodPost, "/apps", body, key)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateAppUnknownOrg(t *testing.T) {
	srv, key, _ := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": "no-such-org"})
	req := authedRequest(http.MethodPost, "/apps", body, key)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCreateAppRequiresMembership(t *testing.T) {
	srv, _, org := newTestServer(t)
	ctx := context.Background()
	outsider, _ := srv.store.CreateUser(ctx, "outsider", false)
	outsiderKey, _ := authkey.Generate()
	srv.store.CreateAPIKey(ctx, outsider.ID, authkey.Hash(outsiderKey))

	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug})
	req := authedRequest(http.MethodPost, "/apps", body, outsiderKey)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCreateAppDuplicateName(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug})

	rec1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec1, authedRequest(http.MethodPost, "/apps", body, key))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, authedRequest(http.MethodPost, "/apps", body, key))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d", rec2.Code)
	}
}

func TestListAndGetApp(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, "/apps", body, key))

	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, authedRequest(http.MethodGet, "/apps", nil, key))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}
	var apps []map[string]any
	json.Unmarshal(listRec.Body.Bytes(), &apps)
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	getRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(getRec, authedRequest(http.MethodGet, "/apps/myapp", nil, key))
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRec.Code)
	}

	missRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(missRec, authedRequest(http.MethodGet, "/apps/nope", nil, key))
	if missRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", missRec.Code)
	}
}

func TestGetAppHidesAppsFromOtherOrgs(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, "/apps", body, key))

	ctx := context.Background()
	outsider, _ := srv.store.CreateUser(ctx, "outsider", false)
	outsiderKey, _ := authkey.Generate()
	srv.store.CreateAPIKey(ctx, outsider.ID, authkey.Hash(outsiderKey))

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodGet, "/apps/myapp", nil, outsiderKey))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (don't reveal the app exists to an outsider), got %d", rec.Code)
	}
}
```

- [ ] **Step 6: Update the handlers**

Replace `internal/api/apps_handlers.go`'s three handlers (keep
`appResponse`/`toAppResponse`/`writeJSON` unchanged):
```go
func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Domain string `json:"domain"`
		Org    string `json:"org"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Domain == "" || req.Org == "" {
		http.Error(w, "name, domain and org are required", http.StatusBadRequest)
		return
	}

	org, err := s.store.GetOrganizationBySlug(r.Context(), req.Org)
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}
	if !s.authorizeOrg(r, org.ID, store.RoleMember) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := s.store.GetAppByName(r.Context(), req.Name); err == nil {
		http.Error(w, "app already exists", http.StatusConflict)
		return
	}

	image := s.registryHost + "/" + req.Org + "/" + req.Name
	app, err := s.store.CreateApp(r.Context(), org.ID, req.Name, req.Domain, image)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, toAppResponse(app))
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.ListApps(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]appResponse, 0, len(apps))
	for _, a := range apps {
		if !s.authorizeApp(r, a, store.RoleMember) {
			continue
		}
		resp = append(resp, toAppResponse(a))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	app, err := s.store.GetAppByName(r.Context(), name)
	if err != nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}
	if !s.authorizeApp(r, app, store.RoleMember) {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toAppResponse(app))
}
```

- [ ] **Step 7: Run this task's tests to confirm they pass**

Run: `go test ./internal/api/... -run 'TestAuthorizeOrg|TestCreateApp|TestListAndGetApp|TestGetAppHidesAppsFromOtherOrgs'`
Expected: PASS. The rest of the package still won't build
(`deploy_handlers_test.go`, `logs_handler_test.go`,
`webhook_handler_test.go` still call the 3-arg-return `newTestServer`
via other means, or construct apps without an org) — that's Task 9.

- [ ] **Step 8: Commit**

```bash
git add internal/api/authz.go internal/api/authz_test.go internal/api/apps_handlers.go internal/api/apps_handlers_test.go
git commit -m "Add org authorization and org-scoped app creation"
```

---

### Task 9: Authorize the remaining app handlers + fix the test ripple

**Files:**
- Modify: `internal/api/deploy_handlers.go` (add `authorizeApp` checks to
  `handleManualDeploy`, `handleSetEnv`, `handleGetLogs`)
- Modify: `internal/api/apps_handlers_test.go` (add one more shared
  helper, `testAPIKeyFor`, used by the files below)
- Modify: `internal/api/deploy_handlers_test.go`
- Modify: `internal/api/logs_handler_test.go`
- Modify: `internal/api/webhook_handler_test.go` (only its `CreateApp`
  call sites — the webhook route is unauthenticated by design, see its
  doc comment, so it needs no `authorizeApp` change and its tests need
  no API key)

**Interfaces:**
- Consumes: `(*Server).authorizeApp` (Task 8), `authkey.Generate`/`Hash`
  (Task 5), the org-taking `store.CreateApp` (Task 6).
- Produces: `testAPIKeyFor(t *testing.T, s *store.Store, isSuperAdmin bool) string`
  — a test helper for files that build their own store/server (with a
  custom orchestrator) instead of using `newTestServer`.

- [ ] **Step 1: Add the shared `testAPIKeyFor` helper**

Add to `internal/api/apps_handlers_test.go` (alongside `newTestServer`),
and add `"fmt"` and `"time"` to its imports:
```go
// testAPIKeyFor creates a user (super-admin if isSuperAdmin) and returns
// a fresh API key for them. Use this in tests that build their own
// store/server directly (e.g. with a custom orchestrator) instead of
// newTestServer.
func testAPIKeyFor(t *testing.T, s *store.Store, isSuperAdmin bool) string {
	t.Helper()
	username := fmt.Sprintf("test-user-%d", time.Now().UnixNano())
	user, err := s.CreateUser(context.Background(), username, isSuperAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("authkey.Generate: %v", err)
	}
	if _, err := s.CreateAPIKey(context.Background(), user.ID, authkey.Hash(key)); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	return key
}
```

- [ ] **Step 2: Write the failing test for deploy/env/logs authorization**

Add to `internal/api/deploy_handlers_test.go`:
```go
func TestManualDeployHidesAppFromOtherOrgs(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	outsiderKey := testAPIKeyFor(t, s, false)

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", []byte(`{}`), outsiderKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (don't reveal the app exists to an outsider), got %d", rec.Code)
	}
}
```

- [ ] **Step 3: Run to confirm it fails**

Run: `go test ./internal/api/... -run TestManualDeployHidesAppFromOtherOrgs`
Expected: FAIL — currently the whole package fails to *build* (every
other test file in this task's list still calls the old, unauthorized
`CreateApp`/`authedRequest` shapes), so this "failure" is a compile
error, not a runtime assertion failure. That's expected at this point in
the task; it resolves once Steps 4-6 update those files.

- [ ] **Step 4: Add authorization to the handlers**

In `internal/api/deploy_handlers.go`, add `"cubeship/internal/store"` to
the imports, and insert the same authorization check into all three
handlers right after their existing `GetAppByName` lookup (before any
other logic):
```go
	if !s.authorizeApp(r, app, store.RoleMember) {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}
```
Concretely: in `handleManualDeploy`, insert it right after the `if err
!= nil { http.Error(...); return }` block that follows
`s.store.GetAppByName`. Same placement in `handleSetEnv`. In
`handleGetLogs`, the existing lookup discards the app
(`if _, err := s.store.GetAppByName(...); err != nil`) — change that to
keep it (`app, err := s.store.GetAppByName(...)`) so the check has
something to authorize against, then insert the same block.

- [ ] **Step 5: Update `deploy_handlers_test.go` and `logs_handler_test.go`**

Replace `internal/api/deploy_handlers_test.go` entirely:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

func TestManualDeployEndpoint(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	key := testAPIKeyFor(t, s, true)

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 3
	orch.HealthCheckInterval = 0
	srv := NewServer(s, orch, "webhook-secret", "registry.example.com")

	body, _ := json.Marshal(map[string]string{"tag": "v2"})
	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", body, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if docker.pulledRef != "127.0.0.1:5000/acme/myapp:v2" {
		t.Fatalf("expected pull of 127.0.0.1:5000/acme/myapp:v2, got %q", docker.pulledRef)
	}
}

func TestManualDeployDefaultsToLatestTag(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	key := testAPIKeyFor(t, s, true)

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 3
	orch.HealthCheckInterval = 0
	srv := NewServer(s, orch, "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", []byte(`{}`), key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if docker.pulledRef != "127.0.0.1:5000/acme/myapp:latest" {
		t.Fatalf("expected default tag latest pulled over loopback, got %q", docker.pulledRef)
	}
}

func TestSetEnvEndpoint(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	key := testAPIKeyFor(t, s, true)

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	body, _ := json.Marshal(map[string]map[string]string{"vars": {"PORT": "9090"}})
	req := authedRequest(http.MethodPut, "/apps/myapp/env", body, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.Env["PORT"] != "9090" {
		t.Fatalf("expected env to be persisted, got %v", got.Env)
	}
}

func TestManualDeployUnknownApp(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	key := testAPIKeyFor(t, s, true)
	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/nope/deploy", []byte(`{}`), key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestManualDeployHidesAppFromOtherOrgs(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	outsiderKey := testAPIKeyFor(t, s, false)

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", []byte(`{}`), outsiderKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (don't reveal the app exists to an outsider), got %d", rec.Code)
	}
}
```

Replace `internal/api/logs_handler_test.go` entirely:
```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

func TestGetLogsStreamsContainerOutput(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	app, _ := s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")
	key := testAPIKeyFor(t, s, true)

	// Containers run without a TTY, so the Engine multiplexes stdout and
	// stderr behind an 8-byte binary frame header. The handler must
	// demultiplex; copying the raw stream prints binary garbage.
	docker := &webhookFakeDocker{
		logsContent: dockerStdoutFrame("hello from the app\n") + dockerStdoutFrame("second line\n"),
	}
	srv := NewServer(s, deploy.New(s, docker), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "hello from the app\nsecond line\n" {
		t.Fatalf("expected the demultiplexed log lines, got %q", rec.Body.String())
	}
}

func TestGetLogsBeforeFirstDeploy(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	key := testAPIKeyFor(t, s, true)

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}
```

- [ ] **Step 6: Update `webhook_handler_test.go`'s `CreateApp` call sites**

The webhook route bypasses `authMiddleware` entirely (see
`handleRegistryWebhook`'s doc comment), so nothing in this file needs an
API key — only `store.CreateApp`'s new leading `orgID` parameter. In
`internal/api/webhook_handler_test.go`, at each of these five call
sites, create an org first and pass its ID (the org can be created
fresh right before each call, or once per test if a test calls
`CreateApp` only once — match whatever's already in scope):

Line 115 (`TestRegistryWebhookTriggersDeployForMatchedApp`):
```go
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
```

Line 160 (`TestRegistryWebhookRejectsMissingToken`):
```go
	org, _ := s.CreateOrganization(context.Background(), "acme", "Acme Inc")
	s.CreateApp(context.Background(), org.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
```

Line 180 (`TestRegistryWebhookRejectsWrongToken`): same pattern as line 160.

Line 204 (`TestRegistryWebhookAcksBeforeDeployFinishes`): same pattern as line 115 (a `ctx` variable is already in scope there).

Line 245 (`TestRegistryWebhookConcurrentNotificationsDoNotRace`): same
pattern as line 160 (uses `context.Background()` inline there).

`TestRegistryWebhookIgnoresUnknownRepository` (around line 139) creates
no app at all — leave it untouched.

- [ ] **Step 7: Run the whole package's tests to confirm they pass**

Run: `go test ./internal/api/...`
Expected: PASS, all tests in the package.

- [ ] **Step 8: Commit**

```bash
git add internal/api
git commit -m "Authorize deploy, env, and logs handlers by organization"
```

---

### Task 10: Organization handlers

**Files:**
- Create: `internal/api/org_handlers.go`
- Test: `internal/api/org_handlers_test.go`
- Modify: `internal/api/server.go` (register the two routes)

**Interfaces:**
- Consumes: `userFromContext`, `store.CreateOrganization`,
  `store.ListOrganizations`, `store.ListMembershipsForUser` (Tasks 1, 3, 7).
- Produces (HTTP contract):
  - `POST /orgs` — body `{"slug":"...","name":"..."}` → 201 with
    `{"slug":"...","name":"..."}`; super-admin only, 403 otherwise; 400
    on missing fields.
  - `GET /orgs` — 200 with a JSON array of the same shape. A super-admin
    sees every org; anyone else sees only the orgs they're a member of.

- [ ] **Step 1: Write the failing test**

`internal/api/org_handlers_test.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
)

func TestCreateOrgRequiresSuperAdmin(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	nonAdminKey := testAPIKeyFor(t, s, false)

	body, _ := json.Marshal(map[string]string{"slug": "acme", "name": "Acme Inc"})
	req := authedRequest(http.MethodPost, "/orgs", body, nonAdminKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrgAsSuperAdmin(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	adminKey := testAPIKeyFor(t, s, true)

	body, _ := json.Marshal(map[string]string{"slug": "acme", "name": "Acme Inc"})
	req := authedRequest(http.MethodPost, "/orgs", body, adminKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["slug"] != "acme" || got["name"] != "Acme Inc" {
		t.Fatalf("unexpected response: %v", got)
	}
}

func TestListOrgsSuperAdminSeesAll(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateOrganization(ctx, "globex", "Globex Corp")
	adminKey := testAPIKeyFor(t, s, true)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := authedRequest(http.MethodGet, "/orgs", nil, adminKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	var got []map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("expected 2 orgs, got %d: %v", len(got), got)
	}
}

func TestListOrgsMemberSeesOnlyTheirOwn(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	acme, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateOrganization(ctx, "globex", "Globex Corp")
	user, _ := s.CreateUser(ctx, "employee1", false)
	s.AddMembership(ctx, user.ID, acme.ID, store.RoleMember)
	key, _ := s.CreateAPIKey(ctx, user.ID, "irrelevant")
	_ = key

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	rawKey := testAPIKeyForExistingUser(t, s, user.ID)
	req := authedRequest(http.MethodGet, "/orgs", nil, rawKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	var got []map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 1 || got[0]["slug"] != "acme" {
		t.Fatalf("expected only acme, got %v", got)
	}
}
```

Note this test needs one more small helper, `testAPIKeyForExistingUser`
(issue a key for a user you already created, as opposed to
`testAPIKeyFor` which creates the user too) — add it to
`internal/api/apps_handlers_test.go` next to `testAPIKeyFor`:
```go
func testAPIKeyForExistingUser(t *testing.T, s *store.Store, userID int64) string {
	t.Helper()
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("authkey.Generate: %v", err)
	}
	if _, err := s.CreateAPIKey(context.Background(), userID, authkey.Hash(key)); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	return key
}
```
And remove the unused `key, _ := s.CreateAPIKey(ctx, user.ID,
"irrelevant"); _ = key` lines from `TestListOrgsMemberSeesOnlyTheirOwn`
above — they were left over from drafting; the real key comes from
`testAPIKeyForExistingUser`.

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/api/... -run 'TestCreateOrg|TestListOrgs'`
Expected: FAIL (`handleCreateOrg`/routes undefined)

- [ ] **Step 3: Implement**

`internal/api/org_handlers.go`:
```go
package api

import (
	"encoding/json"
	"net/http"

	"cubeship/internal/store"
)

type orgResponse struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil || !user.IsSuperAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Slug == "" || req.Name == "" {
		http.Error(w, "slug and name are required", http.StatusBadRequest)
		return
	}

	org, err := s.store.CreateOrganization(r.Context(), req.Slug, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, orgResponse{Slug: org.Slug, Name: org.Name})
}

func (s *Server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if user.IsSuperAdmin {
		orgs, err := s.store.ListOrganizations(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]orgResponse, 0, len(orgs))
		for _, o := range orgs {
			resp = append(resp, orgResponse{Slug: o.Slug, Name: o.Name})
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	memberships, err := s.store.ListMembershipsForUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]orgResponse, 0, len(memberships))
	for _, m := range memberships {
		resp = append(resp, orgResponse{Slug: m.OrgSlug, Name: m.OrgName})
	}
	writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 4: Register the routes**

In `internal/api/server.go`, inside `NewServer`, add alongside the other
`handleAuth` calls:
```go
	srv.handleAuth("POST /orgs", srv.handleCreateOrg)
	srv.handleAuth("GET /orgs", srv.handleListOrgs)
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/api/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api
git commit -m "Add organization create/list endpoints"
```

---

### Task 11: User creation and API key rotation

**Files:**
- Create: `internal/api/user_handlers.go`
- Test: `internal/api/user_handlers_test.go`
- Modify: `internal/api/server.go` (register the two routes)

**Interfaces:**
- Consumes: `authkey.Generate`/`Hash` (Task 5), `store.CreateUser`,
  `store.AddMembership`, `store.CreateAPIKey`, `store.RevokeAPIKeysForUser`
  (Tasks 2-4), `(*Server).authorizeOrg` (Task 8).
- Produces (HTTP contract):
  - `POST /orgs/{slug}/users` — body `{"username":"...","role":"admin"|"member"}`
    → 201 with `{"username":"...","org":"...","role":"...","api_key":"..."}`
    (the raw key is returned exactly once, at creation, same as the
    spec's key-hashing design). Requires the caller to be super-admin or
    an admin of that org; 403 otherwise, 404 if the org doesn't exist,
    400 on a missing/invalid role.
  - `POST /users/me/api-key/rotate` — no body → 200 with
    `{"api_key":"..."}`; any authenticated user, acts on themselves only
    (revokes their own key(s), issues a new one).

- [ ] **Step 1: Write the failing test**

`internal/api/user_handlers_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
)

func TestCreateOrgUserRequiresOrgAdmin(t *testing.T) {
	srv, _, org := newTestServer(t)
	memberKey := testAPIKeyFor(t, srv.store, false)
	srv.store.AddMembership(nil, 0, 0, "") // placeholder call removed below; see Step 1 note

	body, _ := json.Marshal(map[string]string{"username": "employee1", "role": "member"})
	req := authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/users", body, memberKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrgUserAsSuperAdmin(t *testing.T) {
	srv, key, org := newTestServer(t)

	body, _ := json.Marshal(map[string]string{"username": "employee1", "role": "member"})
	req := authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/users", body, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["username"] != "employee1" || got["role"] != "member" || got["api_key"] == "" {
		t.Fatalf("unexpected response: %v", got)
	}

	// The returned key actually works.
	req2 := authedRequest(http.MethodGet, "/orgs", nil, got["api_key"])
	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected the new user's key to authenticate, got %d", rec2.Code)
	}
}

func TestCreateOrgUserInvalidRole(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "employee1", "role": "owner"})
	req := authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/users", body, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRotateAPIKeyIssuesWorkingKeyAndRevokesOld(t *testing.T) {
	srv, oldKey, _ := newTestServer(t)

	req := authedRequest(http.MethodPost, "/users/me/api-key/rotate", nil, oldKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	newKey := got["api_key"]
	if newKey == "" || newKey == oldKey {
		t.Fatalf("expected a new, different key, got %q", newKey)
	}

	oldReq := authedRequest(http.MethodGet, "/orgs", nil, oldKey)
	oldRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(oldRec, oldReq)
	if oldRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected the old key to be revoked, got %d", oldRec.Code)
	}

	newReq := authedRequest(http.MethodGet, "/orgs", nil, newKey)
	newRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(newRec, newReq)
	if newRec.Code != http.StatusOK {
		t.Fatalf("expected the new key to work, got %d", newRec.Code)
	}
}
```

Fix `TestCreateOrgUserRequiresOrgAdmin` before running it — the
`srv.store.AddMembership(nil, 0, 0, "")` line is a placeholder that must
not ship; delete it. The test as intended is simpler than that draft:
```go
func TestCreateOrgUserRequiresOrgAdmin(t *testing.T) {
	srv, _, org := newTestServer(t)
	memberKey := testAPIKeyFor(t, srv.store, false)

	body, _ := json.Marshal(map[string]string{"username": "employee1", "role": "member"})
	req := authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/users", body, memberKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
```
(`testAPIKeyFor` creates a plain non-super-admin user with no membership
in `org` at all, which is already enough to be forbidden — no need to
add a membership first.)

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/api/... -run 'TestCreateOrgUser|TestRotateAPIKey'`
Expected: FAIL (routes undefined)

- [ ] **Step 3: Implement**

`internal/api/user_handlers.go`:
```go
package api

import (
	"encoding/json"
	"net/http"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

func (s *Server) handleCreateOrgUser(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	org, err := s.store.GetOrganizationBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}
	if !s.authorizeOrg(r, org.ID, store.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}
	role := store.Role(req.Role)
	if role != store.RoleAdmin && role != store.RoleMember {
		http.Error(w, "role must be \"admin\" or \"member\"", http.StatusBadRequest)
		return
	}

	user, err := s.store.CreateUser(r.Context(), req.Username, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.AddMembership(r.Context(), user.ID, org.ID, role); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	key, err := authkey.Generate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.store.CreateAPIKey(r.Context(), user.ID, authkey.Hash(key)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"username": user.Username,
		"org":      org.Slug,
		"role":     string(role),
		"api_key":  key,
	})
}

func (s *Server) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := s.store.RevokeAPIKeysForUser(r.Context(), user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	key, err := authkey.Generate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.store.CreateAPIKey(r.Context(), user.ID, authkey.Hash(key)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
}
```

- [ ] **Step 4: Register the routes**

In `internal/api/server.go`, inside `NewServer`, add:
```go
	srv.handleAuth("POST /orgs/{slug}/users", srv.handleCreateOrgUser)
	srv.handleAuth("POST /users/me/api-key/rotate", srv.handleRotateAPIKey)
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/api/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api
git commit -m "Add user creation and API key rotation endpoints"
```

---

### Task 12: Bootstrap the super-admin on first boot

**Files:**
- Modify: `cmd/cubeshipd/main.go`
- Modify: `cmd/cubeshipd/main_test.go`

**Interfaces:**
- Consumes: `store.CountUsers`, `store.CreateUser`, `store.CreateAPIKey`
  (Tasks 2, 4), `authkey.Hash` (Task 5).
- Produces: `ensureSuperAdmin(ctx, s *store.Store, token string) error`
  (unexported, package `main`) — creates one super-admin user named
  `"admin"` the first time it runs against a database with zero users,
  seeding their API key from `token` (the same persisted/generated
  secret `config.Load` already produces as `cfg.Token`). A no-op on
  every later boot.

- [ ] **Step 1: Write the failing test**

`cmd/cubeshipd/main_test.go` already has `TestVersionFlag` (from the
sub-project 1 scaffold); add these, and add `"context"`,
`"cubeship/internal/authkey"`, and `"cubeship/internal/store"` to its
imports:
```go
func TestEnsureSuperAdminCreatesOnFirstBoot(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := ensureSuperAdmin(ctx, s, "bootstrap-token"); err != nil {
		t.Fatalf("ensureSuperAdmin: %v", err)
	}

	user, err := s.GetUserByAPIKeyHash(ctx, authkey.Hash("bootstrap-token"))
	if err != nil {
		t.Fatalf("GetUserByAPIKeyHash: %v", err)
	}
	if !user.IsSuperAdmin {
		t.Fatal("expected the bootstrapped user to be a super-admin")
	}
}

func TestEnsureSuperAdminIsIdempotent(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := ensureSuperAdmin(ctx, s, "first-token"); err != nil {
		t.Fatalf("ensureSuperAdmin (first call): %v", err)
	}
	if err := ensureSuperAdmin(ctx, s, "second-token"); err != nil {
		t.Fatalf("ensureSuperAdmin (second call): %v", err)
	}

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 user after two calls, got %d", n)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, authkey.Hash("second-token")); err == nil {
		t.Fatal("expected the second call to be a no-op, not seed a second key")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./cmd/cubeshipd/... -run TestEnsureSuperAdmin`
Expected: FAIL (`ensureSuperAdmin` undefined)

- [ ] **Step 3: Implement**

Add to `cmd/cubeshipd/main.go` (and add `"cubeship/internal/authkey"` to
its imports):
```go
// ensureSuperAdmin creates the instance's first user — a super-admin —
// the first time the daemon boots against a fresh database, seeding
// their API key from token (cfg.Token, the same persisted/generated
// secret config.Load already manages). A database that already has any
// users is left alone.
func ensureSuperAdmin(ctx context.Context, s *store.Store, token string) error {
	n, err := s.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	user, err := s.CreateUser(ctx, "admin", true)
	if err != nil {
		return err
	}
	if _, err := s.CreateAPIKey(ctx, user.ID, authkey.Hash(token)); err != nil {
		return err
	}
	log.Printf("cubeshipd: created super-admin user %q, API key seeded from the daemon token", user.Username)
	return nil
}
```

In `run()`, call it right after `store.Open` succeeds (before the
registry/Traefik bootstrap block, so the log line above appears near
the other startup logging):
```go
	s, err := store.Open(cfg.DataDir + "/cubeship.db")
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	if err := ensureSuperAdmin(ctx, s, cfg.Token); err != nil {
		return fmt.Errorf("bootstrap super-admin: %w", err)
	}
```
(This inserts right after the existing `defer s.Close()` line and before
the existing `notifyURL := ...` line — nothing else in that block
changes.)

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./cmd/cubeshipd/...`
Expected: PASS. Also run `go build ./...` to confirm the whole module
still compiles.

- [ ] **Step 5: Commit**

```bash
git add cmd/cubeshipd
git commit -m "Bootstrap a super-admin user on first boot"
```

---

### Task 13: CLI API client — orgs, users, key rotation

**Files:**
- Modify: `internal/apiclient/client.go`
- Modify: `internal/apiclient/client_test.go`

**Interfaces:**
- Consumes: the daemon HTTP contracts from Tasks 8, 10, 11.
- Produces:
  - `(*Client).CreateApp(ctx, name, domain, org string) (image string, err error)`
    (gains the `org` parameter; sends it as `"org"` in the request body)
  - `type apiclient.Org struct { Slug string; Name string }`
  - `(*Client).CreateOrg(ctx, slug, name string) error`
  - `(*Client).ListOrgs(ctx) ([]Org, error)`
  - `(*Client).CreateOrgUser(ctx, orgSlug, username, role string) (apiKey string, err error)`
  - `(*Client).RotateAPIKey(ctx) (apiKey string, err error)`

- [ ] **Step 1: Write the failing tests**

Update `TestCreateAppSendsAuthAndReturnsImage` in
`internal/apiclient/client_test.go` for the new signature and body
field:
```go
func TestCreateAppSendsAuthAndReturnsImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodPost || r.URL.Path != "/apps" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "myapp" || body["domain"] != "myapp.example.com" || body["org"] != "acme" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"image": "registry.example.com/acme/myapp"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	image, err := c.CreateApp(context.Background(), "myapp", "myapp.example.com", "acme")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if image != "registry.example.com/acme/myapp" {
		t.Fatalf("expected image registry.example.com/acme/myapp, got %q", image)
	}
}
```

Add these new tests to the same file:
```go
func TestCreateOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/orgs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["slug"] != "acme" || body["name"] != "Acme Inc" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.CreateOrg(context.Background(), "acme", "Acme Inc"); err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
}

func TestListOrgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]string{
			{"slug": "acme", "name": "Acme Inc"},
			{"slug": "globex", "name": "Globex Corp"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	orgs, err := c.ListOrgs(context.Background())
	if err != nil {
		t.Fatalf("ListOrgs: %v", err)
	}
	if len(orgs) != 2 || orgs[0].Slug != "acme" {
		t.Fatalf("unexpected orgs: %+v", orgs)
	}
}

func TestCreateOrgUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/orgs/acme/users" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "employee1" || body["role"] != "member" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"api_key": "new-key-123"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	key, err := c.CreateOrgUser(context.Background(), "acme", "employee1", "member")
	if err != nil {
		t.Fatalf("CreateOrgUser: %v", err)
	}
	if key != "new-key-123" {
		t.Fatalf("expected new-key-123, got %q", key)
	}
}

func TestRotateAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/users/me/api-key/rotate" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"api_key": "rotated-key-456"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	key, err := c.RotateAPIKey(context.Background())
	if err != nil {
		t.Fatalf("RotateAPIKey: %v", err)
	}
	if key != "rotated-key-456" {
		t.Fatalf("expected rotated-key-456, got %q", key)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/apiclient/...`
Expected: FAIL (`CreateApp` arity mismatch, `CreateOrg`/`ListOrgs`/
`CreateOrgUser`/`RotateAPIKey` undefined)

- [ ] **Step 3: Implement**

In `internal/apiclient/client.go`, change `CreateApp` and add the four
new methods:
```go
func (c *Client) CreateApp(ctx context.Context, name, domain, org string) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/apps", map[string]string{"name": name, "domain": domain, "org": org})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create app: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Image, nil
}

type Org struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (c *Client) CreateOrg(ctx context.Context, slug, name string) error {
	resp, err := c.do(ctx, http.MethodPost, "/orgs", map[string]string{"slug": slug, "name": name})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create org: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) ListOrgs(ctx context.Context) ([]Org, error) {
	resp, err := c.do(ctx, http.MethodGet, "/orgs", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list orgs: unexpected status %d", resp.StatusCode)
	}
	var out []Org
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateOrgUser(ctx context.Context, orgSlug, username, role string) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/orgs/"+orgSlug+"/users", map[string]string{"username": username, "role": role})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create user: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.APIKey, nil
}

func (c *Client) RotateAPIKey(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/users/me/api-key/rotate", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("rotate api key: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.APIKey, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/apiclient/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/apiclient
git commit -m "Add org, user, and API key rotation methods to the CLI client"
```

---

### Task 14: CLI — org and user commands

**Files:**
- Create: `cmd/cubeship/org.go`
- Create: `cmd/cubeship/user.go`
- Modify: `cmd/cubeship/main.go` (register the two new command trees)

**Interfaces:**
- Consumes: `apiclient.CreateOrg`, `ListOrgs`, `CreateOrgUser`,
  `RotateAPIKey` (Task 13), `newAPIClient()` (already defined in
  `cmd/cubeship/app.go`, reused here unchanged).
- Produces: `cubeship org create`, `cubeship org list`,
  `cubeship user create`, `cubeship user api-key rotate`.

This task is glue over already-tested `apiclient` methods — verified by
`go build` and a manual `--help` smoke check, not new unit tests, same
as sub-project 1's CLI task.

- [ ] **Step 1: Implement the org commands**

`cmd/cubeship/org.go`:
```go
package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newOrgCmd() *cobra.Command {
	orgCmd := &cobra.Command{Use: "org", Short: "Manage Cubeship organizations"}

	var slug string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new organization (super-admin only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.CreateOrg(context.Background(), slug, args[0]); err != nil {
				return err
			}
			fmt.Printf("Created organization %q (slug: %s)\n", args[0], slug)
			return nil
		},
	}
	createCmd.Flags().StringVar(&slug, "slug", "", "short identifier used in URLs and registry paths")
	createCmd.MarkFlagRequired("slug")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List organizations you belong to (or all, if super-admin)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			orgs, err := c.ListOrgs(context.Background())
			if err != nil {
				return err
			}
			for _, o := range orgs {
				fmt.Printf("%s\t%s\n", o.Slug, o.Name)
			}
			return nil
		},
	}

	orgCmd.AddCommand(createCmd, listCmd)
	return orgCmd
}
```

- [ ] **Step 2: Implement the user commands**

`cmd/cubeship/user.go`:
```go
package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newUserCmd() *cobra.Command {
	userCmd := &cobra.Command{Use: "user", Short: "Manage Cubeship users"}

	var org, role string
	createCmd := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a user in an organization and print their API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			key, err := c.CreateOrgUser(context.Background(), org, args[0], role)
			if err != nil {
				return err
			}
			fmt.Printf("Created user %q in %s (role: %s)\n", args[0], org, role)
			fmt.Printf("API key (shown once, save it now): %s\n", key)
			return nil
		},
	}
	createCmd.Flags().StringVar(&org, "org", "", "organization slug")
	createCmd.MarkFlagRequired("org")
	createCmd.Flags().StringVar(&role, "role", "member", "role within the org: admin or member")

	apiKeyCmd := &cobra.Command{Use: "api-key", Short: "Manage your own API key"}
	rotateCmd := &cobra.Command{
		Use:   "rotate",
		Short: "Revoke your current API key and issue a new one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			key, err := c.RotateAPIKey(context.Background())
			if err != nil {
				return err
			}
			fmt.Printf("New API key (shown once, save it now): %s\n", key)
			fmt.Println("Update your saved credentials: cubeship login <daemon-url> " + key)
			return nil
		},
	}
	apiKeyCmd.AddCommand(rotateCmd)

	userCmd.AddCommand(createCmd, apiKeyCmd)
	return userCmd
}
```

- [ ] **Step 3: Register the commands**

In `cmd/cubeship/main.go`, add alongside the existing `root.AddCommand`
calls:
```go
	root.AddCommand(newOrgCmd())
	root.AddCommand(newUserCmd())
```

- [ ] **Step 4: Build and smoke-test**

Run: `go build ./...`
Expected: succeeds.

Run: `go run ./cmd/cubeship org --help` and
`go run ./cmd/cubeship user --help`
Expected: both print their subcommands (`create`, `list` for org;
`create`, `api-key rotate` for user) with no errors.

- [ ] **Step 5: Commit**

```bash
git add cmd/cubeship
git commit -m "Add CLI commands for organizations and users"
```

---

### Task 15: CLI — `app create` gains `--org`

**Files:**
- Modify: `cmd/cubeship/app.go`

**Interfaces:**
- Consumes: the 4-arg `apiclient.CreateApp(ctx, name, domain, org string)` (Task 13).

- [ ] **Step 1: Update the command**

In `cmd/cubeship/app.go`, change the `createCmd` block:
```go
	var domain, org string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Register a new app and get its registry image path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			image, err := c.CreateApp(context.Background(), args[0], domain, org)
			if err != nil {
				return err
			}
			fmt.Printf("Created %s. Push to: %s\n", args[0], image)
			return nil
		},
	}
	createCmd.Flags().StringVar(&domain, "domain", "", "domain the app will be served on")
	createCmd.MarkFlagRequired("domain")
	createCmd.Flags().StringVar(&org, "org", "", "organization slug that will own this app")
	createCmd.MarkFlagRequired("org")
```
(This replaces the existing `var domain string` /  `createCmd` / two
`createCmd.Flags()...` lines — everything else in the file, including
`deployCmd`, `logsCmd`, and `envCmd`, is unchanged.)

- [ ] **Step 2: Build and smoke-test**

Run: `go build ./...`
Expected: succeeds.

Run: `go run ./cmd/cubeship app create --help`
Expected: shows both `--domain` and `--org` as required flags.

- [ ] **Step 3: Commit**

```bash
git add cmd/cubeship/app.go
git commit -m "Require --org on app create"
```

---

### Task 16: Update the Docker integration test for orgs

**Files:**
- Modify: `test/integration/deploy_test.go`

**Interfaces:**
- Consumes: `apiclient.CreateOrg` (Task 13), the org-aware
  `CreateApp` (Task 13), `ensureSuperAdmin` (Task 12, exercised
  implicitly — the daemon's existing `CUBESHIP_TOKEN` env var now also
  becomes the bootstrapped super-admin's API key, so this test needs no
  new auth setup at all).

This is the last task. It updates the one place sub-project 1's
Docker-based end-to-end test assumed a flat, org-less app namespace, and
runs the full test suite (unit + this integration test, if Docker is
available) as a final check that the whole plan holds together.

- [ ] **Step 1: Create an organization before creating the app**

In `test/integration/deploy_test.go`, right before the existing
`image, err := client.CreateApp(ctx, "myapp", "myapp.localtest.me")`
line, add:
```go
	if err := client.CreateOrg(ctx, "acme", "Acme Inc"); err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
```
`client` here is the same `apiclient.Client` constructed a few lines
above with `testToken` — the daemon's `ensureSuperAdmin` (Task 12)
already seeded that exact token as a super-admin's key on first boot, so
this call is already authorized with no extra setup.

- [ ] **Step 2: Pass the org through app creation and update the expected image**

Change:
```go
	image, err := client.CreateApp(ctx, "myapp", "myapp.localtest.me")
```
to:
```go
	image, err := client.CreateApp(ctx, "myapp", "myapp.localtest.me", "acme")
```
and change:
```go
	if image != "registry.localtest.me/myapp" {
```
to:
```go
	if image != "registry.localtest.me/acme/myapp" {
```

- [ ] **Step 3: Update the build/push targets to the org-prefixed path**

Change:
```go
	buildApp := exec.Command("docker", "build", "-t", "localhost:5000/myapp:latest", "./testapp")
```
to:
```go
	buildApp := exec.Command("docker", "build", "-t", "localhost:5000/acme/myapp:latest", "./testapp")
```
and change:
```go
	push := exec.Command("docker", "push", "localhost:5000/myapp:latest")
```
to:
```go
	push := exec.Command("docker", "push", "localhost:5000/acme/myapp:latest")
```
The `GET /apps/myapp` polling line and the `https://myapp.localtest.me/`
request are unchanged — the app's own name (`myapp`) and domain stay
global/unscoped per this plan's ruling; only the registry repository
path is org-prefixed.

- [ ] **Step 4: Run the full unit test suite**

Run: `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test -race -count=1 ./...`
Expected: all clean, every package passing.

- [ ] **Step 5: Run the live integration test if Docker is available**

Run: `go test -tags integration ./test/integration/... -v -timeout 5m`
Expected: the same result documented in sub-project 1 — the pipeline up
through the webhook-triggered deploy passes; the final
HTTPS-through-Traefik assertion is expected to fail only on Docker
Desktop for Mac/Windows (see the test file's own package doc comment).
If it fails at an earlier step than that (e.g. `CreateOrg`,
`CreateApp`, or the push itself), that's a real regression from this
task — fix it before committing, don't just note it.

- [ ] **Step 6: Commit**

```bash
git add test/integration/deploy_test.go
git commit -m "Update the integration test for org-scoped app creation"
```

---

## Plan Self-Review Notes

- **Spec coverage:** multi-org membership with per-org role (Tasks 3, 8),
  super-admin (Tasks 2, 8, 12), admin-only user creation (Task 11),
  API-key-based auth replacing the shared token (Tasks 5, 7), one active
  key per user / rotate-by-revoke (Tasks 4, 11), org-scoped registry
  image path (Task 8), CLI-only surface (Tasks 13-15, no web UI task
  exists). The spec's Docker Registry v2 token-auth section is
  deliberately **not** covered here — see the plan header's phasing note;
  it is its own follow-up plan once this one's users/orgs/API keys exist
  to build on.
- **Deviation from the written spec, confirmed intentional and
  documented:** app names stay globally unique (flat `/apps/{name}`)
  instead of `UNIQUE(org_id, name)` — see "Ruling carried from
  plan-writing" at the top of this plan.
- **Error-handling case from the spec with no implementation here:** the
  spec's error-handling section describes rejecting removal of an org's
  last admin. This plan implements no membership-removal endpoint at
  all (the spec's own CLI section only lists creation commands), so
  there is nothing yet that could trigger that case — deferred to
  whichever future plan adds membership removal.
- **Type consistency check:** `store.CreateApp`'s new leading `orgID
  int64` parameter is used consistently in every task that calls it
  (Tasks 6, 8, 9, 16, and the test-only call sites in Task 9's
  `webhook_handler_test.go` updates). `authedRequest`'s new trailing
  `apiKey string` parameter and `newTestServer`'s new 3-value return are
  used consistently everywhere they're called from Task 8 onward.
  `store.OrgMembership.OrgName` (added to Task 3 during self-review, to
  support Task 10's member-facing `GET /orgs` response) is threaded
  through its `SELECT`, its scan, and its test's struct literal
  consistently.