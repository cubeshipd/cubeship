package authkey

import (
	"strings"
	"testing"
)

func TestGenerateProducesUniqueKeys(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(a) != 64 {
		t.Fatalf("expected a 64-hex-char key, got %d chars: %q", len(a), a)
	}
	if a == b {
		t.Fatal("expected two calls to Generate to produce different keys")
	}
}

func TestHashIsDeterministicAndDistinct(t *testing.T) {
	h1 := Hash("my-secret-key")
	h2 := Hash("my-secret-key")
	if h1 != h2 {
		t.Fatalf("expected Hash to be deterministic, got %q and %q", h1, h2)
	}
	if Hash("a-different-key") == h1 {
		t.Fatal("expected different inputs to hash differently")
	}
	if h1 == "my-secret-key" {
		t.Fatal("expected Hash to actually transform the input, not return it verbatim")
	}
}

// A generated password is what stands between an exposed database — or
// an account somebody was handed — and whoever finds it, so it has to
// be long, random, and made of characters that survive being retyped.
func TestAGeneratedPasswordIsLongRandomAndTypeable(t *testing.T) {
	a, err := Password()
	if err != nil {
		t.Fatalf("Password: %v", err)
	}
	b, err := Password()
	if err != nil {
		t.Fatalf("Password: %v", err)
	}
	if a == b {
		t.Fatal("two calls produced the same password")
	}
	if len(a) != PasswordLength {
		t.Fatalf("password is %d characters, want %d", len(a), PasswordLength)
	}
	for _, c := range a {
		if !strings.ContainsRune(passwordAlphabet, c) {
			t.Fatalf("password contains %q, which is outside the alphabet", c)
		}
	}
}
