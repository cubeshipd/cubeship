package template

import "encoding/json"

// Normalized is the canonical form of a valid file: names filled in
// from keys, defaults applied, and the internal hostnames spelled out.
// It is what the catalog stores and what an instance will read.
type Normalized struct {
	SchemaVersion int                  `json:"schema_version"`
	MinCubeship   *string              `json:"min_cubeship"`
	Project       string               `json:"project"`
	Environment   string               `json:"environment"`
	Inputs        []NormalizedInput    `json:"inputs"`
	Databases     []NormalizedDatabase `json:"databases"`
	Stores        []NormalizedStore    `json:"stores"`
	Apps          []NormalizedApp      `json:"apps"`
}

type NormalizedInput struct {
	Key      string   `json:"key"`
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Help     string   `json:"help,omitempty"`
	Required bool     `json:"required"`
	Default  any      `json:"default,omitempty"`
	Pattern  string   `json:"pattern,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Options  []string `json:"options,omitempty"`
	Generate *int     `json:"generate,omitempty"`
}

type NormalizedLimits struct {
	CPU         *float64 `json:"cpu"`
	MemoryBytes *int64   `json:"memory_bytes"`
}

type NormalizedDatabase struct {
	Key     string  `json:"key"`
	Name    string  `json:"name"`
	Engine  string  `json:"engine"`
	Version *string `json:"version"`
	// Extensions is the normalized list: sorted, deduplicated, and
	// carrying what one of them requires — so a template asking for
	// vectorchord alone stores both, and two releases that mean the
	// same thing compare equal.
	Extensions []string          `json:"extensions"`
	Username   *string           `json:"username"`
	Database   *string           `json:"database"`
	Expose     *int              `json:"expose"`
	Limits     *NormalizedLimits `json:"limits"`
	Port       *int              `json:"port"`
}

type NormalizedStore struct {
	Key     string            `json:"key"`
	Name    string            `json:"name"`
	Version *string           `json:"version"`
	Buckets []string          `json:"buckets"`
	Limits  *NormalizedLimits `json:"limits"`
}

// NormalizedSource is an image, or a repository an instance builds.
type NormalizedSource struct {
	Type       string // "image", "dockerfile" or "railpack"
	Image      string
	Tag        *string
	Repo       string
	Ref        *string
	Dockerfile *string
}

// MarshalJSON writes only the fields of the kind of source this is, so
// an image never carries a null repo.
func (s NormalizedSource) MarshalJSON() ([]byte, error) {
	if s.Type == "image" {
		return json.Marshal(struct {
			Type  string  `json:"type"`
			Image string  `json:"image"`
			Tag   *string `json:"tag"`
		}{s.Type, s.Image, s.Tag})
	}
	return json.Marshal(struct {
		Type       string  `json:"type"`
		Repo       string  `json:"repo"`
		Ref        *string `json:"ref"`
		Dockerfile *string `json:"dockerfile"`
	}{s.Type, s.Repo, s.Ref, s.Dockerfile})
}

type NormalizedDomain struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type NormalizedAttach struct {
	Kind   string  `json:"kind"`
	Key    string  `json:"key"`
	Bucket *string `json:"bucket"`
	Prefix string  `json:"prefix"`
}

type NormalizedAutoscale struct {
	Min int     `json:"min"`
	Max int     `json:"max"`
	CPU float64 `json:"cpu"`
}

type NormalizedApp struct {
	Key          string               `json:"key"`
	Name         string               `json:"name"`
	Source       NormalizedSource     `json:"source"`
	Port         *int                 `json:"port"`
	Health       *string              `json:"health"`
	Domains      []NormalizedDomain   `json:"domains"`
	Attach       []NormalizedAttach   `json:"attach"`
	Env          map[string]string    `json:"env"`
	Limits       *NormalizedLimits    `json:"limits"`
	Scale        *int                 `json:"scale"`
	Spread       bool                 `json:"spread"`
	Autoscale    *NormalizedAutoscale `json:"autoscale"`
	Volumes      []NormalizedVolume   `json:"volumes"`
	TCP          []NormalizedTCP      `json:"tcp"`
	InternalHost string               `json:"internal_host"`
}

// NormalizedTCP is a published port. Host is nil for the instance to pick
// one, the port as digits, or the ${input.<key>} that answers it.
type NormalizedTCP struct {
	Port int     `json:"port"`
	Host *string `json:"host"`
}

type NormalizedVolume struct {
	Path string `json:"path"`
}

// defaultVersion is the newest version an engine offers, which is what
// a database that names none runs.
func defaultVersion(e *engine) string {
	if len(e.versions) == 0 {
		return ""
	}
	return e.versions[0]
}

func orNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func limitsOf(l *Limits) *NormalizedLimits {
	if l == nil {
		return nil
	}
	n := &NormalizedLimits{CPU: l.CPU}
	if l.Memory != nil {
		if bytes, ok := parseSize(*l.Memory); ok {
			n.MemoryBytes = &bytes
		}
	}
	return n
}

func normalize(m Manifest) Normalized {
	n := Normalized{
		SchemaVersion: SchemaVersion,
		MinCubeship:   orNil(m.MinCubeship),
		Project:       m.Project,
		Environment:   m.Environment,
		Inputs:        []NormalizedInput{},
		Databases:     []NormalizedDatabase{},
		Stores:        []NormalizedStore{},
		Apps:          []NormalizedApp{},
	}
	for _, in := range m.Inputs {
		n.Inputs = append(n.Inputs, NormalizedInput{
			Key: in.Key, Type: in.Type, Label: in.Label, Help: in.Help, Required: in.Required,
			Default: in.Default, Pattern: in.Pattern, Min: in.Min, Max: in.Max, Options: in.Options, Generate: in.Generate,
		})
	}
	for _, db := range m.Databases {
		nd := NormalizedDatabase{
			Key: db.Key, Name: or(db.Name, db.Key), Engine: db.Engine,
			Version: orNil(db.Version), Extensions: []string{},
			Username: orNil(db.Username), Database: orNil(db.Database),
			Expose: db.Expose, Limits: limitsOf(db.Limits),
		}
		if e := findEngine(db.Engine); e != nil {
			nd.Port = &e.port
			// Normalized against the version this will actually run,
			// which is the engine's newest when the file names none —
			// the same version the daemon would default to.
			// Never nil: a database with none carries `[]`, so nothing
			// downstream has to tell "none" from "this release did not
			// say". A file that failed validation keeps `[]` too — the
			// manifest is only published when it validated.
			if normalized, problem := normalizeExtensions(db.Engine, or(db.Version, defaultVersion(e)), db.Extensions); problem.code == "" && normalized != nil {
				nd.Extensions = normalized
			}
		}
		n.Databases = append(n.Databases, nd)
	}
	for _, s := range m.Stores {
		n.Stores = append(n.Stores, NormalizedStore{
			Key: s.Key, Name: or(s.Name, s.Key), Version: orNil(s.Version), Buckets: s.Buckets, Limits: limitsOf(s.Limits),
		})
	}
	for _, a := range m.Apps {
		name := or(a.Name, a.Key)
		na := NormalizedApp{
			Key: a.Key, Name: name, Port: a.Port, Health: orNil(a.Health),
			Domains: []NormalizedDomain{}, Attach: []NormalizedAttach{}, Env: a.Env,
			Limits: limitsOf(a.Limits), Scale: a.Scale, Spread: a.Spread != nil && *a.Spread,
			InternalHost: InternalHost(m.Project, m.Environment, name),
		}
		if a.Image != "" {
			na.Source = NormalizedSource{Type: "image", Image: a.Image, Tag: orNil(a.Tag)}
		} else {
			na.Source = NormalizedSource{Type: or(a.Build, "dockerfile"), Repo: a.Repo, Ref: orNil(a.Ref), Dockerfile: orNil(a.Dockerfile)}
		}
		for _, d := range a.Domains {
			port := defaultPort
			if d.Port != nil {
				port = *d.Port
			} else if a.Port != nil {
				port = *a.Port
			}
			na.Domains = append(na.Domains, NormalizedDomain{Host: d.Host, Port: port})
		}
		for _, at := range a.Attach {
			kind, key := "store", at.Store
			if at.Database != "" {
				kind, key = "database", at.Database
			}
			na.Attach = append(na.Attach, NormalizedAttach{Kind: kind, Key: key, Bucket: orNil(at.Bucket), Prefix: at.Prefix})
		}
		if as := a.Autoscale; as != nil {
			lo := 1
			if as.Min != nil {
				lo = *as.Min
			}
			na.Autoscale = &NormalizedAutoscale{Min: lo, Max: as.Max, CPU: as.CPU}
		}
		na.Volumes = []NormalizedVolume{}
		for _, v := range a.Volumes {
			clean, _ := volumePath(v.Path)
			na.Volumes = append(na.Volumes, NormalizedVolume{Path: clean})
		}
		na.TCP = []NormalizedTCP{}
		for _, tp := range a.TCP {
			na.TCP = append(na.TCP, NormalizedTCP{Port: tp.Port, Host: orNil(tp.Host)})
		}
		n.Apps = append(n.Apps, na)
	}
	return n
}
