package template

import (
	"fmt"
	"slices"
	"strings"
)

// The extension rules, copied from the daemon's internal/datastore so
// that reading a template never needs the daemon — the same reason
// engines.go exists. TestExtensionsAgreeWithTheDaemon in rules_test.go
// runs every engine, every version and every combination through both
// and requires the same answer, which is what notices this going stale.

// extension is what a template needs to know about one.
type extension struct {
	name string
	// requires are added to a template's list rather than demanded of
	// it, exactly as the daemon adds them: there is one right answer to
	// "VectorChord needs pgvector", so nobody is asked to type it.
	requires []string
	// versions are the engine versions the daemon offers it at. Most of
	// these are contrib modules, which every Postgres image carries, so
	// most of them are every version; the two with an image of their own
	// are offered where there is a build to run.
	versions []string
}

// pgVersions is every Postgres version the daemon runs, newest first —
// which is what a contrib module is offered at.
var pgVersions = []string{"18", "17", "16", "15"}

// extensions are the ones the daemon offers, in the order it lists them.
// Only Postgres has any.
var extensions = []extension{
	{name: "pgvector", versions: pgVersions},
	{name: "vectorchord", requires: []string{"pgvector"}, versions: pgVersions},
	{name: "pg_trgm", versions: pgVersions},
	{name: "btree_gin", versions: pgVersions},
	{name: "btree_gist", versions: pgVersions},
	{name: "bloom", versions: pgVersions},
	{name: "citext", versions: pgVersions},
	{name: "cube", versions: pgVersions},
	{name: "earthdistance", requires: []string{"cube"}, versions: pgVersions},
	{name: "fuzzystrmatch", versions: pgVersions},
	{name: "hstore", versions: pgVersions},
	{name: "intarray", versions: pgVersions},
	{name: "isn", versions: pgVersions},
	{name: "ltree", versions: pgVersions},
	{name: "pgcrypto", versions: pgVersions},
	{name: "seg", versions: pgVersions},
	{name: "tablefunc", versions: pgVersions},
	{name: "tsm_system_rows", versions: pgVersions},
	{name: "unaccent", versions: pgVersions},
	{name: "uuid-ossp", versions: pgVersions},
	{name: "dblink", versions: pgVersions},
	{name: "postgres_fdw", versions: pgVersions},
	{name: "pg_prewarm", versions: pgVersions},
	{name: "pgstattuple", versions: pgVersions},
	{name: "pg_buffercache", versions: pgVersions},
	{name: "pg_stat_statements", versions: pgVersions},
}

// extensionEngine is the one engine that takes any.
const extensionEngine = "postgres"

func findExtension(name string) *extension {
	for i := range extensions {
		if extensions[i].name == name {
			return &extensions[i]
		}
	}
	return nil
}

// extensionProblem is one reason a template's `extensions:` is refused,
// with the diagnostic code that names it. An empty code is no problem.
type extensionProblem struct {
	code, message, hint string
}

// normalizeExtensions is the daemon's NormalizeExtensions, said in the
// terms a template is checked in: the same accept/reject and the same
// normalized list, which is what the parity test compares.
//
// version is the version the database will actually run — the template's
// own, or the engine's default when it names none — because which
// extensions exist is a fact about a version.
func normalizeExtensions(engine, version string, names []string) ([]string, extensionProblem) {
	if len(names) == 0 {
		return nil, extensionProblem{}
	}
	if engine != extensionEngine {
		return nil, extensionProblem{
			code:    "extension.engine",
			message: fmt.Sprintf("%s takes no extensions — only %s does", engine, extensionEngine),
		}
	}

	want := map[string]bool{}
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if findExtension(name) == nil {
			return nil, extensionProblem{
				code:    "extension.unknown",
				message: fmt.Sprintf("%q is not an extension this platform offers", raw),
				hint:    "the ones this platform offers are " + strings.Join(extensionNames(), ", "),
			}
		}
		want[name] = true
	}
	if len(want) == 0 {
		return nil, extensionProblem{}
	}
	for _, name := range sortedNames(want) {
		for _, dep := range findExtension(name).requires {
			want[dep] = true
		}
	}

	out := sortedNames(want)
	for _, name := range out {
		if !slices.Contains(findExtension(name).versions, version) {
			return nil, extensionProblem{
				code:    "extension.version",
				message: fmt.Sprintf("%s is not offered on %s %s", name, extensionEngine, version),
				hint:    "try " + strings.Join(findExtension(name).versions, ", "),
			}
		}
	}
	// Every extension offered at a version is offered by one image
	// there, so nothing else can fail — and the parity test is what
	// would notice the daemon growing a combination this does not know
	// about.
	return out, extensionProblem{}
}

func extensionNames() []string {
	names := make([]string, 0, len(extensions))
	for _, x := range extensions {
		names = append(names, x.name)
	}
	return names
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// extensionsSince is the first release whose instances create a database
// with extensions. An older one installs the template and creates the
// database **without** them, which is an Immich that comes up and then
// fails on its first vector query — so the gate is an error, not advice.
const extensionsSince = "0.9.0"

// withoutExtensions are the newest releases that do not, two for the
// reason withoutVolumes is two: a range leaves prereleases out unless it
// names one.
var withoutExtensions = []string{"0.8.999", "0.9.0-rc.0"}
