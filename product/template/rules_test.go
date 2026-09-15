package template

import (
	"encoding/json"
	"slices"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/limits"
)

// The rules a template is held to are copied from the daemon, so that
// reading a template never needs the daemon. These are what notice a
// copy going stale.

func TestTheEnginesAreTheDaemons(t *testing.T) {
	var names []string
	for _, e := range datastore.Engines() {
		names = append(names, string(e))
		ours := findEngine(string(e))
		if ours == nil {
			t.Errorf("the daemon runs %s and templates do not know it", e)
			continue
		}
		if !slices.Equal(ours.versions, e.Versions()) || ours.port != e.Port() ||
			ours.hasDatabase != e.HasDatabase() || ours.stem != e.VarStem() {
			t.Errorf("%s: ours %+v, the daemon's versions %v port %d database %v stem %s",
				e, *ours, e.Versions(), e.Port(), e.HasDatabase(), e.VarStem())
		}
	}
	if len(names) != len(engines) {
		t.Errorf("templates know %d engines, the daemon runs %v", len(engines), names)
	}
}

func TestTheFloorsAreTheDaemons(t *testing.T) {
	if minCPU != limits.MinCPU || minMemory != limits.MinMemory {
		t.Errorf("floors %v/%d, the daemon's %v/%d", minCPU, minMemory, limits.MinCPU, limits.MinMemory)
	}
	if maxAutoscale != app.MaxAutoscale || maxHealthPath != app.MaxHealthPathLength {
		t.Error("autoscale or health path bound differs from the daemon's")
	}
}

func TestHealthPathsAgreeWithTheDaemon(t *testing.T) {
	for _, p := range []string{"/", "/api/heartbeat", "/up;x=1", "api", "/a b", "/a?b", "/a#b", "/\"quoted\"", "/ünï"} {
		if daemon, ours := app.ValidHealthPath(p), healthPathProblem(p) == ""; daemon != ours {
			t.Errorf("%q: the daemon says %v, templates say %v", p, daemon, ours)
		}
	}
}

func TestVolumePathsAgreeWithTheDaemon(t *testing.T) {
	for _, p := range []string{"/data", "/data/", "data", "/", "/proc", "/procs", "/sys/x", "/dev", "/a:b", "/a,b", " /x/../y "} {
		clean, err := app.CleanVolumePath(p)
		ours, problem := volumePath(p)
		if (err == nil) != (problem == "") || (err == nil && clean != ours) {
			t.Errorf("%q: the daemon says %q, %v; templates say %q, %q", p, clean, err, ours, problem)
		}
	}
}

func TestTCPHostPortsAgreeWithTheDaemon(t *testing.T) {
	if minTCPHostPort != app.MinTCPHostPort {
		t.Errorf("templates refuse host ports below %d, the daemon below %d", minTCPHostPort, app.MinTCPHostPort)
	}
}

// The published schema is a file, not generated, so it is checked
// against the keys the decoder actually accepts.
func TestJSONSchemaMatchesTheDecoder(t *testing.T) {
	var s schemaNode
	if err := json.Unmarshal(JSONSchema, &s); err != nil {
		t.Fatal(err)
	}
	apps := s.Properties["apps"].Items
	check := func(name string, node *schemaNode, want []string) {
		t.Helper()
		if node == nil {
			t.Errorf("%s: missing from the schema", name)
			return
		}
		got := make([]string, 0, len(node.Properties))
		for k := range node.Properties {
			got = append(got, k)
		}
		slices.Sort(got)
		want = slices.Sorted(slices.Values(want))
		if !slices.Equal(got, want) {
			t.Errorf("%s: schema has %v, the decoder %v", name, got, want)
		}
	}
	check("manifest", &s, keySets["manifest"])
	check("app", apps, keySets["app"])
	check("database", s.Properties["databases"].Items, keySets["database"])
	check("store", s.Properties["stores"].Items, keySets["store"])
	check("domain", apps.Properties["domains"].Items, keySets["domain"])
	check("attach", apps.Properties["attach"].Items, keySets["attach"])
	check("autoscale", apps.Properties["autoscale"], keySets["autoscale"])
	check("tcp", apps.Properties["tcp"].Items, keySets["tcp"])

	variants := s.Properties["inputs"].Items.OneOf
	if len(variants) != len(inputTypes) {
		t.Fatalf("schema has %d input types, the decoder %d", len(variants), len(inputTypes))
	}
	for _, v := range variants {
		typ, _ := v.Properties["type"].Const.(string)
		extra, ok := inputTypes[typ]
		if !ok {
			t.Errorf("schema input type %q is not decoded", typ)
			continue
		}
		check("input "+typ, v, append(slices.Clone(inputBase), extra...))
	}
}

