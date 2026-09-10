// Package release is what version this instance is and what changed in
// it.
//
// **The notes are in the binary, not fetched.** Cubeship runs on
// somebody's own VPS, which may be behind a firewall and shares an
// address with whoever else the provider put on it — so a dialog that
// asks GitHub what changed is one that is sometimes empty and sometimes
// rate-limited, which is worse than not having it. And there is nothing
// to fetch: the notes for the version that is running are known when it
// is built.
//
// What that buys, and it is the point: an instance can only ever show
// notes up to the version it is on, which is exactly the question
// somebody has after an upgrade.
package release

import (
	"bufio"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// notes are the release files, one per version.
//
// One file per release rather than one document with headings in it: a
// parser that finds a release by heading breaks the first time somebody
// writes a heading differently, and this is a thing a person edits by
// hand every few weeks. The root CHANGELOG.md is generated from these —
// see cmd/changelog.
//
//go:embed all:notes
var notes embed.FS

// Note is one release.
type Note struct {
	// Version is the release, without a leading v.
	Version string `json:"version"`
	// Date is when it went out, as written: YYYY-MM-DD.
	Date string `json:"date"`
	// Summary is one sentence, for a list of releases where the body
	// would be too much.
	Summary string `json:"summary"`
	// Body is the notes themselves, in Markdown.
	Body string `json:"body"`
	// Prerelease says this version is not a stable one. Derived from
	// the version rather than written down: semver says a version with
	// a hyphen in it is a prerelease, and two places to say so is one
	// place to disagree.
	Prerelease bool `json:"prerelease"`
}

// All is every release this build knows about, newest first.
func All() ([]Note, error) {
	entries, err := fs.ReadDir(notes, "notes")
	if err != nil {
		return nil, fmt.Errorf("read the release notes: %w", err)
	}
	out := make([]Note, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := notes.ReadFile("notes/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		n, err := parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return Compare(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

// Since is every release newer than `seen` and no newer than `current`,
// newest first.
//
// The upper bound is what makes this honest: an instance shows what
// changed in the version **it is running**, never in one it has not
// been upgraded to. A build carrying notes for a version ahead of it —
// which is what a release branch looks like mid-flight — would
// otherwise advertise something nobody can use.
//
// An empty `seen` is somebody who has never been shown anything, and
// gets the current release alone rather than the whole history: the
// question after an install is not "what has this project ever done".
func Since(all []Note, seen, current string) []Note {
	current = Normalize(current)
	if current == "" {
		// A build with no version stamped on it — `make dev`, or a
		// binary somebody built by hand. There is nothing to say it
		// changed *into*.
		return nil
	}
	seen = Normalize(seen)
	var out []Note
	for _, n := range all {
		if Compare(n.Version, current) > 0 {
			continue
		}
		if seen == "" {
			if n.Version == current {
				out = append(out, n)
			}
			continue
		}
		if Compare(n.Version, seen) > 0 {
			out = append(out, n)
		}
	}
	return out
}

// Normalize turns what a build or a column carries into a version this
// package can compare: a leading v goes, and anything that is not a
// version at all — "dev", "local", "" — becomes empty.
func Normalize(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if v == "" {
		return ""
	}
	if _, err := strconv.Atoi(strings.SplitN(strings.SplitN(v, "-", 2)[0], ".", 2)[0]); err != nil {
		return ""
	}
	return v
}

// Compare orders two versions the way semver does: -1, 0 or 1.
//
// The one rule that is not "compare the numbers" is the one that
// matters here: **a version with a prerelease suffix comes before the
// same version without one**, because 0.5.0-rc.1 is on the way to
// 0.5.0 rather than after it. Suffixes are compared as text, which is
// enough for rc.1 < rc.2 and is not asked to do more.
func Compare(a, b string) int {
	a, b = Normalize(a), Normalize(b)
	an, apre := split(a)
	bn, bpre := split(b)
	for i := range 3 {
		if an[i] != bn[i] {
			if an[i] < bn[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	case apre < bpre:
		return -1
	}
	return 1
}

// split reads a version into its three numbers and whatever came after
// a hyphen. A part that is not a number is zero, which makes a
// malformed version sort low rather than crash a listing.
func split(v string) ([3]int, string) {
	var nums [3]int
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	for i, part := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		nums[i], _ = strconv.Atoi(part)
	}
	return nums, pre
}

// parse reads one note file: a small YAML-ish header between `---`
// lines, then the body.
//
// Hand-rolled rather than a YAML dependency, and the header is
// deliberately three flat fields so it can stay that way. What this
// refuses is a file missing one of them — a release with no version is
// one nothing can order, and a release with no summary is a blank line
// in a list.
func parse(raw string) (Note, error) {
	s := bufio.NewScanner(strings.NewReader(raw))
	s.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !s.Scan() || strings.TrimSpace(s.Text()) != "---" {
		return Note{}, fmt.Errorf("a release note starts with a --- header")
	}
	var n Note
	for s.Scan() {
		line := s.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Note{}, fmt.Errorf("header line %q is not `key: value`", line)
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "version":
			n.Version = Normalize(value)
		case "date":
			n.Date = value
		case "summary":
			n.Summary = value
		default:
			return Note{}, fmt.Errorf("unknown header %q", strings.TrimSpace(key))
		}
	}
	var body strings.Builder
	for s.Scan() {
		body.WriteString(s.Text())
		body.WriteByte('\n')
	}
	if err := s.Err(); err != nil {
		return Note{}, err
	}
	switch {
	case n.Version == "":
		return Note{}, fmt.Errorf("no version in the header")
	case n.Date == "":
		return Note{}, fmt.Errorf("no date in the header")
	case n.Summary == "":
		return Note{}, fmt.Errorf("no summary in the header")
	}
	n.Body = strings.TrimSpace(body.String())
	n.Prerelease = strings.Contains(n.Version, "-")
	return n, nil
}
