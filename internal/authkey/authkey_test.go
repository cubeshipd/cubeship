package authkey

import "testing"

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
