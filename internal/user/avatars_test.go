package user

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// profiles is where the dashboard keeps the faces, from this package.
const profiles = "../../web/public/profiles"

// **Adding a face is two edits, and this is what makes two safe.**
//
// The list lives here because the daemon is what refuses a name — the
// same reason Themes does — and the files live in the dashboard's own
// image, which the daemon cannot read: they are two containers. So the
// two halves cannot be derived from each other, and the only thing left
// is to notice when they disagree.
//
// Each way of disagreeing fails differently and neither is loud: a name
// here with no file there is a broken image on somebody's account, and
// a file there with no name here is a face nobody can choose and nobody
// knows is missing.
func TestEveryFaceIsBothANameAndAFile(t *testing.T) {
	entries, err := os.ReadDir(profiles)
	if err != nil {
		t.Fatalf("read %s: %v", profiles, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".png" {
			continue
		}
		files = append(files, strings.TrimSuffix(e.Name(), ".png"))
	}
	slices.Sort(files)

	named := slices.Clone(Avatars)
	slices.Sort(named)

	if !slices.Equal(files, named) {
		t.Errorf("the files are %v and user.Avatars is %v\n"+
			"a name with no file is a broken image; a file with no name cannot be chosen",
			files, named)
	}
}

// A name is a name, not a path: it goes straight into `/profiles/<n>.png`
// in an <img src>, and the daemon is the last thing between somebody's
// input and that.
func TestAFaceIsNamedAndNotAddressed(t *testing.T) {
	for _, name := range Avatars {
		if strings.ContainsAny(name, "/\\.: ") {
			t.Errorf("%q is not a name", name)
		}
	}
	for _, bad := range []string{"../secret", "/etc/passwd", "https://evil.example", "blue.png", ""} {
		if bad != "" && ValidAvatar(bad) {
			t.Errorf("ValidAvatar(%q) = true", bad)
		}
	}
	if !ValidAvatar("") {
		t.Error("empty is no face at all, which is what most accounts have")
	}
}
