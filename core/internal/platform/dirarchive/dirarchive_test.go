package dirarchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A volume comes back as it was: files, directories, modes and links.
func TestAVolumeArchiveRestoresWhatWasThere(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "mnesia", "queue.dat"), "messages", 0o600)
	mustWrite(t, filepath.Join(src, ".erlang.cookie"), "secret", 0o400)
	if err := os.Symlink("mnesia/queue.dat", filepath.Join(src, "latest")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	entries, err := Write(src, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 4 {
		t.Fatalf("want 4 entries (a dir, two files, a link), got %d", entries)
	}

	dst := filepath.Join(t.TempDir(), "restored")
	if err := Extract(&buf, dst); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dst, "mnesia", "queue.dat")); got != "messages" {
		t.Errorf("queue.dat = %q", got)
	}
	info, err := os.Stat(filepath.Join(dst, ".erlang.cookie"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Errorf("the cookie's mode is %v, want 0400", info.Mode().Perm())
	}
	if link, err := os.Readlink(filepath.Join(dst, "latest")); err != nil || link != "mnesia/queue.dat" {
		t.Errorf("the link is %q, %v", link, err)
	}
}

// An empty volume writes no entries, which a backup reports as a failure.
func TestAnEmptyVolumeHasNoEntries(t *testing.T) {
	var buf bytes.Buffer
	entries, err := Write(t.TempDir(), &buf)
	if err != nil || entries != 0 {
		t.Fatalf("entries %d, err %v", entries, err)
	}
}

// An archive naming a path outside the volume is refused.
func TestARestoreRefusesAnEntryOutsideTheVolume(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	archive := tar.NewWriter(gz)
	body := "x"
	_ = archive.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = archive.Write([]byte(body))
	_ = archive.Close()
	_ = gz.Close()

	parent := t.TempDir()
	if err := Extract(&buf, filepath.Join(parent, "v")); err == nil {
		t.Fatal("an entry outside the volume was accepted")
	}
	if _, err := os.Stat(filepath.Join(parent, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the entry was written outside the volume")
	}
}

func mustWrite(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
