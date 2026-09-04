// Package envvar holds the environment variables Cubeship injects into an
// app's container, and the layering rules that decide which value wins.
package envvar

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Map is one level's worth of variables. It is stored as JSONB.
type Map map[string]string

// MarshalJSONB encodes m for a JSONB column. A nil Map becomes "{}"
// rather than JSON null, which the NOT NULL columns would reject and
// which every reader would then have to special-case.
func MarshalJSONB(m Map) (string, error) {
	if m == nil {
		m = Map{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("encode env: %w", err)
	}
	return string(b), nil
}

// UnmarshalJSONB decodes a JSONB column into a Map.
func UnmarshalJSONB(b []byte, into *Map) error {
	return json.Unmarshal(b, into)
}

// Merge layers each map over the last, in order: a key set by a later map
// overrides the same key set by an earlier one. This is how an app
// inherits its environment's variables and its environment inherits its
// project's, while still being able to override either.
func Merge(layers ...Map) Map {
	merged := make(Map)
	for _, layer := range layers {
		for k, v := range layer {
			merged[k] = v
		}
	}
	return merged
}

// Slice renders m as the "KEY=value" strings Docker expects, sorted so a
// container's configuration is deterministic.
func Slice(m Map) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

// Source names the level a variable was set at.
const (
	SourceProject     = "project"
	SourceEnvironment = "environment"
	SourceApp         = "app"
)

// Layer is one level's variables together with where they came from.
type Layer struct {
	Source string
	Vars   Map
}

// Resolved is one variable in an app's final environment, and the level
// that won it. It answers the question Merge alone cannot: not just what
// the container will see, but why.
type Resolved struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// Resolve applies the same precedence as Merge — each layer overriding
// the last — and reports which one supplied each surviving value, sorted
// by key.
func Resolve(layers ...Layer) []Resolved {
	winner := make(map[string]Resolved)
	for _, layer := range layers {
		for k, v := range layer.Vars {
			winner[k] = Resolved{Key: k, Value: v, Source: layer.Source}
		}
	}

	out := make([]Resolved, 0, len(winner))
	for _, r := range winner {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Resolved) int { return strings.Compare(a.Key, b.Key) })
	return out
}
