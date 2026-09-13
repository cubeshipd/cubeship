// Package template reads a Cubeship template file: one YAML document
// describing apps, the managed data they need, and the questions an
// installer has to answer.
//
// It is outside internal/ on purpose. The catalog that indexes
// templates from GitHub (hosted/discovery) and the daemon that will one
// day install them must agree on what a valid file is, and the only
// way two programs never disagree about that is running the same code.
package template

import _ "embed"

// SchemaVersion is the `version:` this package reads, and the
// `schema_version` it writes into a normalized manifest.
const SchemaVersion = 1

// JSONSchema is the published schema for editors and agents, served at
// https://cubeship.dev/schema/template/v1.json. TestJSONSchemaMatchesTheDecoder
// is what keeps it describing the keys decode.go actually accepts.
//
//go:embed schema.json
var JSONSchema []byte

// Severity says whether a diagnostic refuses the file.
type Severity string

const (
	// Error refuses the file.
	Error Severity = "error"
	// Warning is advice the author may keep; the file is still valid.
	Warning Severity = "warning"
	// Info is a remark and nothing more.
	Info Severity = "info"
)

// Diagnostic is one thing wrong with, or worth saying about, a file.
// Code is stable: the docs have a section per code.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	// Path is where in the document, as keys and indexes:
	// ["apps", 0, "env", "DB_HOST"].
	Path   []any  `json:"path"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// Result is what Validate found. Manifest is set only when OK is.
type Result struct {
	OK          bool
	Diagnostics []Diagnostic
	Manifest    *Normalized
}

// Blocks reports whether any diagnostic is an error.
func Blocks(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == Error {
			return true
		}
	}
	return false
}

// Validate parses, checks and normalizes a template file.
//
// Each layer runs only when the one before it left something to work
// with: semantics over a half-decoded file would report nonsense.
func Validate(source []byte) Result {
	doc, diags := parse(source)
	if doc == nil {
		return Result{Diagnostics: diags}
	}

	m, diags := decode(doc)
	if Blocks(diags) {
		return Result{Diagnostics: diags}
	}

	found := append(checkSemantics(m, doc), advise(m, doc)...)
	if Blocks(found) {
		return Result{Diagnostics: found}
	}
	n := normalize(m)
	return Result{OK: true, Diagnostics: found, Manifest: &n}
}
