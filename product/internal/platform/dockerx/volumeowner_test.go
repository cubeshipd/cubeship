package dockerx

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
)

func (f *fakeAPI) CopyFromContainer(_ context.Context, _, path string) (io.ReadCloser, container.PathStat, error) {
	archive, ok := f.archives[path]
	if !ok {
		return nil, container.PathStat{}, errdefs.NotFound(errors.New("no such path"))
	}
	return io.NopCloser(bytes.NewReader(archive)), container.PathStat{}, nil
}

// archive is what the Engine answers for one path: a tar whose first
// entry is the path itself.
func archive(t *testing.T, h tar.Header, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	h.Size = int64(len(body))
	if err := w.WriteHeader(&h); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The image's own directory decides: pgAdmin's /var/lib/pgadmin is 5050:0
// with group write, and that is what an empty volume there must be.
func TestPathOwnerCopiesTheImagesDirectory(t *testing.T) {
	api := &fakeAPI{
		localImages: map[string]bool{"dpage/pgadmin4": true},
		imageUser:   "pgadmin",
		archives: map[string][]byte{
			"/var/lib/pgadmin": archive(t, tar.Header{Name: "pgadmin", Typeflag: tar.TypeDir, Mode: 0o775, Uid: 5050, Gid: 0}, ""),
		},
	}
	got, err := newWithAPI(api).PathOwner(context.Background(), "dpage/pgadmin4", "/var/lib/pgadmin")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Owner{UID: 5050, GID: 0, Mode: 0o775}); got != want {
		t.Errorf("owner = %+v, want %+v", got, want)
	}
	if api.removedID == "" {
		t.Error("the container made to read the image was not removed")
	}
}

// Nothing at the path in the image: the user the image runs as, looked up
// by name in its own passwd and group.
func TestPathOwnerFallsBackToTheImagesUser(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/sh\nrabbitmq:x:999:998::/var/lib/rabbitmq:/usr/sbin/nologin\n"
	group := "root:x:0:\nrabbitmq:x:998:\nstaff:x:50:\n"
	cases := []struct {
		user string
		want Owner
	}{
		{"", Owner{UID: 0, GID: 0, Mode: 0o755}},
		{"1000", Owner{UID: 1000, GID: 0, Mode: 0o755}},
		{"1000:1000", Owner{UID: 1000, GID: 1000, Mode: 0o755}},
		{"rabbitmq", Owner{UID: 999, GID: 998, Mode: 0o755}},
		{"rabbitmq:staff", Owner{UID: 999, GID: 50, Mode: 0o755}},
		{"999:staff", Owner{UID: 999, GID: 50, Mode: 0o755}},
	}
	for _, c := range cases {
		api := &fakeAPI{
			localImages: map[string]bool{"rabbitmq": true},
			imageUser:   c.user,
			archives: map[string][]byte{
				"/etc/passwd": archive(t, tar.Header{Name: "passwd", Typeflag: tar.TypeReg, Mode: 0o644}, passwd),
				"/etc/group":  archive(t, tar.Header{Name: "group", Typeflag: tar.TypeReg, Mode: 0o644}, group),
			},
		}
		got, err := newWithAPI(api).PathOwner(context.Background(), "rabbitmq", "/var/lib/rabbitmq")
		if err != nil {
			t.Errorf("user %q: %v", c.user, err)
			continue
		}
		if got != c.want {
			t.Errorf("user %q: owner = %+v, want %+v", c.user, got, c.want)
		}
	}
}

// A USER the image's own files do not name is refused rather than guessed
// as root, which is the one owner that is sure to be wrong.
func TestPathOwnerRefusesAUserTheImageDoesNotName(t *testing.T) {
	api := &fakeAPI{
		localImages: map[string]bool{"broken": true},
		imageUser:   "ghost",
		archives: map[string][]byte{
			"/etc/passwd": archive(t, tar.Header{Name: "passwd", Typeflag: tar.TypeReg, Mode: 0o644}, "root:x:0:0::/:/bin/sh\n"),
		},
	}
	if _, err := newWithAPI(api).PathOwner(context.Background(), "broken", "/data"); !errors.Is(err, errNoSuchUser) {
		t.Errorf("err = %v, want errNoSuchUser", err)
	}
}

type ownerAnswer struct {
	owner Owner
	asked int
}

func (o *ownerAnswer) PathOwner(context.Context, string, string) (Owner, error) {
	o.asked++
	return o.owner, nil
}

func TestSeedVolumeGivesAnEmptyVolumeTheImagesMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "volumes", "5")
	engine := &ownerAnswer{owner: Owner{UID: os.Getuid(), GID: os.Getgid(), Mode: 0o770}}

	if err := SeedVolume(context.Background(), engine, dir, "dpage/pgadmin4", "/var/lib/pgadmin"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o770 {
		t.Errorf("mode = %v, want %v", got, fs.FileMode(0o770))
	}
}

// Data is never re-owned: whatever wrote it, or a restore, decided.
func TestSeedVolumeLeavesAVolumeWithDataAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "queue"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	engine := &ownerAnswer{owner: Owner{Mode: 0o777}}

	if err := SeedVolume(context.Background(), engine, dir, "rabbitmq", "/var/lib/rabbitmq"); err != nil {
		t.Fatal(err)
	}
	if engine.asked != 0 {
		t.Error("the image was read for a volume that already holds data")
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want it left at 0700", info.Mode().Perm())
	}
}
