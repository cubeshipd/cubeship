package user

import "testing"

func TestAccessIsOrdered(t *testing.T) {
	if !AccessRead.Within(AccessDeploy) || !AccessDeploy.Within(AccessFull) || AccessFull.Within(AccessDeploy) {
		t.Fatal("read ≤ deploy ≤ full does not hold")
	}
}

// A key whose projects were all deleted reaches nothing. Reading an empty
// list as "no restriction" would hand it the whole instance.
func TestAScopedKeyWithNoProjectsLeftSeesNone(t *testing.T) {
	u := &User{Key: &KeyScope{Access: AccessFull, Projects: []string{}}}
	if u.SeesProject("web") || !u.ProjectScoped() || !u.Key.Restricted() {
		t.Fatal("a key with an empty project list reaches a project")
	}
	unscoped := &User{Key: &KeyScope{Access: AccessFull}}
	if !unscoped.SeesProject("web") || unscoped.Key.Restricted() {
		t.Fatal("a full key reaching every project is treated as restricted")
	}
	if !(&User{}).SeesProject("web") {
		t.Fatal("a session does not see a project")
	}
}
