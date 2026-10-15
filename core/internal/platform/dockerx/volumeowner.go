package dockerx

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
)

// Owner is who a directory belongs to, and its permission bits.
type Owner struct {
	UID, GID int
	Mode     fs.FileMode
}

// defaultVolumeMode is what a volume gets when the image has nothing at
// its path: what MkdirAll gave it before this existed.
const defaultVolumeMode fs.FileMode = 0o755

// PathOwner is the owner and mode an empty volume at path should start
// with, for a container of image: those of the image's own directory at
// path, or — when the image has none — the user the image runs as.
//
// This is what Docker does for a named volume on its first mount, and
// what a bind mount does not: a directory the daemon made is root's, and
// an image with a non-root USER cannot write to it. pgAdmin, running as
// 5050, exits on its first start.
//
// The image is read through a container that is created and never
// started, because the Engine only copies files out of a container.
func (c *Client) PathOwner(ctx context.Context, image, path string) (Owner, error) {
	info, _, err := c.api.ImageInspectWithRaw(ctx, image)
	if err != nil {
		return Owner{}, fmt.Errorf("inspect image %q: %w", image, err)
	}
	user := ""
	if info.Config != nil {
		user = info.Config.User
	}

	resp, err := c.api.ContainerCreate(ctx,
		// Never started, so the entrypoint is never looked for — an image
		// with no command at all is still one a container can be made of.
		&container.Config{Image: image, Entrypoint: []string{"/cubeship-reads-this-image"}},
		&container.HostConfig{NetworkMode: "none"}, nil, nil, "")
	if err != nil {
		return Owner{}, fmt.Errorf("create a container to read %q: %w", image, err)
	}
	defer func() {
		// With its volumes: an image that declares VOLUME at this path
		// gets an anonymous one on create, and without this each deploy
		// of an empty volume would leave one behind.
		_ = c.api.ContainerRemove(context.WithoutCancel(ctx), resp.ID,
			container.RemoveOptions{Force: true, RemoveVolumes: true})
	}()

	dir, err := c.copyOut(ctx, resp.ID, path)
	if err != nil {
		return Owner{}, err
	}
	if dir != nil {
		if o, ok := dirOwner(dir); ok {
			return o, nil
		}
	}

	if uid, gid, named := parseUser(user); !named {
		return Owner{UID: uid, GID: gid, Mode: defaultVolumeMode}, nil
	}
	passwd, err := c.copyOutFile(ctx, resp.ID, "/etc/passwd")
	if err != nil {
		return Owner{}, err
	}
	group, err := c.copyOutFile(ctx, resp.ID, "/etc/group")
	if err != nil {
		return Owner{}, err
	}
	uid, gid, err := resolveUser(user, passwd, group)
	if err != nil {
		return Owner{}, fmt.Errorf("image %q runs as %w", image, err)
	}
	return Owner{UID: uid, GID: gid, Mode: defaultVolumeMode}, nil
}

// copyOut is the tar the Engine answers for path, or nil when the image
// has nothing there. The caller reads what it needs and nothing more, so
// a large directory is never transferred: closing is what stops it.
func (c *Client) copyOut(ctx context.Context, id, path string) (*tar.Reader, error) {
	rc, _, err := c.api.CopyFromContainer(ctx, id, path)
	if errdefs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s from the image: %w", path, err)
	}
	// The first header is all dirOwner reads, so the buffer bounds what is
	// transferred before the close below.
	head, err := io.ReadAll(io.LimitReader(rc, 1<<16))
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("read %s from the image: %w", path, err)
	}
	return tar.NewReader(bytes.NewReader(head)), nil
}

// copyOutFile is one small file from the image, or nil when it has none.
func (c *Client) copyOutFile(ctx context.Context, id, path string) ([]byte, error) {
	rc, _, err := c.api.CopyFromContainer(ctx, id, path)
	if errdefs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s from the image: %w", path, err)
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	if _, err := tr.Next(); err != nil {
		return nil, fmt.Errorf("read %s from the image: %w", path, err)
	}
	return io.ReadAll(io.LimitReader(tr, 1<<20))
}

