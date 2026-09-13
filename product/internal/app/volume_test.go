package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWhatAVolumePathMayBe(t *testing.T) {
	for in, want := range map[string]string{
		"/var/lib/rabbitmq":       "/var/lib/rabbitmq",
		"/data/":                  "/data",
		" /usr/share/es/../data ": "/usr/share/data",
		"/device":                 "/device",
	} {
		if got, err := CleanVolumePath(in); err != nil || got != want {
			t.Errorf("CleanVolumePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "data", "/", "/..", "/proc", "/proc/1", "/sys/fs", "/dev", "/a:b", "/a,b"} {
		if _, err := CleanVolumePath(bad); !errors.Is(err, ErrInvalidVolumePath) {
			t.Errorf("CleanVolumePath(%q) = %v, want ErrInvalidVolumePath", bad, err)
		}
	}
}

// Data kept after its volume is removed is found by what is on disk, and
// still says what it was.
func TestKeptVolumeDataIsListedByWhatIsOnDisk(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	for _, id := range []int64{3, 7} {
		if err := prepareVolume(dir, "web/production/api", &Volume{ID: id, Path: "/data", CreatedAt: created}); err != nil {
			t.Fatal(err)
		}
	}
	// A directory with no record, and something that is not a volume.
	if err := os.MkdirAll(filepath.Join(dir, "volumes", "9"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "volumes", "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := orphans(dir, map[int64]bool{7: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 9 || got[1].ID != 3 {
		t.Fatalf("orphans = %+v, want 9 then 3", got)
	}
	if got[1].App != "web/production/api" || got[1].Path != "/data" || !got[1].CreatedAt.Equal(created) {
		t.Errorf("kept data does not say what it was: %+v", got[1])
	}

	if err := removeVolumeData(dir, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(VolumeDir(dir, 3)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the data is still there: %v", err)
	}
	if _, err := os.Stat(volumeRecordPath(dir, 3)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the record is still there: %v", err)
	}
}

func TestAVolumePinsWhereAndHowManyAnAppRuns(t *testing.T) {
	yes, no := true, false
	a := &Scoped{App: App{Volumes: []Volume{{NodeSlug: "control-plane"}}}}
	for _, tc := range []struct {
		name string
		p    Placement
		want bool
	}{
		{"nothing changes", Placement{}, true},
		{"the same machine", Placement{Nodes: []string{"control-plane"}}, true},
		{"one copy", Placement{Replicas: 1}, true},
		{"spread off", Placement{Spread: &no}, true},
		{"another machine", Placement{Nodes: []string{"eu-1"}}, false},
		{"two machines", Placement{Nodes: []string{"control-plane", "eu-1"}}, false},
		{"two copies", Placement{Replicas: 2}, false},
		{"spread", Placement{Spread: &yes}, false},
	} {
		if got := keepsVolume(a, tc.p); got != tc.want {
			t.Errorf("%s: keepsVolume = %v, want %v", tc.name, got, tc.want)
		}
	}
}
