package traefik_test

import (
	"os"
	"path/filepath"
	"testing"

	"cubeship/internal/platform/traefik"
)

func retryPath(dir string) string {
	return filepath.Join(dir, "traefik-dynamic", traefik.RetryFileName)
}

// What makes Traefik try again is the configuration being different
// from the last one it was handed, so asking twice has to leave the
// directory in two different states. Writing the same file twice is the
// case that looks like it works and does nothing at all: Traefik skips
// a configuration identical to the one it already has.
func TestAskingTwiceIsTwoDifferentDirectories(t *testing.T) {
	dir := t.TempDir()

	if there, err := traefik.RetryCertificates(dir); err != nil || !there {
		t.Fatalf("first ask: there=%v err=%v", there, err)
	}
	if _, err := os.Stat(retryPath(dir)); err != nil {
		t.Fatalf("the first ask wrote no file: %v", err)
	}

	if there, err := traefik.RetryCertificates(dir); err != nil || there {
		t.Fatalf("second ask: there=%v err=%v", there, err)
	}
	if _, err := os.Stat(retryPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("the second ask left the file: %v", err)
	}

	// And a third is a change again, so an instance that goes on
	// missing a certificate goes on asking.
	if there, err := traefik.RetryCertificates(dir); err != nil || !there {
		t.Fatalf("third ask: there=%v err=%v", there, err)
	}
}

// The routes are the one thing this must not touch. Rewriting those is
// either an identical document, which changes nothing, or a moment with
// a router missing, which is a name off the internet.
func TestAskingDoesNotTouchTheRoutes(t *testing.T) {
	dir := t.TempDir()
	if _, err := traefik.WriteRoutes(dir, []traefik.Route{{
		App: "web/production/api", Host: "api.example.com",
		Servers: []string{"http://one:8080"},
	}}, true); err != nil {
		t.Fatalf("write the routes: %v", err)
	}
	before, err := os.ReadFile(routesPath(dir))
	if err != nil {
		t.Fatalf("read the routes: %v", err)
	}

	for range 3 {
		if _, err := traefik.RetryCertificates(dir); err != nil {
			t.Fatalf("ask: %v", err)
		}
	}

	after, err := os.ReadFile(routesPath(dir))
	if err != nil {
		t.Fatalf("read the routes again: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("asking for a certificate rewrote the routes:\n%s", after)
	}
}
