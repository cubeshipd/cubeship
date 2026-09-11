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

// thumbSuffix is what `scripts/profiles.sh` names the small copy, which
// is the one the sidebar draws on every screen.
const thumbSuffix = "-sm"

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
		name := strings.TrimSuffix(e.Name(), ".png")
		if strings.HasSuffix(name, thumbSuffix) {
			continue // checked below, against the face it belongs to
		}
		files = append(files, name)
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

// **Every face has a thumbnail, because the sidebar asks for one on
// every screen.** It is the same `<img src>` derivation as the face
// itself — `/profiles/<name>-sm.png` — so a missing one is not a
// fallback to the large file, it is a broken image beside somebody's
// username for the whole time they are signed in.
//
// A face added by hand rather than through `scripts/profiles.sh` is
// exactly how that happens, and it is invisible from the Go side: the
// name resolves, the large file is there, and the one file nothing in
// this package mentions is the one that is missing.
func TestEveryFaceHasAThumbnail(t *testing.T) {
	for _, name := range Avatars {
		thumb := filepath.Join(profiles, name+thumbSuffix+".png")
		if _, err := os.Stat(thumb); err != nil {
			t.Errorf("%s has no thumbnail: %v\nrun scripts/profiles.sh", name, err)
		}
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
