package user

import (
	"strings"
	"testing"
)

// **The two queries that resolve a credential to a person read the same
// columns as everything else.** They spelled the list themselves once,
// so a column added to `users` left them selecting one fewer than
// scanUser reads — and because those two are how an API key and a
// session cookie become a caller, every request on the instance
// answered 401.
//
// A test rather than a comment, because the failure does not look like
// its cause: nothing about "unauthorized" says a SELECT is one column
// short.
//
// **It pins the property, not the string.** It used to assert the exact
// list, which meant every column added to `users` broke it and the fix
// was to paste the new list in — a test edited that way is a test
// nobody reads, and it would have said nothing about the thing it
// exists for. What matters is that the joined list is *derived*: same
// length, same order, every name aliased and none of them written out
// again.
func TestTheAuthenticationQueriesReadEveryUserColumn(t *testing.T) {
	plain := split(userColumns)
	joined := split(joinedUserColumns)

	if len(joined) != len(plain) {
		t.Fatalf("the joined list has %d columns and the plain one %d", len(joined), len(plain))
	}
	for i, name := range plain {
		if want := "u." + name; joined[i] != want {
			t.Errorf("column %d is %q, want %q", i, joined[i], want)
		}
	}
	if len(plain) == 0 {
		t.Fatal("userColumns is empty, so this proves nothing")
	}
}

func split(list string) []string {
	out := strings.Split(list, ",")
	for i, name := range out {
		out[i] = strings.TrimSpace(name)
	}
	return out
}
