package template

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func umami(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/umami.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func codes(ds []Diagnostic) []string {
	out := []string{}
	for _, d := range ds {
		out = append(out, d.Code)
	}
	return out
}

func errorsOf(ds []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for _, d := range ds {
		if d.Severity == Error {
			out = append(out, d)
		}
	}
	return out
}

func TestValidateAcceptsTheFixtureAndNormalizesIt(t *testing.T) {
	r := Validate([]byte(umami(t)))
	if errs := errorsOf(r.Diagnostics); len(errs) > 0 || !r.OK {
		t.Fatalf("refused: %+v", errs)
	}
	m := r.Manifest
	if m.SchemaVersion != 1 || m.Environment != "production" || m.Databases[0].Name != "umami-db" {
		t.Errorf("manifest = %+v", m)
	}
	app := m.Apps[0]
	if app.InternalHost != "cubeship-umami-production-web" {
		t.Errorf("internal host = %q", app.InternalHost)
	}
	if *app.Limits.CPU != 1 || *app.Limits.MemoryBytes != 1<<30 {
		t.Errorf("limits = %+v", app.Limits)
	}
	if app.Domains[0] != (NormalizedDomain{Host: "${input.domain}", Port: 3000}) {
		t.Errorf("domain = %+v", app.Domains[0])
	}

	b, err := json.Marshal(app.Source)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"type":"image","image":"ghcr.io/umami-software/umami","tag":"postgresql-v2"}` {
		t.Errorf("source = %s", b)
	}
}

// Two releases of one file must be one document, whatever order its
// author wrote the variables in.
func TestNormalizedEnvironmentIsSorted(t *testing.T) {
	r := Validate([]byte(umami(t)))
	b, _ := json.Marshal(r.Manifest.Apps[0].Env)
	if string(b) != `{"APP_SECRET":"${input.appSecret}","DATABASE_TYPE":"postgresql"}` {
		t.Errorf("env = %s", b)
	}
}

func TestASyntaxErrorStopsEverythingElse(t *testing.T) {
	r := Validate([]byte("project: \"x\n"))
	if r.OK || r.Manifest != nil {
		t.Fatal("accepted a broken file")
	}
	for _, d := range r.Diagnostics {
		if d.Code != "yaml.syntax" {
			t.Errorf("code = %s", d.Code)
		}
	}
}

func TestASyntaxErrorNamesItsLine(t *testing.T) {
	r := Validate([]byte("project: umami\n  bad: indent\n"))
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "yaml.syntax" || r.Diagnostics[0].Line != 2 {
		t.Fatalf("diagnostics = %+v", r.Diagnostics)
	}
	if strings.Contains(r.Diagnostics[0].Message, "line") || strings.HasPrefix(r.Diagnostics[0].Message, "yaml:") {
		t.Errorf("message repeats the position: %q", r.Diagnostics[0].Message)
	}
}

func TestAKeyGivenTwiceIsRefused(t *testing.T) {
	r := Validate([]byte("a: 1\na: 2\nb: 1\nb: 2\n"))
	if got := codes(r.Diagnostics); !slices.Equal(got, []string{"yaml.syntax", "yaml.syntax"}) {
		t.Fatalf("codes = %v", got)
	}
	if r.Diagnostics[0].Line == r.Diagnostics[1].Line {
		t.Error("both point at one line")
	}
}

func TestAnAliasIsRefused(t *testing.T) {
	r := Validate([]byte("version: 1\nproject: &p demo\nenvironment: *p\napps: []\n"))
	if !slices.Contains(codes(r.Diagnostics), "yaml.alias") {
		t.Fatalf("codes = %v", codes(r.Diagnostics))
	}
}

func TestAnEmptyFileIsOneDiagnostic(t *testing.T) {
	r := Validate([]byte("   \n"))
	if got := codes(r.Diagnostics); !slices.Equal(got, []string{"yaml.empty"}) {
		t.Fatalf("codes = %v", got)
	}
}

func TestWarningsDoNotRefuse(t *testing.T) {
	r := Validate([]byte("version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n"))
	if !r.OK {
		t.Fatalf("refused: %+v", r.Diagnostics)
	}
	if !slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool { return d.Severity == Warning }) {
		t.Error("no warning")
	}
}

func TestABrokenRuleIsNotNormalized(t *testing.T) {
	broken := strings.Replace(umami(t), "${input.domain}", "analytics.example.com", 1)
	if r := Validate([]byte(broken)); r.Manifest != nil || r.OK {
		t.Fatal("normalized a file with an error")
	}
}

const minimal = `version: 1
project: umami
apps:
  - key: web
    image: ghcr.io/umami-software/umami
`

func TestTheShape(t *testing.T) {
	cases := []struct {
		name, source, code string
		path               string
		line               int
		hint               string
	}{
		{"an unknown key suggests the real one", minimal + "    healthcheck: /up\n", "schema.unknown-key", "apps.0.healthcheck", 6, `"health"`},
		{"a version it does not speak", strings.Replace(minimal, "version: 1", "version: 2", 1), "schema.invalid_value", "version", 1, ""},
		{"no apps at all", "version: 1\nproject: umami\napps: []\n", "schema.too_small", "apps", 3, ""},
		{"a missing project", "version: 1\napps:\n  - key: web\n    image: nginx\n", "schema.invalid_type", "project", 0, ""},
		{"a version written as a number", minimal + "databases:\n  - key: db\n    engine: postgres\n    version: 18\n", "schema.invalid_type", "databases.0.version", 9, `quote it: "18"`},
		{"a tag the registry would refuse", minimal + "    tag: \"no spaces\"\n", "schema.invalid_format", "apps.0.tag", 6, ""},
		{"an input type that does not exist", minimal + "inputs:\n  - key: x\n    type: colour\n    label: X\n", "schema.invalid_union", "inputs.0.type", 8, ""},
		{"a key an input type does not take", minimal + "inputs:\n  - key: x\n    type: domain\n    label: X\n    options: [a, b]\n", "schema.unknown-key", "inputs.0.options", 10, ""},
		{"a port out of range", minimal + "    port: 70000\n", "schema.too_big", "apps.0.port", 6, ""},
		{"a build nobody runs", minimal + "    build: nix\n", "schema.invalid_value", "apps.0.build", 6, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Validate([]byte(tc.source))
			i := slices.IndexFunc(r.Diagnostics, func(d Diagnostic) bool { return d.Code == tc.code })
			if i < 0 {
				t.Fatalf("no %s in %+v", tc.code, r.Diagnostics)
			}
			d := r.Diagnostics[i]
			if got := joinPath(d.Path); got != tc.path {
				t.Errorf("path = %s, want %s", got, tc.path)
			}
			if tc.line != 0 && d.Line != tc.line {
				t.Errorf("line = %d, want %d", d.Line, tc.line)
			}
			if !strings.Contains(d.Hint, tc.hint) {
				t.Errorf("hint = %q, want %q in it", d.Hint, tc.hint)
			}
		})
	}
}

func TestEnvironmentIsNotSuggestedAsEnv(t *testing.T) {
	r := Validate([]byte(minimal + "    environment: staging\n"))
	if len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "schema.unknown-key" {
		t.Fatalf("diagnostics = %+v", r.Diagnostics)
	}
	if strings.Contains(r.Diagnostics[0].Hint, "env") {
		t.Errorf("hint = %q", r.Diagnostics[0].Hint)
	}
}

func TestAVariableWrittenAsANumberIsAString(t *testing.T) {
	r := Validate([]byte(minimal + "    env:\n      PORT: 3000\n"))
	if !r.OK || r.Manifest.Apps[0].Env["PORT"] != "3000" {
		t.Fatalf("result = %+v", r)
	}
}

const right = `version: 1
project: demo
inputs:
  - key: domain
    type: domain
    label: Where it answers
databases:
  - key: db
    engine: postgres
    version: "18"
    database: analytics
apps:
  - key: web
    image: ghcr.io/umami-software/umami
    tag: postgresql-v2
    port: 3000
    health: /api/heartbeat
    domains:
      - host: ${input.domain}
    attach:
      - database: db
    limits:
      cpu: 1
      memory: 1Gi
`

func semanticCodes(t *testing.T, source string) []string {
	t.Helper()
	doc, diags := parse([]byte(source))
	if doc == nil {
		t.Fatalf("does not parse: %+v", diags)
	}
	m, diags := decode(doc)
	if Blocks(diags) {
		t.Fatalf("does not decode: %+v", diags)
	}
	return codes(checkSemantics(m, doc))
}

func TestTheSemantics(t *testing.T) {
	mysql := strings.NewReplacer("engine: postgres", "engine: mysql", `version: "18"`, `version: "8.4"`).Replace(right)
	cases := []struct {
		name, source, code string
	}{
		{"a literal host", strings.Replace(right, "${input.domain}", "analytics.example.com", 1), "domain.literal"},
		{"an undeclared reference", strings.Replace(right, "${input.domain}", "${input.nope}", 1), "reference.unknown"},
		{"an attribute a kind lacks", right + "    env:\n      URL: ${db.db.endpoint}\n", "reference.attribute"},
		{"a tag inside the image", strings.Replace(right, "umami\n", "umami:latest\n", 1), "image.tagged"},
		{"two databases at one prefix", strings.NewReplacer(
			"    database: analytics\n", "    database: analytics\n  - key: other\n    engine: postgres\n",
			"      - database: db\n", "      - database: db\n      - database: other\n").Replace(right), "attach.collision"},
		{"an engine version the daemon lacks", strings.Replace(right, `version: "18"`, `version: "9"`, 1), "engine.version"},
		{"a user the engine refuses", strings.Replace(mysql, "    database: analytics", "    database: analytics\n    username: root", 1), "engine.username"},
		{"a memory that is not a size", strings.Replace(right, "1Gi", "loads", 1), "limits.memory"},
		{"a memory below the floor", strings.Replace(right, "1Gi", "1Ki", 1), "limits.memory"},
		{"a duplicate key", strings.Replace(right, "  - key: web", "  - key: web\n    image: nginx\n  - key: web", 1), "key.duplicate"},
		{"a reserved name", strings.Replace(right, "  - key: db\n", "  - key: db\n    name: engines\n", 1), "name.reserved"},
		{"an app with no source", strings.Replace(right, "    image: ghcr.io/umami-software/umami\n", "", 1), "source.missing"},
		{"an app with two sources", strings.Replace(right, "    port: 3000", "    repo: https://github.com/x/y\n    build: railpack\n    port: 3000", 1), "source.conflict"},
		{"a health path the daemon refuses", strings.Replace(right, "/api/heartbeat", "/up?deep=1", 1), "health.path"},
		{"a version that is not a range", strings.Replace(right, "project: demo", "project: demo\nminCubeship: soon", 1), "version.range"},
	}
	if got := semanticCodes(t, right); len(got) != 0 {
		t.Fatalf("the right file has %v", got)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := semanticCodes(t, tc.source); !slices.Contains(got, tc.code) {
				t.Errorf("codes = %v, want %s", got, tc.code)
			}
		})
	}
}

func TestAdviceForABareApp(t *testing.T) {
	r := Validate([]byte("version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\ndatabases:\n  - key: db\n    engine: postgres\n"))
	got := codes(r.Diagnostics)
	for _, want := range []string{"advice.no-health", "advice.floating-tag", "advice.no-limits", "advice.unreachable-app", "advice.dockerhub", "advice.unpinned-engine"} {
		if !slices.Contains(got, want) {
			t.Errorf("no %s in %v", want, got)
		}
	}
	if Blocks(r.Diagnostics) {
		t.Error("advice refused the file")
	}
}

func TestVersionRanges(t *testing.T) {
	for _, ok := range []string{"0.6.0", ">=0.6.0", ">= 0.6.0 <1.0.0", "^0.6 || ^1", "~1.2.3", "*", "1.x", "1.0.0 - 2.0.0", "0.6.0-rc.1"} {
		if !validRange(ok) {
			t.Errorf("refused %q", ok)
		}
	}
	for _, bad := range []string{"", "soon", ">=", "1.2.3.4", "latest"} {
		if validRange(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestReferences(t *testing.T) {
	got := findReferences("${db.main.host}:${db.main.port} and $ {input.x} and ${nope}")
	if len(got) != 2 || got[0].attr != "host" || got[1].attr != "port" || got[0].kind != "db" {
		t.Errorf("references = %+v", got)
	}
}

func joinPath(path []any) string {
	parts := make([]string, len(path))
	for i, p := range path {
		b, _ := json.Marshal(p)
		parts[i] = strings.Trim(string(b), `"`)
	}
	return strings.Join(parts, ".")
}
