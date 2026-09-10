package user

import "testing"

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
func TestTheAuthenticationQueriesReadEveryUserColumn(t *testing.T) {
	got := qualify("u", userColumns)
	want := "u.id, u.username, u.role, u.theme, u.created_at"
	if got != want {
		t.Errorf("qualify(\"u\", userColumns) = %q, want %q", got, want)
	}
	if joinedUserColumns != got {
		t.Errorf("the joined list is %q", joinedUserColumns)
	}
}
