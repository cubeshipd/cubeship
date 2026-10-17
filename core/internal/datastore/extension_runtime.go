package datastore

import (
	"fmt"
	"slices"
	"strings"
)

// postgresBuilds is the support matrix for the extensions that are not
// in the plain image: which of them each Postgres version can run, and
// what image that combination takes.
//
// Only those. The contrib modules — pg_trgm, hstore, pgcrypto and the
// rest — are inside the `postgresql-<major>` package every image here is
// built on, so they need no row and no image: they are offered at every
// version, and creating one is a statement against the container that is
// already running.
//
// Ordered from fewest extensions to most, per version, because that is
// the order buildFor searches: asking for pgvector alone gets the
// pgvector image, not the larger one that also carries VectorChord.
//
// The images are the projects' own, pinned to a digest that carries both
// linux/amd64 and linux/arm64 — an instance on an ARM box is a normal
// Cubeship install, and an image that is not built for it fails with
// "exec format error" minutes after somebody clicks create.
//
// **PostGIS is the one that is missing and should not be.** postgis's
// own images publish linux/amd64 only, so half the instances Cubeship
// runs on could not start one — and an extension offered on a form that
// fails on an ARM box is worse than one that was never offered. It goes
// in when there is a multi-platform build to pin.
var postgresBuilds = map[string][]postgresBuild{
	"15": {
		{
			provides: []Extension{ExtensionPgvector},
			image:    "pgvector/pgvector", tag: "0.8.6-pg15",
			digest: "sha256:a947c45cdc5906a1bc951f20a8709e321256343ee0f251e4ae00b5e7def4e6da",
		},
		{
			provides: []Extension{ExtensionPgvector, ExtensionVectorChord},
			image:    "ghcr.io/tensorchord/vchord-postgres", tag: "pg15-v1.1.1",
			digest:  "sha256:56771998ae0e8c6ec82563ec7a98966f12a2d40fe065a807d1d3bb5ec3610a09",
			preload: []string{"vchord", "vector"},
		},
	},
	"16": {
		{
			provides: []Extension{ExtensionPgvector},
			image:    "pgvector/pgvector", tag: "0.8.6-pg16",
			digest: "sha256:ccc6e83d6e35e931dc7c5def2022729d5a6c370318d099181995567ff1fb4d6b",
		},
		{
			provides: []Extension{ExtensionPgvector, ExtensionVectorChord},
			image:    "ghcr.io/tensorchord/vchord-postgres", tag: "pg16-v1.1.1",
			digest:  "sha256:d12a579a95c5ea7bb0294181eddf091ab790eebd37f40c49b6de221e2fb756ed",
			preload: []string{"vchord", "vector"},
		},
	},
	"17": {
		{
			provides: []Extension{ExtensionPgvector},
			image:    "pgvector/pgvector", tag: "0.8.6-pg17",
			digest: "sha256:cf134a767f474095eeba57e0117be8e568e011a63f33fbf252f14c9b760f8e6f",
		},
		{
			provides: []Extension{ExtensionPgvector, ExtensionVectorChord},
			image:    "ghcr.io/tensorchord/vchord-postgres", tag: "pg17-v1.1.1",
			digest:  "sha256:22fb63e5a505601bf80975c858591558ca1b48952c1cbb3e9124c51b68f8aa8d",
			preload: []string{"vchord", "vector"},
		},
	},
	"18": {
		{
			provides: []Extension{ExtensionPgvector},
			image:    "pgvector/pgvector", tag: "0.8.6-pg18",
			digest: "sha256:2ba9ca5f2e7daa0f0e7723cba1ee9167bab54efd3640516a44ac1a928dd67e7a",
		},
		{
			provides: []Extension{ExtensionPgvector, ExtensionVectorChord},
			image:    "ghcr.io/tensorchord/vchord-postgres", tag: "pg18-v1.1.1",
			digest:  "sha256:700ff05ebdc7e4afd4dd8442e0684293046c3833677fec326550000a04cf8a91",
			preload: []string{"vchord", "vector"},
		},
	},
}

// SupportedExtensions are the extensions e offers at this version, in
// the order AllExtensions lists them.
//
// Empty for every engine but Postgres, and that is the answer rather
// than an omission: the four others run images nobody here has picked an
// extension build of, and offering a checkbox that cannot be honoured is
// worse than not offering one.
//
// Two sources, and the difference is where the code lives: a contrib
// module is in the server package every image here is built on, so it is
// offered at every version; the rest are offered where postgresBuilds
// has an image carrying them.
func SupportedExtensions(e Engine, version string) []Extension {
	if e != EnginePostgres {
		return nil
	}
	if !e.KnowsVersion(version) {
		return nil
	}
	fromImage := map[Extension]bool{}
	for _, b := range postgresBuilds[version] {
		for _, x := range b.provides {
			fromImage[x] = true
		}
	}
	var out []Extension
	for _, x := range extensionOrder {
		if !extensionSpecs[x].image || fromImage[x] {
			out = append(out, x)
		}
	}
	return out
}

