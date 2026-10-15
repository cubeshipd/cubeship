package release_test

import (
	"net/http"
	"testing"

	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

type releases struct {
	Version string `json:"version"`
	Notes   []struct {
		Version string `json:"version"`
		Summary string `json:"summary"`
		Body    string `json:"body"`
	} `json:"notes"`
	Unseen []struct {
		Version string `json:"version"`
	} `json:"unseen"`
}

// A test's server has no version stamped on it, which is a developer's
// build — and that is exactly the case worth pinning: it has nothing to
// say changed, and the dialog stays away rather than showing the whole
// history to somebody running `make dev`.
func TestABuildWithNoVersionHasNothingToAnnounce(t *testing.T) {
	f := servertest.New(t)

	var out releases
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/releases", nil, f.AdminKey, &out), http.StatusOK)
	if out.Version != "" {
		t.Errorf("a build with nothing stamped on it reports version %q", out.Version)
	}
	if len(out.Unseen) != 0 {
		t.Errorf("it has %d releases to announce", len(out.Unseen))
	}
	// The history is still readable — the notes are in the binary
	// either way, and a developer looking one up should find it.
	if len(out.Notes) == 0 {
		t.Error("no release notes at all, and this build carries them")
	}
	for _, n := range out.Notes {
		if n.Summary == "" || n.Body == "" {
			t.Errorf("%s came back with a blank a dialog would render", n.Version)
		}
	}
}

// **A member reads them.** What changed in the software everyone on
// this instance is using is not a fact about its configuration, and a
// release note only the admin may read is one the person who noticed
// the change cannot look up.
func TestAMemberReadsTheReleaseNotes(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/releases", nil, memberKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/releases/seen", nil, memberKey), http.StatusNoContent)
}

// And nobody at all reads nothing.
func TestReleaseNotesNeedSomebodySignedIn(t *testing.T) {
	f := servertest.New(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/releases", nil, ""), http.StatusUnauthorized)
}
