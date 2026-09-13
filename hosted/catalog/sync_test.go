package catalog

import (
	"context"
	"os"
	"slices"
	"testing"

	"cubeship/template"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../product/template/testdata/umami.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func umamiRepo(releases ...Release) Repo {
	return Repo{
		NodeID: "R_umami", ID: 7, Owner: "lucasaarch", Name: "cubeship-umami-template",
		URL: "https://github.com/lucasaarch/cubeship-umami-template", Stars: 3,
		Topics: []string{Topic, "analytics"}, Releases: releases,
	}
}

func release(tag, commit string) Release {
	return Release{Tag: tag, Commit: commit, URL: "https://github.com/x/y/releases/tag/" + tag, PublishedAt: published}
}

// complete puts every file a release needs at commit.
func complete(t *testing.T, gh *fakeGitHub, commit string) {
	t.Helper()
	prefix := "lucasaarch/cubeship-umami-template@" + commit + ":"
	gh.files[prefix+TemplateFile] = fixture(t)
	gh.files[prefix+ReadmeFile] = []byte("# Umami\n")
	gh.files[prefix+IconFile] = squarePNG(256)
}

type harness struct {
	gh     *fakeGitHub
	store  *fakeStore
	syncer *Syncer
}

func newHarness() *harness {
	h := &harness{
		gh:    &fakeGitHub{files: map[string][]byte{}, failing: map[string]bool{}, lookup: map[string]Repo{}},
		store: newStore(),
	}
	h.syncer = &Syncer{GitHub: h.gh, Store: h.store, Topic: Topic, Log: quietLog()}
	return h
}

func (h *harness) run(t *testing.T) Report {
	t.Helper()
	rep, err := h.syncer.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestACompleteReleaseIsAccepted(t *testing.T) {
	h := newHarness()
	h.gh.repos = []Repo{umamiRepo(release("v1.0.0", "abc"))}
	complete(t, h.gh, "abc")

	rep := h.run(t)
	if rep.Accepted != 1 || rep.Rejected != 0 || rep.Failed != 0 {
		t.Fatalf("report = %+v", rep)
	}
	rec := h.store.releases[0]
	if !rec.Accepted || rec.Manifest == nil || rec.Readme != "# Umami\n" || rec.Name != "v1.0.0" {
		t.Errorf("release = %+v", rec)
	}
	if len(rec.Icon) == 0 {
		t.Error("the icon was not kept")
	}
	if template.Blocks(rec.Problems) {
		t.Errorf("accepted with errors: %+v", rec.Problems)
	}
	if h.store.hidden[7] != "" {
		t.Errorf("hidden = %q", h.store.hidden[7])
	}
}

func TestAReleaseMissingFilesIsRejectedWithEveryReason(t *testing.T) {
	h := newHarness()
	h.gh.repos = []Repo{umamiRepo(release("v1.0.0", "abc"))}
	h.gh.files["lucasaarch/cubeship-umami-template@abc:template.yml"] = fixture(t)

	h.run(t)
	rec := h.store.releases[0]
	if rec.Accepted || rec.Manifest != nil || rec.Icon != nil {
		t.Fatalf("release = %+v", rec)
	}
	var codes []string
	for _, p := range rec.Problems {
		codes = append(codes, p.Code)
	}
	for _, want := range []string{"release.template-missing", "release.readme-missing", "release.icon-missing"} {
		if !slices.Contains(codes, want) {
			t.Errorf("no %s in %v", want, codes)
		}
	}
	if rec.Problems[0].Hint != "rename template.yml to template.yaml" {
		t.Errorf("hint = %q", rec.Problems[0].Hint)
	}
}

func TestAnInvalidTemplateIsRejectedWithTheValidatorsDiagnostics(t *testing.T) {
	h := newHarness()
	h.gh.repos = []Repo{umamiRepo(release("v1.0.0", "abc"))}
	complete(t, h.gh, "abc")
	h.gh.files["lucasaarch/cubeship-umami-template@abc:template.yaml"] = []byte("version: 1\nproject: x\napps: []\n")

	h.run(t)
	rec := h.store.releases[0]
	if rec.Accepted || !slices.ContainsFunc(rec.Problems, func(d template.Diagnostic) bool { return d.Code == "schema.too_small" }) {
		t.Fatalf("release = %+v", rec)
	}
	if rec.Icon != nil {
		t.Error("kept the icon of a rejected release")
	}
}

func TestABadIconIsRejected(t *testing.T) {
	h := newHarness()
	h.gh.repos = []Repo{umamiRepo(release("v1.0.0", "abc"))}
	complete(t, h.gh, "abc")
	h.gh.files["lucasaarch/cubeship-umami-template@abc:icon.png"] = squarePNG(64)

	h.run(t)
	if rec := h.store.releases[0]; rec.Accepted || rec.Problems[len(rec.Problems)-1].Code != "icon.size" {
		t.Fatalf("release = %+v", rec)
	}
}

func TestAReleaseIsReadOncePerCommit(t *testing.T) {
	h := newHarness()
	h.gh.repos = []Repo{umamiRepo(release("v1.0.0", "abc"))}
	complete(t, h.gh, "abc")
	h.run(t)
	h.run(t)
	if len(h.store.releases) != 1 {
		t.Fatalf("indexed %d times", len(h.store.releases))
	}

	// The tag moved: that is a different release.
	h.gh.repos = []Repo{umamiRepo(release("v1.0.0", "def"))}
	complete(t, h.gh, "def")
	h.run(t)
	if len(h.store.releases) != 2 || h.store.releases[1].Commit != "def" {
		t.Fatalf("releases = %+v", h.store.releases)
	}
}

func TestPrereleasesDraftsAndUnresolvedTagsAreSkipped(t *testing.T) {
	h := newHarness()
	pre, draft, loose := release("v2.0.0-rc.1", "p"), release("v2.0.0", "d"), release("v0.1.0", "")
	pre.Prerelease, draft.Draft = true, true
	h.gh.repos = []Repo{umamiRepo(pre, draft, loose)}
	if rep := h.run(t); rep.Accepted+rep.Rejected != 0 || len(h.store.releases) != 0 {
		t.Fatalf("indexed %+v", h.store.releases)
	}
}

// Nothing is recorded for a release GitHub did not finish answering
// about, so the next pass reads it again rather than keeping a
// rejection that was never the author's fault.
func TestATransientFailureRecordsNothing(t *testing.T) {
	h := newHarness()
	h.gh.repos = []Repo{umamiRepo(release("v1.0.0", "abc"))}
	complete(t, h.gh, "abc")
	h.gh.failing["lucasaarch/cubeship-umami-template@abc:README.md"] = true

	if rep := h.run(t); rep.Failed != 1 || len(h.store.releases) != 0 {
		t.Fatalf("report %+v, releases %+v", rep, h.store.releases)
	}
	delete(h.gh.failing, "lucasaarch/cubeship-umami-template@abc:README.md")
	if rep := h.run(t); rep.Accepted != 1 {
		t.Fatalf("second pass %+v", rep)
	}
}

func TestABlockedOwnerIsHiddenAndNotRead(t *testing.T) {
	h := newHarness()
	h.store.blocked["lucasaarch"] = true
	h.gh.repos = []Repo{umamiRepo(release("v1.0.0", "abc"))}
	complete(t, h.gh, "abc")

	h.run(t)
	if h.store.hidden[7] != HiddenBlocked || len(h.store.releases) != 0 {
		t.Fatalf("hidden %q, releases %d", h.store.hidden[7], len(h.store.releases))
	}
}

func TestARepositorySearchLostIsAskedAboutBeforeItIsHidden(t *testing.T) {
	h := newHarness()
	h.store.repos[7] = umamiRepo()
	h.store.repos[8] = Repo{NodeID: "R_gone", ID: 8}
	h.store.repos[9] = Repo{NodeID: "R_untagged", ID: 9}
	h.gh.lookup["R_umami"] = umamiRepo(release("v1.0.0", "abc"))
	h.gh.lookup["R_untagged"] = Repo{NodeID: "R_untagged", ID: 9, Owner: "x", Name: "y", Topics: []string{"other"}}
	complete(t, h.gh, "abc")

	h.run(t)
	if !slices.Equal(h.gh.looked, []string{"R_umami", "R_gone", "R_untagged"}) {
		t.Errorf("looked up %v", h.gh.looked)
	}
	if h.store.hidden[7] != "" || len(h.store.releases) != 1 {
		t.Errorf("a repository search merely missed was hidden (%q) or not read", h.store.hidden[7])
	}
	if h.store.hidden[8] != HiddenGone || h.store.hidden[9] != HiddenUntagged {
		t.Errorf("hidden = %v", h.store.hidden)
	}
}
