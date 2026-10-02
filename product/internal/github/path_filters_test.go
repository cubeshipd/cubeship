package github

import "testing"

func TestChangedFilesDeduplicatesAllChangeKinds(t *testing.T) {
	got := changedFiles([]pushCommit{
		{Added: []string{"a", "same"}, Modified: []string{"same", "b"}},
		{Removed: []string{"a", "c"}},
	})
	if len(got) != 4 || got[0] != "a" || got[1] != "same" || got[2] != "b" || got[3] != "c" {
		t.Fatalf("changedFiles() = %#v", got)
	}
}
