package authkey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
)

func Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// passwordAlphabet is letters and digits, and nothing else.
//
// A generated password is carried by hand — read off a screen, pasted
// into a sign-in form, sent to whoever it belongs to — and it is also
// what a connection string is built out of for a database. Punctuation
// survives none of that reliably: it is a shell quote here, a URL escape
// there, and a character somebody mistypes everywhere. A password
// somebody chooses may contain anything.
const passwordAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// PasswordLength is 24 characters of that alphabet — about 143 bits,
// which is far past what can be guessed at the rate anything here
// accepts a guess.
const PasswordLength = 24

// Password returns one nobody chose, for the places this instance fills
// a password in rather than asking: a database created without one, an
// account an admin opens for somebody else.
//
// It is here beside Generate rather than in either of them, because two
// generators would be two answers to what a password this instance
// invented looks like — and the weaker one would be found by whoever
// copied it next.
func Password() (string, error) {
	limit := big.NewInt(int64(len(passwordAlphabet)))
	out := make([]byte, PasswordLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", errors.New("generate password: " + err.Error())
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}
