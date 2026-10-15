// Command templatecatalog publishes one validated templates commit as JSON.
package main

import (
	"cubeship/tools/internal/catalog"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	repo := flag.String("repo", ".", "templates repository checkout")
	out := flag.String("out", "snapshot.json", "snapshot output file")
	flag.Parse()
	if err := publish(*repo, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func publish(repo, out string) error {
	snapshot, err := catalog.Build(repo)
	if err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if len(data) > 32<<20 {
		return fmt.Errorf("catalog snapshot exceeds 32 MiB")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(out), ".snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), out)
}
