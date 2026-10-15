package user

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const MaxAvatarBytes = 512 << 10

var (
	ErrNoAvatar       = errors.New("this account has no profile image")
	ErrAvatarTooLarge = errors.New("a profile image must be 512 KiB or smaller")
	ErrAvatarType     = errors.New("a profile image must be a PNG, JPEG or WebP")
)

// AvatarURL is a versioned, same-origin image address. Legacy preset names
// are retained for old clients, but the dashboard displays initials for them.
func (u *User) AvatarURL() string {
	version, uploaded := strings.CutPrefix(u.Avatar, "upload:")
	if !uploaded {
		return ""
	}
	return "/api/users/" + url.PathEscape(u.Username) + "/avatar?v=" + url.QueryEscape(version)
}

// SetAvatar changes only the caller's picture. Bounded bytes and their sniffed
// media type are persisted atomically with the version, independently of profile
// edits. No SVG or caller-provided content type is served from the instance.
func (s *Service) SetAvatar(ctx context.Context, caller *User, body io.Reader) error {
	if caller == nil {
		return ErrUnauthenticated
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxAvatarBytes+1))
	if err != nil {
		return fmt.Errorf("read profile image: %w", err)
	}
	if len(data) > MaxAvatarBytes {
		return ErrAvatarTooLarge
	}
	mediaType := http.DetectContentType(data)
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return ErrAvatarType
	}
	version := fmt.Sprintf("upload:%x", sha256.Sum256(data))
	return s.Repo().SetAvatar(ctx, caller.ID, version, mediaType, data)
}

// Avatar is visible to signed-in users, like names in shared activity. Reading
// a picture does not grant access to the account's settings or credentials.
func (s *Service) Avatar(ctx context.Context, caller *User, username string) ([]byte, string, string, error) {
	if caller == nil {
		return nil, "", "", ErrUnauthenticated
	}
	if username == "me" {
		username = caller.Username
	}
	return s.Repo().Avatar(ctx, username)
}

func (s *Service) ClearAvatar(ctx context.Context, caller *User) error {
	if caller == nil {
		return ErrUnauthenticated
	}
	return s.Repo().ClearAvatar(ctx, caller.ID)
}
