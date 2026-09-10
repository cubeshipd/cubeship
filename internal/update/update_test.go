package update

import (
	"os"
	"testing"
	"time"
)

// The status file is what an instance mid-update knows about itself,
// and a browser that reloads is why it is a file: nothing a client
// remembers survives the daemon going away, and the daemon going away
// is the middle of the operation.
func TestTheRunSurvivesOnDisk(t *testing.T) {
	s := Store{DataDir: t.TempDir()}
	if s.Read() != nil {
		t.Fatal("a fresh instance reports a run it never had")
	}

	r := &Run{Version: "0.2.0", From: "0.1.0", Status: StatusRunning, StartedAt: time.Now()}
	if err := s.Write(r); err != nil {
		t.Fatal(err)
	}
	s.Step(r, "pulling the new images")
	s.Step(r, "replacing the daemon")

	back := s.Read()
	if !back.Running() {
		t.Fatalf("it came back as %+v", back)
	}
	if back.Step != "replacing the daemon" {
		t.Errorf("the step is %q", back.Step)
	}
	if len(back.Done) != 1 || back.Done[0] != "pulling the new images" {
		t.Errorf("what is done is %v, and a screen shows it as the progress", back.Done)
	}
}

// A file nothing can read is **no run**, not a broken one: the
// alternative is an instance that refuses every request for ever
// because something wrote nonsense into a status file, which is exactly
// the failure this mechanism exists to avoid.
func TestNonsenseOnDiskIsNotARunningUpdate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/"+StatusFile, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := (Store{DataDir: dir}).Read(); r != nil {
		t.Errorf("it read %+v out of a file that is not a run", r)
	}
}

// **A run that cannot end would lock the instance for ever.** The one
// way to leave one behind is the updater dying between stopping the
// daemon and starting it — the single moment nothing is left to write
// the file — so a record older than the patience is read as over.
func TestARunNothingIsComingForStopsCountingAsRunning(t *testing.T) {
	fresh := &Run{Status: StatusRunning, StartedAt: time.Now()}
	if fresh.Stale(StuckAfter) {
		t.Error("an update that started a moment ago was written off")
	}
	old := &Run{Status: StatusRunning, StartedAt: time.Now().Add(-StuckAfter - time.Minute)}
	if !old.Stale(StuckAfter) {
		t.Error("an update from an hour ago still locks the instance")
	}
	done := &Run{Status: StatusDone, StartedAt: time.Now().Add(-24 * time.Hour)}
	if done.Stale(StuckAfter) {
		t.Error("a finished run was called stale, which is a different thing")
	}
}

// Only the tag moves. The reference is what the daemon was told —
// install.sh passes it, and an operator may point it at a mirror — so a
// registry with a port in its host must not be mistaken for one.
func TestOnlyTheTagChanges(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ghcr.io/cubeshipd/cubeshipd:0.1.0", "ghcr.io/cubeshipd/cubeshipd:0.2.0"},
		{"ghcr.io/cubeshipd/cubeshipd", "ghcr.io/cubeshipd/cubeshipd:0.2.0"},
		{"registry.example.com:5000/mirror/cubeshipd", "registry.example.com:5000/mirror/cubeshipd:0.2.0"},
		{"registry.example.com:5000/mirror/cubeshipd:0.1.0", "registry.example.com:5000/mirror/cubeshipd:0.2.0"},
		{"", ""},
	} {
		if got := retag(tc.in, "0.2.0"); got != tc.want {
			t.Errorf("retag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
