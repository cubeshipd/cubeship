package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailedPublicationKeepsPreviousSnapshot(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "snapshot.json")
	previous := []byte(`{"last":"valid"}`)
	if err := os.WriteFile(out, previous, 0600); err != nil {
		t.Fatal(err)
	}
	if err := publish(filepath.Join(dir, "missing-repository"), out); err == nil {
		t.Fatal("accepted a missing repository")
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != string(previous) {
		t.Fatalf("previous snapshot changed: %s %v", data, err)
	}
}
