package release

import "testing"

func TestVersionsOrderTheWaySemverSays(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
		why  string
	}{
		{"0.1.0", "0.1.0", 0, ""},
		{"0.2.0", "0.1.0", 1, ""},
		{"0.1.0", "0.10.0", -1, "the parts are numbers, not text"},
		{"1.0.0", "0.99.9", 1, ""},
		// The rule that is not "compare the numbers", and the one that
		// matters: a release candidate is on the way to a version
		// rather than after it.
		{"0.5.0-rc.1", "0.5.0", -1, "a candidate comes before the release it is a candidate for"},
		{"0.5.0-rc.1", "0.5.0-rc.2", -1, ""},
		{"0.5.0-rc.2", "0.4.9", 1, ""},
		// A build with nothing stamped on it sorts below everything,
		// rather than crashing a listing.
		{"dev", "0.1.0", -1, "an unstamped build is not a version"},
		{"v0.2.0", "0.2.0", 0, "a leading v is spelling, not meaning"},
	} {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d %s", tc.a, tc.b, got, tc.want, tc.why)
		}
		if got := Compare(tc.b, tc.a); got != -tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

var history = []Note{
	{Version: "0.4.0"}, {Version: "0.3.0"}, {Version: "0.3.0-rc.1"}, {Version: "0.2.0"}, {Version: "0.1.0"},
}

func versions(notes []Note) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Version)
	}
	return out
}

func TestWhatAnInstanceHasToSayAfterAnUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name, seen, current string
		want                []string
		why                 string
	}{
		{
			name: "two versions behind", seen: "0.2.0", current: "0.4.0",
			want: []string{"0.4.0", "0.3.0", "0.3.0-rc.1"},
			why:  "everything between, newest first",
		},
		{
			name: "up to date", seen: "0.4.0", current: "0.4.0", want: nil,
			why: "nothing changed since they last looked",
		},
		{
			// The one that keeps this honest: a build carrying notes
			// for a version ahead of it — which is what a release
			// branch looks like mid-flight — must not advertise
			// something nobody can run.
			name: "notes ahead of the build", seen: "0.1.0", current: "0.2.0",
			want: []string{"0.2.0"},
			why:  "never a version this instance is not on",
		},
		{
			// After an install the question is not "what has this
			// project ever done".
			name: "never seen anything", seen: "", current: "0.3.0",
			want: []string{"0.3.0"},
			why:  "the release they are on, not the whole history",
		},
		{
			name: "an unstamped build", seen: "0.1.0", current: "dev", want: nil,
			why: "there is nothing it changed into",
		},
	} {
		got := versions(Since(history, tc.seen, tc.current))
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v — %s", tc.name, got, tc.want, tc.why)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v, want %v — %s", tc.name, got, tc.want, tc.why)
				break
			}
		}
	}
}

// The notes in this build have to parse, and the newest of them has to
// be a version. It is the one test that reads the files a person writes
// by hand every few weeks — a header typed wrong is otherwise found by
// somebody opening the dialog after an upgrade.
func TestEveryNoteInThisBuildParses(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatalf("read the release notes: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("this build carries no release notes at all")
	}
	for _, n := range all {
		if n.Version == "" || n.Date == "" || n.Summary == "" || n.Body == "" {
			t.Errorf("%+v is missing something a dialog would show as a blank", n)
		}
	}
	for i := 1; i < len(all); i++ {
		if Compare(all[i-1].Version, all[i].Version) <= 0 {
			t.Errorf("%s is listed above %s", all[i-1].Version, all[i].Version)
		}
	}
}