// dirOwner reads the first entry of the Engine's archive of a path, which
// is the path itself. Anything but a directory — a file, or a symlink the
// archive does not follow — is not an owner a volume can copy.
func dirOwner(tr *tar.Reader) (Owner, bool) {
	h, err := tr.Next()
	if err != nil || h.Typeflag != tar.TypeDir {
		return Owner{}, false
	}
	return Owner{UID: h.Uid, GID: h.Gid, Mode: fs.FileMode(h.Mode).Perm()}, true
}

// parseUser reads an image's USER as far as it can without the image's
// files: "" is root, and "uid" or "uid:gid" are numbers. named says a
// user or group is a name, which only /etc/passwd and /etc/group answer.
func parseUser(user string) (uid, gid int, named bool) {
	if user == "" {
		return 0, 0, false
	}
	name, group, hasGroup := strings.Cut(user, ":")
	uid, err := strconv.Atoi(name)
	if err != nil {
		return 0, 0, true
	}
	if !hasGroup {
		// A numeric user with no group runs in group 0 unless passwd says
		// otherwise; passwd is not worth reading for the one case.
		return uid, 0, false
	}
	gid, err = strconv.Atoi(group)
	if err != nil {
		return 0, 0, true
	}
	return uid, gid, false
}

// errNoSuchUser is a USER the image's own files do not name.
var errNoSuchUser = errors.New("a user or group its /etc/passwd and /etc/group do not name")

// resolveUser is the uid and gid of an image's USER, looked up in its
// passwd and group files: a user's group is its primary one unless USER
// names another.
func resolveUser(user string, passwd, group []byte) (uid, gid int, err error) {
	name, groupName, hasGroup := strings.Cut(user, ":")
	uid, gid = -1, -1
	if n, err := strconv.Atoi(name); err == nil {
		uid = n
		gid = 0
	}
	for _, f := range entries(passwd) {
		// name:password:uid:gid:...
		if len(f) < 4 || (f[0] != name && f[2] != name) {
			continue
		}
		u, errU := strconv.Atoi(f[2])
		g, errG := strconv.Atoi(f[3])
		if errU == nil && errG == nil {
			uid, gid = u, g
			break
		}
	}
	if uid < 0 {
		return 0, 0, fmt.Errorf("%q, %w", user, errNoSuchUser)
	}
	if !hasGroup {
		return uid, gid, nil
	}
	if n, err := strconv.Atoi(groupName); err == nil {
		return uid, n, nil
	}
	for _, f := range entries(group) {
		// name:password:gid:members
		if len(f) >= 3 && f[0] == groupName {
			if g, err := strconv.Atoi(f[2]); err == nil {
				return uid, g, nil
			}
		}
	}
	return 0, 0, fmt.Errorf("%q, %w", user, errNoSuchUser)
}

func entries(file []byte) [][]string {
	var out [][]string
	s := bufio.NewScanner(bytes.NewReader(file))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.Split(line, ":"))
	}
	return out
}

// PathOwners is the one Engine call SeedVolume needs.
type PathOwners interface {
	PathOwner(ctx context.Context, image, path string) (Owner, error)
}

// SeedVolume makes a volume's directory and, while it is empty, gives it
// the owner and mode the image has at path.
//
// **Only while it is empty.** A volume holding data belongs to whatever
// wrote it — a restore puts owners back, and an app may have chowned its
// own files — so a deploy never touches one. An empty one is asked about
// again on every deploy until something is written, which is a container
// created and removed, and nothing more.
//
// Owners only take when the daemon is root, which it is on a VPS; the
// mode takes either way.
func SeedVolume(ctx context.Context, engine PathOwners, dir, image, path string) error {
	if err := os.MkdirAll(dir, defaultVolumeMode); err != nil {
		return fmt.Errorf("make volume %s: %w", path, err)
	}
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("read volume %s: %w", path, err)
	}
	_, err = f.Readdirnames(1)
	f.Close()
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("read volume %s: %w", path, err)
		}
		return nil
	}

	o, err := engine.PathOwner(ctx, image, path)
	if err != nil {
		return fmt.Errorf("find who owns %s in the image: %w", path, err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(dir, o.UID, o.GID); err != nil {
			return fmt.Errorf("give volume %s to %d:%d: %w", path, o.UID, o.GID, err)
		}
	}
	if err := os.Chmod(dir, o.Mode); err != nil {
		return fmt.Errorf("set the mode of volume %s: %w", path, err)
	}
	return nil
}
