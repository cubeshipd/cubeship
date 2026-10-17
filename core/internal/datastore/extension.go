package datastore

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"slices"
)

// Extension is an optional piece of Postgres that a managed database can
// be created with: pgvector for vector columns, VectorChord for the
// index over them.
//
// It is a **name Cubeship chose**, not a package, an image or a piece of
// SQL. Nothing a caller sends is ever concatenated into a Dockerfile, an
// apt line or a statement — a name that is not in `extensionSpecs` is
// refused, and what is run for one that is comes from the table below.
// That is the whole of the security model here, and it is why this is an
// allowlist rather than a string passed through.
//
// The public name and the SQL name differ on purpose and are both
// spelled out: you ask for `pgvector` and the database gets `CREATE
// EXTENSION vector`, because that is what the extension is called in
// Postgres and what an application's migrations will say.
type Extension string

const (
	// The two that need an image of their own — see postgresBuilds.
	ExtensionPgvector    Extension = "pgvector"
	ExtensionVectorChord Extension = "vectorchord"

	// The rest are contrib modules. They ship inside the
	// `postgresql-<major>` package that every image here is built on, so
	// installing one is a statement and nothing else — no new image, no
	// new container, no interruption.
	ExtensionPgTrgm        Extension = "pg_trgm"
	ExtensionBtreeGin      Extension = "btree_gin"
	ExtensionBtreeGist     Extension = "btree_gist"
	ExtensionBloom         Extension = "bloom"
	ExtensionCitext        Extension = "citext"
	ExtensionCube          Extension = "cube"
	ExtensionEarthdistance Extension = "earthdistance"
	ExtensionFuzzystrmatch Extension = "fuzzystrmatch"
	ExtensionHstore        Extension = "hstore"
	ExtensionIntarray      Extension = "intarray"
	ExtensionISN           Extension = "isn"
	ExtensionLtree         Extension = "ltree"
	ExtensionPgcrypto      Extension = "pgcrypto"
	ExtensionSeg           Extension = "seg"
	ExtensionTablefunc     Extension = "tablefunc"
	ExtensionTSMSystemRows Extension = "tsm_system_rows"
	ExtensionUnaccent      Extension = "unaccent"
	ExtensionUUIDOSSP      Extension = "uuid-ossp"
	ExtensionDblink        Extension = "dblink"
	ExtensionPostgresFDW   Extension = "postgres_fdw"
	ExtensionPgPrewarm     Extension = "pg_prewarm"
	ExtensionPgstattuple   Extension = "pgstattuple"
	ExtensionPgBuffercache Extension = "pg_buffercache"

	// And one contrib module that is not only a statement: its library
	// is loaded by the postmaster, so turning it on replaces the
	// container.
	ExtensionPgStatStatements Extension = "pg_stat_statements"
)

// extensionSpec is everything Cubeship knows about running one.
type extensionSpec struct {
	// sqlName is what Postgres calls it — the argument to CREATE
	// EXTENSION, which is not always the name anybody types at us.
	sqlName string
	// requires are the extensions this one cannot work without. They are
	// added to a request rather than demanded of it: asking for
	// VectorChord and being told to also ask for pgvector is a rule with
	// exactly one right answer, so Cubeship gives it.
	requires []Extension
	// install overrides the statement built from sqlName, for the one
	// extension whose own documentation says something else.
	install string
	// summary is the one line the API, the CLI and the dashboard show
	// beside the name, so nobody has to leave the screen to find out
	// what they are turning on.
	summary string
	// preload is a library the postmaster has to map before the first
	// backend starts. An extension with one cannot be added to a running
	// container — see Service.AddExtensions.
	preload string
	// image says this one is not in the plain Postgres image and needs a
	// build from postgresBuilds. The rest are contrib modules, which
	// ship inside the `postgresql-<major>` package every image here is
	// built on, so creating one is a statement and nothing else.
	image bool
}

// extensionOrder is every extension this release knows, in the order
// they are offered **and installed**.
//
// Dependencies come first, which is what makes the install order the
// list order — TestEveryExtensionComesAfterWhatItNeeds holds it to that,
// so adding one is a decision about where it goes rather than a second
// table to keep in step.
var extensionOrder = []Extension{
	ExtensionPgvector,
	ExtensionVectorChord,
	ExtensionPgTrgm,
	ExtensionBtreeGin,
	ExtensionBtreeGist,
	ExtensionBloom,
	ExtensionCitext,
	ExtensionCube,
	ExtensionEarthdistance,
	ExtensionFuzzystrmatch,
	ExtensionHstore,
	ExtensionIntarray,
	ExtensionISN,
	ExtensionLtree,
	ExtensionPgcrypto,
	ExtensionSeg,
	ExtensionTablefunc,
	ExtensionTSMSystemRows,
	ExtensionUnaccent,
	ExtensionUUIDOSSP,
	ExtensionDblink,
	ExtensionPostgresFDW,
	ExtensionPgPrewarm,
	ExtensionPgstattuple,
	ExtensionPgBuffercache,
	ExtensionPgStatStatements,
}

