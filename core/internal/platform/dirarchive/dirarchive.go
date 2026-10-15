// Package dirarchive turns a directory into a .tar.gz stream and back.
//
// It is what a volume's backup is, on whichever machine the volume is:
// the control plane archives its own volumes, and a worker archives its
// own and sends them straight to S3. One copy of the format, so a backup
// taken on one machine restores on another.
package dirarchive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ErrEmpty is a directory with nothing in it. An app that keeps state has
// written something, so an empty archive is a copy that did not happen —
// and restoring it would empty the volume.
var ErrEmpty = errors.New("the volume is empty, so there is nothing to back up")

// Write writes every entry under dir, keeping modes and owners — a
// queue's files belong to the user its image runs as, and a restore that
// handed them to root would leave it unable to start. Symlinks are kept as
// links, never followed. It answers how many entries it wrote; dir itself
// is not one.
func Write(dir string, out io.Writer) (int, error) {
	gz := gzip.NewWriter(out)
	archive := tar.NewWriter(gz)
	entries := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		} else if !info.Mode().IsRegular() && !info.IsDir() {
			return nil // sockets and pipes are not data
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			header.Name += "/"
		}
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		entries++
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(archive, f)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("archive the volume: %w", err)
	}
	if err := archive.Close(); err != nil {
		return 0, fmt.Errorf("close the archive: %w", err)
	}
	return entries, gz.Close()
}

// Extract unpacks what Write wrote into dir. An entry that would land
// outside dir is refused rather than skipped: an archive with one is not
// one this instance wrote.
func Extract(in io.Reader, dir string) error {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	defer gz.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	archive := tar.NewReader(gz)
	for {
		h, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}
		name := filepath.FromSlash(strings.TrimSuffix(h.Name, "/"))
		if !filepath.IsLocal(name) {
			return fmt.Errorf("the archive has an entry outside the volume: %q", h.Name)
		}
		mode := os.FileMode(h.Mode) & os.ModePerm
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
			_ = root.Chmod(name, mode)
		case tar.TypeReg:
			f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, archive)
			if err := f.Close(); copyErr == nil {
				copyErr = err
			}
			if copyErr != nil {
				return copyErr
			}
		case tar.TypeSymlink:
			if err := root.Symlink(h.Linkname, name); err != nil {
				return err
			}
		default:
			continue
		}
		// Owners only take when the daemon is root, which it is on a VPS.
		_ = root.Lchown(name, h.Uid, h.Gid)
	}
}

// Replace puts an archive in dir's place. It is extracted beside dir
// first and swapped in only once that worked, so a restore that fails
// partway leaves the data as it was. The new dir keeps the owner and mode
// the current one has, since dir itself is not in the archive.
func Replace(in io.Reader, dir string) error {
	staged := dir + ".restore"
	previous := dir + ".previous"
	_ = os.RemoveAll(staged)
	if err := Extract(in, staged); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	if info, err := os.Stat(dir); err == nil {
		_ = os.Chmod(staged, info.Mode().Perm())
		if uid, gid, ok := owner(info); ok {
			_ = os.Lchown(staged, uid, gid)
		}
	}
	_ = os.RemoveAll(previous)
	if err := os.Rename(dir, previous); err != nil && !os.IsNotExist(err) {
		_ = os.RemoveAll(staged)
		return fmt.Errorf("move the current data aside: %w", err)
	}
	if err := os.Rename(staged, dir); err != nil {
		_ = os.Rename(previous, dir)
		_ = os.RemoveAll(staged)
		return fmt.Errorf("put the restored data in place: %w", err)
	}
	if err := os.RemoveAll(previous); err != nil {
		log.Printf("restore: removing the replaced data at %s: %v", previous, err)
	}
	return nil
}
