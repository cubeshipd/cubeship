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
