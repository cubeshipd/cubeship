package user

import (
	"errors"
	"testing"
)

func member(grants ...Grant) *User {
	return &User{Role: RoleMember, Policy: NewPolicy(grants)}
}

func TestAnAdminWithNoKeyRoleReachesEverything(t *testing.T) {
	admin := &User{Role: RoleAdmin}
	if err := Allow(admin, ResDatabases, LevelManage, "main"); err != nil || !admin.Admin() {
		t.Fatalf("an admin was refused: %v", err)
	}
}

func TestAGrantIsALevelOnSomeItems(t *testing.T) {
	u := member(Grant{Resource: ResApps, Level: LevelManage, Items: []string{"web"}},
		Grant{Resource: ResDatabases, Level: LevelView, Items: nil})

	if err := Allow(u, ResApps, LevelManage, "web"); err != nil {
		t.Errorf("manage on web was refused: %v", err)
	}
	if err := Allow(u, ResApps, LevelView, "shop"); !errors.Is(err, ErrHidden) {
		t.Errorf("an app outside the grant answered %v, want hidden", err)
	}
	if err := Allow(u, ResDatabases, LevelManage, "main"); !errors.Is(err, ErrForbidden) {
		t.Errorf("manage on a database granted view answered %v", err)
	}
	if err := Allow(u, ResFirewall, LevelView, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("an ungranted resource answered %v", err)
	}
}

// **An empty list of items is none, never every one.** A role whose
// projects were all deleted must reach nothing; reading it as no
// restriction would hand it the instance.
func TestAnEmptyItemListReachesNothing(t *testing.T) {
	u := member(Grant{Resource: ResApps, Level: LevelManage, Items: []string{}})
	if Sees(u, ResApps, "web") || CanAny(u, ResApps, LevelView) {
		t.Fatal("an empty item list reached an item")
	}
}

func TestSecretsAreTheirOwnGrant(t *testing.T) {
	u := member(Grant{Resource: ResApps, Level: LevelManage})
	if err := AllowSecrets(u, ResApps, "web"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manage without secrets read a secret: %v", err)
	}
	u = member(Grant{Resource: ResApps, Level: LevelView, Secrets: true})
	if err := AllowSecrets(u, ResApps, "web"); err != nil {
		t.Fatalf("view with secrets was refused: %v", err)
	}
}

// A key's role narrows its owner and never widens them.
func TestAKeyRoleOnlyNarrows(t *testing.T) {
	owner := NewPolicy([]Grant{{Resource: ResApps, Level: LevelView, Items: []string{"web", "shop"}}})
	key := NewPolicy([]Grant{
		{Resource: ResApps, Level: LevelManage, Secrets: true, Items: []string{"web", "blog"}},
		{Resource: ResDatabases, Level: LevelManage},
	})
	got := Intersect(owner, key)
	apps := got[ResApps]
	if apps.Level != LevelView || apps.Secrets || len(apps.Items) != 1 || apps.Items[0] != "web" {
		t.Errorf("apps came out %+v", apps)
	}
	if _, ok := got[ResDatabases]; ok {
		t.Error("the key's role added databases the owner does not have")
	}
	if Intersect(nil, key)[ResDatabases].Level != LevelManage {
		t.Error("an admin's key did not carry its role")
	}
}

func TestWithin(t *testing.T) {
	wide := NewPolicy([]Grant{{Resource: ResApps, Level: LevelManage, Secrets: true}})
	narrow := NewPolicy([]Grant{{Resource: ResApps, Level: LevelView, Items: []string{"web"}}})
	if !narrow.Within(wide) || wide.Within(narrow) {
		t.Fatal("within is the wrong way round")
	}
	if !wide.Within(nil) || Policy(nil).Within(wide) {
		t.Fatal("nil is not everything")
	}
}

func TestGrantsAreValidated(t *testing.T) {
	bad := [][]Grant{
		{{Resource: "users", Level: LevelView}},
		{{Resource: ResApps, Level: "admin"}},
		{{Resource: ResFirewall, Level: LevelView, Items: []string{"x"}}},
		{{Resource: ResFirewall, Level: LevelView, Secrets: true}},
		{{Resource: ResApps, Level: LevelView}, {Resource: ResApps, Level: LevelManage}},
	}
	for _, grants := range bad {
		if err := ValidateGrants(grants); !errors.Is(err, ErrBadGrant) {
			t.Errorf("%+v was accepted", grants)
		}
	}
	if err := ValidateGrants(DefaultMemberPolicy().Grants()); err != nil {
		t.Errorf("the member default is not valid: %v", err)
	}
}
