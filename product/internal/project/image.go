package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"cubeship/internal/user"
)

// What a project's picture may be.
//
// **Three types and a small ceiling**, because the dashboard sends
// something it has already shrunk: the browser has the file decoded to
// show a preview, so it scales it to a square and hands over a few tens
// of kilobytes. Nothing here resizes — the ceiling is what makes that
// safe rather than a hope, and refusing is a sentence somebody reads
// while they still have the file.
//
// The cap is deliberately not generous. This is an icon in a grid, and
// a megabyte of it is a megabyte through the daemon on every load of
// the projects screen.
const (
	// MaxImageBytes is the largest picture a project may wear.
	MaxImageBytes = 512 << 10
)

// ImageTypes are the media types a picture may be.
//
// PNG and JPEG because they are what a photo or a logo already is, and
// WebP because it is what a browser's canvas will hand back when asked
// for one. No SVG: it is a document with scripting in it, served from
// this instance's own origin beside the session cookie.
var ImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

var (
	// ErrNoImage is a project that wears none.
	ErrNoImage = errors.New("this project has no image")

	// ErrImageTooLarge refuses one over MaxImageBytes.
	ErrImageTooLarge = fmt.Errorf("an image must be %d KiB or smaller", MaxImageBytes>>10)

	// ErrImageType refuses anything that is not one of ImageTypes.
	ErrImageType = errors.New("an image must be a PNG, a JPEG or a WebP")
)

// imagePath is where a project's picture lives.
//
// Keyed by id rather than by slug, like a datastore's data directory
// and for the same reason: the row is what owns the file, and a name is
// a thing that could in principle be reused.
func (s *Service) imagePath(id int64) string {
	return filepath.Join(s.dataDir, "projects", strconv.FormatInt(id, 10))
}

// SetImage gives a project a picture, replacing whatever it had.
//
// **The bytes decide the type, not the header.** `Content-Type` is
// whatever the caller wrote, and this file is served back with the type
// recorded here — so a caller who could name it would be choosing what
// this instance serves from its own origin. `http.DetectContentType`
// reads the first bytes and that is what is believed.
//
// An admin's, like creating the project: a picture is part of what the
// instance looks like to everybody who opens it.
//
// The file is written before the row, and the row is what any screen
// reads — so a write that dies in the middle leaves bytes nobody
// serves rather than a row pointing at a file that is not there.
func (s *Service) SetImage(ctx context.Context, caller *user.User, slug string, body io.Reader) error {
	p, err := s.Resolve(ctx, caller, slug, user.RoleAdmin)
	if err != nil {
		return err
	}

	// One byte over the limit is read on purpose: a reader stopped at
	// exactly the cap cannot tell a file that fits from one that was
	// truncated.
	data, err := io.ReadAll(io.LimitReader(body, MaxImageBytes+1))
	if err != nil {
		return fmt.Errorf("read image: %w", err)
	}
	if len(data) > MaxImageBytes {
		return ErrImageTooLarge
	}
	if len(data) == 0 {
		return ErrImageType
	}

	mediaType := http.DetectContentType(data)
	if _, ok := ImageTypes[mediaType]; !ok {
		return ErrImageType
	}

	path := s.imagePath(p.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("make the image directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write image: %w", err)
	}
	return s.Repo().SetImage(ctx, p.ID, mediaType)
}

// Image is the picture a project wears, and the type to serve it as.
//
// A **member's**, unlike setting one: it is drawn on the projects grid,
// which every member opens.
func (s *Service) Image(ctx context.Context, caller *user.User, slug string) ([]byte, string, error) {
	p, err := s.Resolve(ctx, caller, slug, user.RoleMember)
	if err != nil {
		return nil, "", err
	}
	if !p.HasImage() {
		return nil, "", ErrNoImage
	}
	data, err := os.ReadFile(s.imagePath(p.ID))
	if err != nil {
		// The row says there is one and the disk disagrees — a data
		// directory restored without it, or a file removed by hand.
		// "No image" is the honest answer and the grid draws a mark.
		return nil, "", ErrNoImage
	}
	return data, p.Image, nil
}

// ClearImage takes the picture off a project.
//
// The row goes first here, the reverse of SetImage, and for the same
// reason: what a screen reads is the row, so the moment it says there
// is no picture there is none, whatever is still on the disk. The file
// is removed after, and a failure to remove it leaves bytes nobody
// serves.
func (s *Service) ClearImage(ctx context.Context, caller *user.User, slug string) error {
	p, err := s.Resolve(ctx, caller, slug, user.RoleAdmin)
	if err != nil {
		return err
	}
	if !p.HasImage() {
		return nil
	}
	if err := s.Repo().SetImage(ctx, p.ID, ""); err != nil {
		return err
	}
	if err := os.Remove(s.imagePath(p.ID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove image: %w", err)
	}
	return nil
}
