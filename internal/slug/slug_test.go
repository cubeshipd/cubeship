package slug

import "testing"

// The dashboard addresses a resource and its settings screen at the same
// path plus one segment. Next.js resolves a static segment before a
// dynamic one, so an app actually called "settings" would be a resource
// nothing could open — the settings screen would answer at its address
// instead, silently and forever.
//
// Refusing it at creation is the only place that can be caught while the
// person who typed it is still there to type another.
func TestReservedWordsAreRefused(t *testing.T) {
	if Valid("settings") {
		t.Error(`"settings" was accepted, and would be a resource with no address of its own`)
	}
	if !Reserved("settings") {
		t.Error("Reserved does not report why it was refused, so the caller cannot say")
	}

	// Only the exact word. A slug that merely contains it is fine —
	// nothing collides with a path segment but the segment itself.
	for _, ok := range []string{"settings-api", "my-settings", "setting"} {
		if !Valid(ok) {
			t.Errorf("%q was refused, but it is not a path segment the dashboard uses", ok)
		}
		if Reserved(ok) {
			t.Errorf("%q was reported as reserved", ok)
		}
	}
}

func TestShape(t *testing.T) {
	for _, ok := range []string{"api", "public-api", "a", "a1", "1a"} {
		if !Valid(ok) {
			t.Errorf("%q should be a valid slug", ok)
		}
	}
	for _, bad := range []string{"", "-api", "api-", "Api", "my api", "my/api", "café"} {
		if Valid(bad) {
			t.Errorf("%q should not be a valid slug", bad)
		}
	}
}
