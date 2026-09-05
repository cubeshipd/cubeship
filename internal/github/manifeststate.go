package github

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"
)

// ManifestStateTTL is how long a registration may sit half-finished.
// Long enough to read GitHub's confirmation page and press the button,
// short enough that a nonce left behind by an abandoned attempt is not
// lying around to be spent later.
const ManifestStateTTL = 15 * time.Minute

// manifestState is one outstanding registration: who started it, when it
// stops being valid, and whether it was started in the knowledge that it
// replaces an App this instance already has.
type manifestState struct {
	user    int64
	replace bool
	expires time.Time
}

// manifestStates holds the single-use nonces that tie a manifest
// exchange to the flow that started it.
//
// The exchange is what makes this instance a GitHub App, and GitHub's
// conversion endpoint is unauthenticated: a code is a code, whoever made
// the manifest it came from. Without a nonce, a link to
// /github/app-created?code=… sent to a signed-in admin would register
// *the sender's* App — their private key, their webhook secret — with
// the admin's browser doing nothing but following a link, and land them
// on the install page for it, which is exactly the page they expected.
//
// So the nonce is issued here, travels to GitHub in the manifest form's
// `state` and comes back in the redirect, and the exchange refuses a
// state this daemon did not issue to this caller. It is held in memory
// rather than in a row: it is worth nothing after fifteen minutes, and a
// daemon that restarts mid-flow should make someone start again anyway.
type manifestStates struct {
	mu  sync.Mutex
	out map[string]manifestState
}

// issue mints a nonce for one registration and remembers it.
func (m *manifestStates) issue(userID int64, replace bool, now time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(raw)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.out == nil {
		m.out = make(map[string]manifestState)
	}
	// Abandoned flows are the common case — someone opens GitHub's page
	// and closes it — so the map is swept here rather than by a timer.
	for key, s := range m.out {
		if now.After(s.expires) {
			delete(m.out, key)
		}
	}
	m.out[state] = manifestState{user: userID, replace: replace, expires: now.Add(ManifestStateTTL)}
	return state, nil
}

// consume spends a nonce, and reports whether it was this caller's and
// still valid. A nonce is good once: the exchange behind it spends a
// single-use GitHub code, and a replay is somebody's second attempt at
// something that already happened.
func (m *manifestStates) consume(state string, userID int64, now time.Time) (manifestState, bool) {
	if state == "" {
		return manifestState{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, s := range m.out {
		// Constant time, because the comparison is against a secret this
		// caller is trying to guess if they are guessing at all.
		if subtle.ConstantTimeCompare([]byte(key), []byte(state)) != 1 {
			continue
		}
		if s.user != userID {
			// Left alone: this is not the flow it belongs to, and
			// spending it here would cancel somebody else's.
			return manifestState{}, false
		}
		delete(m.out, key)
		if now.After(s.expires) {
			return manifestState{}, false
		}
		return s, true
	}
	return manifestState{}, false
}