var extensionSpecs = map[Extension]extensionSpec{
	ExtensionPgvector: {
		sqlName: "vector", image: true,
		summary: `Vector columns, distance operators and HNSW indexes, for embeddings. Created as the SQL extension "vector".`,
	},
	ExtensionVectorChord: {
		sqlName: "vchord", image: true,
		requires: []Extension{ExtensionPgvector},
		// CASCADE as well as the explicit order above: the statement is
		// the one VectorChord documents, and it keeps working against a
		// database somebody has already half set up.
		install: `CREATE EXTENSION IF NOT EXISTS vchord CASCADE`,
		preload: "vchord",
		summary: `A disk-friendly vector index over pgvector's types, which it needs and Cubeship adds for you. Created as the SQL extension "vchord".`,
	},
	ExtensionPgTrgm: {
		sqlName: "pg_trgm",
		summary: "Trigram similarity and indexes for it: fuzzy text search, and fast LIKE '%…%'.",
	},
	ExtensionBtreeGin: {
		sqlName: "btree_gin",
		summary: "GIN index support for ordinary types, so one index can cover a jsonb column and an integer beside it.",
	},
	ExtensionBtreeGist: {
		sqlName: "btree_gist",
		summary: "GiST index support for ordinary types — what an exclusion constraint on a range plus an id needs.",
	},
	ExtensionBloom: {
		sqlName: "bloom",
		summary: "A small index that answers equality on any combination of its columns, instead of one index per combination.",
	},
	ExtensionCitext: {
		sqlName: "citext",
		summary: "A text type that compares case-insensitively, for email addresses and usernames.",
	},
	ExtensionCube: {
		sqlName: "cube",
		summary: "A multidimensional cube type and distance operators over it.",
	},
	ExtensionEarthdistance: {
		sqlName:  "earthdistance",
		requires: []Extension{ExtensionCube},
		summary:  "Great-circle distance between points on the earth, built on cube — which Cubeship adds for you.",
	},
	ExtensionFuzzystrmatch: {
		sqlName: "fuzzystrmatch",
		summary: "Levenshtein, soundex and metaphone: how alike two strings sound or are spelled.",
	},
	ExtensionHstore: {
		sqlName: "hstore",
		summary: "A key-value type in a single column, with indexes over it.",
	},
	ExtensionIntarray: {
		sqlName: "intarray",
		summary: "Operators and indexes for arrays of integers — containment and overlap, quickly.",
	},
	ExtensionISN: {
		sqlName: "isn",
		summary: "Types that validate ISBN, ISSN, EAN and UPC rather than storing them as text.",
	},
	ExtensionLtree: {
		sqlName: "ltree",
		summary: "A type for hierarchical labels — categories, threads, org charts — with indexed ancestor queries.",
	},
	ExtensionPgcrypto: {
		sqlName: "pgcrypto",
		summary: "Hashing, HMAC, symmetric encryption and a proper random generator, inside the database.",
	},
	ExtensionSeg: {
		sqlName: "seg",
		summary: "A type for a line segment or a floating-point interval, with indexes over it.",
	},
	ExtensionTablefunc: {
		sqlName: "tablefunc",
		summary: "crosstab and friends: pivoting rows into columns in SQL.",
	},
	ExtensionTSMSystemRows: {
		sqlName: "tsm_system_rows",
		summary: "TABLESAMPLE that takes a number of rows rather than a percentage.",
	},
	ExtensionUnaccent: {
		sqlName: "unaccent",
		summary: "Strips accents, so a search for \"jose\" finds \"José\". Usually paired with pg_trgm.",
	},
	ExtensionUUIDOSSP: {
		sqlName: "uuid-ossp",
		summary: `Generators for UUID versions 1, 3, 4 and 5. Created as the SQL extension "uuid-ossp".`,
	},
	ExtensionDblink: {
		sqlName: "dblink",
		summary: "Query another PostgreSQL server from inside this one, a connection at a time.",
	},
	ExtensionPostgresFDW: {
		sqlName: "postgres_fdw",
		summary: "Map tables on another PostgreSQL server into this one and query them as if they were here.",
	},
	ExtensionPgPrewarm: {
		sqlName: "pg_prewarm",
		summary: "Load a table back into the cache after a restart, instead of waiting for traffic to do it.",
	},
	ExtensionPgstattuple: {
		sqlName: "pgstattuple",
		summary: "How much of a table or index is dead space — what says whether a VACUUM FULL is worth it.",
	},
	ExtensionPgBuffercache: {
		sqlName: "pg_buffercache",
		summary: "What is in the shared buffer cache right now, row by row.",
	},
	ExtensionPgStatStatements: {
		sqlName: "pg_stat_statements",
		// The one contrib module here that is not just a statement: its
		// library has to be loaded by the postmaster, so turning it on
		// replaces the container.
		preload: "pg_stat_statements",
		summary: "Execution counts and total time per query shape — the first place to look when something is slow.",
	},
}