// buildFor is the image that serves want at this version: the first one
// carrying everything asked for.
//
// It is "provides ⊇ want" rather than an exact match, so a combination
// nobody enumerated is still served by an image that happens to cover
// it — and so the table stays a list of images rather than a list of
// subsets of them.
func buildFor(version string, want Extensions) (postgresBuild, bool) {
	// Only the ones that are not already in the plain image. A database
	// with nothing but contrib modules keeps running exactly what it ran
	// before, which is what makes adding one cost it no downtime.
	var need []Extension
	for _, x := range want {
		if extensionSpecs[x].image {
			need = append(need, x)
		}
	}
	if len(need) == 0 {
		return postgresBuild{}, false
	}
	for _, b := range postgresBuilds[version] {
		covers := true
		for _, x := range need {
			if !slices.Contains(b.provides, x) {
				covers = false
				break
			}
		}
		if covers {
			return b, true
		}
	}
	return postgresBuild{}, false
}

// preloadFor is what has to be in shared_preload_libraries for these
// extensions at this version: the image's own requirements, then any the
// extensions bring themselves.
//
// It is the second thing that decides whether adding an extension can be
// done to the container that is running. A library the postmaster maps
// at startup cannot be added to a server that has already started.
func preloadFor(version string, want Extensions) []string {
	var out []string
	add := func(lib string) {
		if lib != "" && !slices.Contains(out, lib) {
			out = append(out, lib)
		}
	}
	if b, ok := buildFor(version, want); ok {
		for _, lib := range b.preload {
			add(lib)
		}
	}
	for _, x := range extensionOrder {
		if want.Has(x) {
			add(extensionSpecs[x].preload)
		}
	}
	return out
}

// NormalizeExtensions turns what somebody asked for into the list this
// instance will store, or says why it cannot.
//
// Deterministic, because it is persisted and returned: duplicates go,
// what one extension requires is added, and the result is sorted. Two
// requests naming the same extensions in different orders create two
// datastores with the same list, which is what makes a template's
// `extensions:` comparable across releases.
//
// Empty in, empty out — and an empty list is the normal case, which goes
// on running the plain postgres image.
func NormalizeExtensions(e Engine, version string, names []string) (Extensions, error) {
	want := map[Extension]bool{}
	for _, raw := range names {
		name := Extension(strings.ToLower(strings.TrimSpace(raw)))
		if name == "" {
			continue
		}
		if !name.Valid() {
			return nil, fmt.Errorf("%w: %q — this release offers %s",
				ErrUnknownExtension, raw, extensionList(AllExtensions()))
		}
		want[name] = true
	}
	if len(want) == 0 {
		return nil, nil
	}
	if e != EnginePostgres {
		return nil, fmt.Errorf("%w: %s has none — extensions are a Postgres idea, and only Postgres images here carry any",
			ErrExtensionsUnsupported, e)
	}

	// What an extension requires is added rather than demanded. One pass
	// is enough for a table this shallow, and the test that walks every
	// spec is what would notice if it stopped being.
	for _, x := range sortedSet(want) {
		for _, dep := range x.Requires() {
			want[dep] = true
		}
	}

	offered := SupportedExtensions(e, version)
	out := make(Extensions, 0, len(want))
	for _, x := range sortedSet(want) {
		if !slices.Contains(offered, x) {
			return nil, fmt.Errorf("%w: %s is not offered on %s %s — %s",
				ErrExtensionVersion, x, e, version, offeredList(e, version))
		}
		out = append(out, x)
	}
	slices.Sort(out)

	if needsImage(out) {
		if _, ok := buildFor(version, out); !ok {
			return nil, fmt.Errorf("%w: %s together on %s %s is not a combination Cubeship has an image for",
				ErrExtensionCombination, extensionList(out), e, version)
		}
	}
	return out, nil
}

// needsImage reports whether any of these is not in the plain image.
func needsImage(want Extensions) bool {
	for _, x := range want {
		if extensionSpecs[x].image {
			return true
		}
	}
	return false
}

// sortedSet is the keys of a set, in order, so every loop over one is
// the same loop — an error message that names "the first unsupported
// extension" should not depend on map iteration. Named around the
// standard library's `maps`, which service.go imports.
func sortedSet(set map[Extension]bool) []Extension {
	out := make([]Extension, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	slices.Sort(out)
	return out
}

func extensionList(xs []Extension) string {
	names := make([]string, 0, len(xs))
	for _, x := range xs {
		names = append(names, string(x))
	}
	return strings.Join(names, ", ")
}

func offeredList(e Engine, version string) string {
	offered := SupportedExtensions(e, version)
	if len(offered) == 0 {
		return "this version offers none"
	}
	return "this version offers " + extensionList(offered)
}

// extensionInstall is one statement to run inside the container, and the
// extension it creates.
type extensionInstall struct {
	Extension Extension
	SQL       string
}

// InstallPlan is the statements that create this datastore's extensions,
// in the order they have to run.
//
// The order is extensionOrder's, not the stored list's, and that is what
// puts an extension after whatever it requires. Every statement comes
// from the table and every one is `IF NOT EXISTS`, which is what makes
// running this again — on a `start`, or after a port was published and
// the container replaced — a no-op that still proves they are there.
func (d *Datastore) InstallPlan() []extensionInstall {
	plan := make([]extensionInstall, 0, len(d.Extensions))
	for _, x := range extensionOrder {
		if d.Extensions.Has(x) {
			plan = append(plan, extensionInstall{Extension: x, SQL: x.install()})
		}
	}
	return plan
}
