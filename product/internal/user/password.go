package user

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Passwords are hashed with Argon2id, not with the SHA-256 that hashes
// API keys and session tokens.
//
// The difference is the input. An API key is 32 bytes of randomness, so
// guessing it is hopeless whatever the hash costs; a password is chosen
// by a person, so the hash has to be slow and memory-hard on purpose to
// make guessing expensive.
//
// These are the parameters RFC 9106 gives as its second recommended
// option: 64 MiB, three passes. They are stored alongside every hash, so
// raising them later leaves existing hashes verifiable.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonKeyLen  = 32
	argonSaltLen = 16
)

// ErrInvalidCredentials is what every failed sign-in returns, whether the
// username is unknown, the password is wrong, or the account has no
// password at all. Distinguishing them would tell an attacker which
// usernames exist.
var ErrInvalidCredentials = errors.New("incorrect username or password")

// ErrPasswordTooShort guards against the shortest passwords. It is a
// floor, not a policy: length is the only rule that reliably helps, and
// composition rules mostly produce worse passwords.
var ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)

// MinPasswordLength is that floor.
const MinPasswordLength = 12

// HashPassword derives a storable hash, in the PHC string format so the
// parameters travel with it.
func HashPassword(password string) (string, error) {
	if len([]rune(password)) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	parallelism := argonParallelism()
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, parallelism, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether password produced encoded.
//
// It re-derives with the parameters recorded in encoded rather than the
// current constants, so hashes written by an older build still verify
// after those constants change.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, time uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &parallelism); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// argonParallelism follows the machine, capped: a VPS with many cores
// should use them, and one with few should not promise lanes it cannot
// run in parallel.
func argonParallelism() uint8 {
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return uint8(n)
}

// dummyHash is verified against when no account matches, so that a
// sign-in with an unknown username costs the same as one with a known
// username and the wrong password. Without it, response time answers
// "does this account exist".
var dummyHash = mustHash("cubeship-timing-equalizer")

func mustHash(password string) string {
	encoded, err := HashPassword(password)
	if err != nil {
		panic("hashing the timing equalizer: " + err.Error())
	}
	return encoded
}
