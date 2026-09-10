// Command changelog writes the repository's CHANGELOG.md from the
// release notes the daemon carries.
//
// **Generated, so there is one place a release is written down.** The
// notes are per-version files because that is what the daemon embeds
// and what a person edits; this is the same content as one document,
// for whoever is reading on GitHub rather than in the app.
//
// `-check` writes nothing and fails when the file on disk is not what
// this would write, which is what CI runs: a release note added without
// the changelog following it is the file going stale, and the next
// person to look would not know which is right.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"cubeship/internal/release"
)

const path = "CHANGELOG.md"

const header = `# Changelog

Every release of Cubeship, newest first.

<!-- Generated from internal/release/notes by cmd/changelog. Edit a note
     there and run ` + "`make changelog`" + `; editing this file is editing the
     copy rather than the thing. -->
`

func main() {
	check := flag.Bool("check", false, "fail if CHANGELOG.md is not what this would write")
	flag.Parse()

	notes, err := release.All()
	if err != nil {
		fail(err)
	}

	var b strings.Builder
	b.WriteString(header)
	for _, n := range notes {
		fmt.Fprintf(&b, "\n## %s — %s\n\n", n.Version, n.Date)
		if n.Prerelease {
			b.WriteString("*Prerelease.*\n\n")
		}
		fmt.Fprintf(&b, "%s\n\n", n.Summary)
		b.WriteString(n.Body)
		b.WriteString("\n")
	}
	want := b.String()

	if !*check {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			fail(err)
		}
		return
	}

	got, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	if string(got) != want {
		fmt.Fprintf(os.Stderr, "%s is out of date: run `make changelog`\n", path)
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "changelog:", err)
	os.Exit(1)
}
