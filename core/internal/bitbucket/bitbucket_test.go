package bitbucket

import "testing"

func TestParseRepositoryURL(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{{"https://bitbucket.org/acme/api.git", true}, {"https://bitbucket.org/acme/api/src/main", true}, {"https://github.com/acme/api", false}, {"git@bitbucket.org:acme/api.git", false}} {
		_, ok := ParseRepositoryURL(tc.in)
		if ok != tc.ok {
			t.Errorf("%q: got %v", tc.in, ok)
		}
	}
}
func TestBranchAndSignature(t *testing.T) {
	if b, ok := BranchOf("refs/heads/main"); !ok || b != "main" {
		t.Fatal(b, ok)
	}
	if err := VerifyWebhook([]byte("x"), "s", "bad"); err == nil {
		t.Fatal("accepted bad signature")
	}
}