type schemaNode struct {
	Properties map[string]*schemaNode `json:"properties"`
	Items      *schemaNode            `json:"items"`
	OneOf      []*schemaNode          `json:"oneOf"`
	Const      any                    `json:"const"`
}

// The extension rules are a copy, so this runs every engine, every
// version and every combination through both and requires the same
// answer — accepted or refused, and the same normalized list.
//
// A subset walk rather than a handful of cases: with a table this size
// the combinations that matter are the ones nobody thought to write
// down, and the pairs are cheap.
func TestExtensionsAgreeWithTheDaemon(t *testing.T) {
	daemon := datastore.AllExtensions()
	var names []string
	for _, x := range daemon {
		names = append(names, string(x))
	}

	// The same list, in the same order.
	if !slices.Equal(names, extensionNames()) {
		t.Fatalf("templates know %v, the daemon %v", extensionNames(), names)
	}
	for _, x := range daemon {
		ours := findExtension(string(x))
		var requires []string
		for _, dep := range x.Requires() {
			requires = append(requires, string(dep))
		}
		if !slices.Equal(ours.requires, requires) {
			t.Errorf("%s: ours needs %v, the daemon's %v", x, ours.requires, requires)
		}
		for _, e := range datastore.Engines() {
			for _, v := range e.Versions() {
				offered := slices.Contains(datastore.SupportedExtensions(e, v), x)
				mine := e == datastore.EnginePostgres && slices.Contains(ours.versions, v)
				if offered != mine {
					t.Errorf("%s on %s %s: the daemon says %v, templates say %v", x, e, v, offered, mine)
				}
			}
		}
	}

	// Every request either side could be given: each engine, each
	// version, and every pair of extensions plus a name neither knows.
	inputs := [][]string{nil, {}, {"nope"}, {"PgVector"}, {"vector"}}
	for i, a := range names {
		inputs = append(inputs, []string{a}, []string{a, a})
		for _, b := range names[i+1:] {
			inputs = append(inputs, []string{a, b}, []string{b, a})
		}
	}
	for _, e := range datastore.Engines() {
		for _, v := range e.Versions() {
			for _, in := range inputs {
				want, wantErr := datastore.NormalizeExtensions(e, v, in)
				got, problem := normalizeExtensions(string(e), v, in)
				if (wantErr != nil) != (problem.code != "") {
					t.Fatalf("%s %s %v: the daemon says %v, templates say %q",
						e, v, in, wantErr, problem.code)
				}
				if wantErr != nil {
					continue
				}
				if !slices.Equal(want.Strings(), got) && !(len(want) == 0 && len(got) == 0) {
					t.Errorf("%s %s %v: the daemon normalizes to %v, templates to %v",
						e, v, in, want.Strings(), got)
				}
			}
		}
	}
}

// The published schema names the extensions in an enum, so an editor
// completes them and refuses a typo before anybody publishes a release.
// It is a file rather than generated, so this is what keeps it in step.
func TestTheSchemaNamesEveryExtension(t *testing.T) {
	var s struct {
		Properties struct {
			Databases struct {
				Items struct {
					Properties struct {
						Extensions struct {
							Items struct {
								Enum []string `json:"enum"`
							} `json:"items"`
						} `json:"extensions"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"databases"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(JSONSchema, &s); err != nil {
		t.Fatal(err)
	}
	got := s.Properties.Databases.Items.Properties.Extensions.Items.Enum
	if !slices.Equal(got, extensionNames()) {
		t.Errorf("the schema offers %v, the decoder %v", got, extensionNames())
	}
}