// AllExtensions is every extension this release knows, in the order a
// form should offer them and the order they are installed in.
func AllExtensions() []Extension { return slices.Clone(extensionOrder) }

// SQLName is what Postgres calls this extension.
func (x Extension) SQLName() string { return extensionSpecs[x].sqlName }

// Summary is the sentence shown beside the name.
func (x Extension) Summary() string { return extensionSpecs[x].summary }

// Requires are the extensions this one is added alongside.
func (x Extension) Requires() []Extension { return extensionSpecs[x].requires }

// Builtin reports whether this extension is in every Postgres image
// Cubeship runs.
//
// It is what decides whether installing it is a statement or a new
// container: a contrib module is already on disk beside the server, so
// creating one interrupts nothing. Served to clients for the same
// reason — a screen should only warn about downtime that is going to
// happen.
func (x Extension) Builtin() bool {
	sp := extensionSpecs[x]
	return !sp.image && sp.preload == ""
}

// Valid reports whether x is an extension this release knows.
func (x Extension) Valid() bool {
	_, ok := extensionSpecs[x]
	return ok
}

// install is the statement that creates x: fixed, idempotent, and built
// from this table rather than from anything a caller sent.
//
// The name is double-quoted because one of them has a dash in it —
// `uuid-ossp` is not a bare identifier — and quoting every one is a rule
// with no exception to forget. It is safe to write here because sqlName
// is a constant in the table above; nothing a request carries reaches
// this string.
func (x Extension) install() string {
	sp := extensionSpecs[x]
	if sp.install != "" {
		return sp.install
	}
	return `CREATE EXTENSION IF NOT EXISTS "` + sp.sqlName + `"`
}

// Extensions is the list stored on a datastore, and the JSONB column
// behind it.
//
// A type of its own so both scan sites read the column the same way:
// `datastores` is read by a plain select and by a join with a Scan of
// its own, and a list decoded two ways is a list decoded differently
// eventually.
type Extensions []Extension

// Scan reads the JSONB column. A NULL — which only a row written before
// the column existed could be — is an empty list rather than an error.
func (e *Extensions) Scan(src any) error {
	*e = nil
	switch v := src.(type) {
	case nil:
		return nil
	case []byte:
		return json.Unmarshal(v, (*[]Extension)(e))
	case string:
		return json.Unmarshal([]byte(v), (*[]Extension)(e))
	default:
		return fmt.Errorf("extensions: cannot read %T from the database", src)
	}
}

// Value writes it. A nil list becomes `[]` rather than JSON null, for
// the reason envvar.MarshalJSONB does the same: a NOT NULL column would
// reject it and every reader would then special-case it.
func (e Extensions) Value() (driver.Value, error) {
	if e == nil {
		e = Extensions{}
	}
	b, err := json.Marshal([]Extension(e))
	if err != nil {
		return nil, fmt.Errorf("encode extensions: %w", err)
	}
	return string(b), nil
}

// Strings is the list as an API and a CLI render it — never nil, so a
// response carries `[]` rather than `null`.
func (e Extensions) Strings() []string {
	out := make([]string, 0, len(e))
	for _, x := range e {
		out = append(out, string(x))
	}
	return out
}

// Has reports whether x is in the list.
func (e Extensions) Has(x Extension) bool { return slices.Contains(e, x) }

// postgresBuild is one image this instance may run a Postgres on, and
// what comes with it.
//
// **The image is here and nowhere else.** A datastore's extensions pick
// a row of this table; nothing derives a repository, a tag or a digest
// from what a caller sent. That is what stops "extensions" from being a
// way to make this instance run an arbitrary image.
type postgresBuild struct {
	// provides is everything this image carries. A request is served by
	// the first build that provides all of it — see buildFor.
	provides []Extension
	// image and tag are the reference, and digest pins it. The tag stays
	// for the sake of anybody reading `docker ps`; the digest is what
	// actually decides which bytes run, so the same Cubeship release
	// always runs the same database image.
	image, tag, digest string
	// preload is what has to be in shared_preload_libraries for this
	// image. VectorChord will not load otherwise — it is a library the
	// postmaster has to map before the first backend starts, which is
	// not something CREATE EXTENSION can do.
	//
	// It is said here rather than left to the image's own CMD, which
	// happens to set the same thing today: what a container runs with is
	// this instance's decision, and a decision that lives in somebody
	// else's default is one that changes without us.
	preload []string
}
