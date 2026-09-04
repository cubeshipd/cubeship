package app

import (
	"testing"

	"cubeship/internal/org"
)

// Running an image somebody already published is what a member is for.
// Turning source into an image means executing whatever that source
// contains, on this host, with the builder's privileges — a different
// kind of act, and an admin's.
//
// No source builds yet. This pins the rule so that adding one is a
// decision about its role rather than an accident: the first source that
// builds fails this test until someone looks at it.
func TestBuildingSourcesNeedAnAdmin(t *testing.T) {
	for _, s := range []Source{SourceRegistry, SourceExternal} {
		if s.Builds() {
			t.Fatalf("%q builds now — check that its role below is deliberate", s)
		}
		if got := RoleToDeploy(s); got != org.RoleMember {
			t.Errorf("RoleToDeploy(%q) = %q, want %q", s, got, org.RoleMember)
		}
	}
}
