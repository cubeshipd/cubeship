package user

import (
	"errors"
	"time"
)

// Session is a signed-in browser.
//
// It is a row rather than a signed cookie because sessions have to be
// revocable: logging out ends one, and changing a password ends every
// other one the account holds. A stateless token cannot be taken back.
type Session struct {
	// TokenHash is what is stored. The token itself goes to the browser
	// once and is never written down here — a copy of this table does not
	// let anyone log in.
	TokenHash string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time

	LastUsedAt *time.Time
}

// SessionLifetime is how long a sign-in lasts. It is absolute rather
// than sliding: a session that is used every day still ends, which
// bounds how long a stolen cookie is worth anything.
const SessionLifetime = 30 * 24 * time.Hour

// SessionCookieName is the cookie the browser carries.
const SessionCookieName = "cubeship_session"

// ErrNoSession reports a cookie that matches no live session — expired,
// logged out, or never real.
var ErrNoSession = errors.New("not signed in")

// Expired reports whether s is past its lifetime.
func (s *Session) Expired(now time.Time) bool { return !now.Before(s.ExpiresAt) }
