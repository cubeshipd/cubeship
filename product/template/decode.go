package template

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// keySets are the keys each kind of mapping accepts, which is also what
// an unknown key's suggestion is picked from.
var keySets = map[string][]string{
	"manifest": {"version", "minCubeship", "project", "environment", "inputs", "databases", "stores", "apps"},
	"input":    {"key", "type", "label", "help", "required", "default", "pattern", "min", "max", "options", "generate"},
	"database": {"key", "name", "engine", "version", "username", "database", "expose", "limits"},
	"store":    {"key", "name", "version", "buckets", "limits"},
	"app": {"key", "name", "image", "tag", "repo", "ref", "build", "dockerfile", "port", "health",
		"domains", "attach", "env", "limits", "scale", "spread", "autoscale", "volumes"},
	"volume":    {"path"},
	"domain":    {"host", "port"},
	"attach":    {"database", "store", "bucket", "prefix"},
	"limits":    {"cpu", "memory"},
	"autoscale": {"min", "max", "cpu"},
}

var inputBase = []string{"key", "type", "label", "help", "required"}

// inputTypes are the keys each input type adds to inputBase.
var inputTypes = map[string][]string{
	"domain": {},
	"text":   {"default", "pattern"},
	"number": {"default", "min", "max"},
	"choice": {"default", "options"},
	"secret": {"generate"},
	"store":  {},
}

type decoder struct {
	doc   *document
	diags []Diagnostic
}

func (d *decoder) fail(code, message string, path []any, hint string) {
	d.diags = append(d.diags, d.doc.diag(Error, "schema."+code, message, path, hint))
}

// decode is the shape of the file: every key known, every value the
// right type. What the shape cannot say is semantics.go.
func decode(doc *document) (Manifest, []Diagnostic) {
	d := &decoder{doc: doc}
	m := Manifest{Environment: "production"}
	f := d.object(doc.root, nil, keySets["manifest"], keySets["manifest"])
	if f == nil {
		return m, d.diags
	}

	if n, ok := d.field(f, nil, "version", true); ok {
		if n.Kind != yaml.ScalarNode || n.Tag != "!!int" || n.Value != "1" {
			d.fail("invalid_value", "version is 1, the only one this reads", []any{"version"}, "")
		} else {
			m.Version = 1
		}
	}
	m.MinCubeship, _ = d.str(f, nil, "minCubeship", false, nil)
	m.Project, _ = d.str(f, nil, "project", true, slugRule)
	if env, ok := d.str(f, nil, "environment", false, slugRule); ok {
		m.Environment = env
	}

	for i, n := range d.list(f, nil, "inputs") {
		if in, ok := d.input(n, []any{"inputs", i}); ok {
			m.Inputs = append(m.Inputs, in)
		}
	}
	for i, n := range d.list(f, nil, "databases") {
		m.Databases = append(m.Databases, d.database(n, []any{"databases", i}))
	}
	for i, n := range d.list(f, nil, "stores") {
		m.Stores = append(m.Stores, d.store(n, []any{"stores", i}))
	}
	if _, given := f["apps"]; !given {
		d.fail("invalid_type", "apps is required", []any{"apps"}, "")
	} else if apps := d.list(f, nil, "apps"); len(apps) == 0 && f["apps"].Kind == yaml.SequenceNode {
		d.fail("too_small", "a template creates at least one app", []any{"apps"}, "")
	} else {
		for i, n := range apps {
			m.Apps = append(m.Apps, d.app(n, []any{"apps", i}))
		}
	}
	return m, d.diags
}

func (d *decoder) input(n *yaml.Node, path []any) (Input, bool) {
	in := Input{Required: true}
	if n.Kind != yaml.MappingNode {
		d.fail("invalid_type", "expected a mapping", path, "")
		return in, false
	}
	var typ *yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "type" {
			typ = n.Content[i+1]
		}
	}
	extra, known := inputTypes[""], false
	if typ != nil && typ.Kind == yaml.ScalarNode && typ.Tag == "!!str" {
		extra, known = inputTypes[typ.Value]
	}
	if !known {
		d.fail("invalid_union", "type is one of domain, text, number, choice, secret or store", child(path, "type"), "")
		return in, false
	}
	in.Type = typ.Value

	f := d.object(n, path, append(slices.Clone(inputBase), extra...), keySets["input"])
	in.Key, _ = d.str(f, path, "key", true, keyRule)
	in.Label, _ = d.str(f, path, "label", true, nil)
	in.Help, _ = d.str(f, path, "help", false, nil)
	if v, ok := d.boolean(f, path, "required"); ok {
		in.Required = v
	}
	switch in.Type {
	case "text", "choice":
		if v, ok := d.str(f, path, "default", false, nil); ok {
			in.Default = v
		}
		in.Pattern, _ = d.str(f, path, "pattern", false, nil)
		if in.Type == "choice" {
			in.Options = d.strings(f, path, "options")
			if _, given := f["options"]; !given {
				d.fail("invalid_type", "options is required", child(path, "options"), "")
			} else if len(in.Options) < 2 {
				d.fail("too_small", "a choice offers at least two options", child(path, "options"), "")
			}
		}
	case "number":
		if v, ok := d.number(f, path, "default"); ok {
			in.Default = v
		}
		in.Min = d.numberPtr(f, path, "min")
		in.Max = d.numberPtr(f, path, "max")
	case "secret":
		in.Generate = d.integerPtr(f, path, "generate", nil, nil)
	}
	return in, true
}

func (d *decoder) database(n *yaml.Node, path []any) Database {
	var db Database
	f := d.object(n, path, keySets["database"], keySets["database"])
	if f == nil {
		return db
	}
	db.Key, _ = d.str(f, path, "key", true, keyRule)
	db.Name, _ = d.str(f, path, "name", false, slugRule)
	db.Engine, _ = d.str(f, path, "engine", true, nil)
	db.Version, _ = d.str(f, path, "version", false, nil)
	db.Username, _ = d.str(f, path, "username", false, nil)
	db.Database, _ = d.str(f, path, "database", false, nil)
	if e, given := f["expose"]; given && !(e.Kind == yaml.ScalarNode && e.Tag == "!!null") {
		db.Expose = d.integerPtr(f, path, "expose", nil, nil)
	}
	db.Limits = d.limits(f, path)
	return db
}

func (d *decoder) store(n *yaml.Node, path []any) Store {
	s := Store{Buckets: []string{}}
	f := d.object(n, path, keySets["store"], keySets["store"])
	if f == nil {
		return s
	}
	s.Key, _ = d.str(f, path, "key", true, keyRule)
	s.Name, _ = d.str(f, path, "name", false, slugRule)
	s.Version, _ = d.str(f, path, "version", false, nil)
	if b := d.strings(f, path, "buckets"); b != nil {
		s.Buckets = b
	}
	s.Limits = d.limits(f, path)
	return s
}

var ports = [2]float64{1, 65535}

func (d *decoder) app(n *yaml.Node, path []any) App {
	a := App{Env: map[string]string{}, Domains: []Domain{}, Attach: []Attach{}}
	f := d.object(n, path, keySets["app"], keySets["app"])
	if f == nil {
		return a
	}
	a.Key, _ = d.str(f, path, "key", true, keyRule)
	a.Name, _ = d.str(f, path, "name", false, slugRule)
	a.Image, _ = d.str(f, path, "image", false, nil)
	a.Tag, _ = d.str(f, path, "tag", false, tagRule)
	a.Repo, _ = d.str(f, path, "repo", false, nil)
	a.Ref, _ = d.str(f, path, "ref", false, nil)
	if build, ok := d.str(f, path, "build", false, nil); ok {
		if build != "dockerfile" && build != "railpack" {
			d.fail("invalid_value", "build is dockerfile or railpack", child(path, "build"), "")
		} else {
			a.Build = build
		}
	}
	a.Dockerfile, _ = d.str(f, path, "dockerfile", false, nil)
	a.Port = d.integerPtr(f, path, "port", &ports[0], &ports[1])
	a.Health, _ = d.str(f, path, "health", false, nil)

	for i, dn := range d.list(f, path, "domains") {
		p := child(child(path, "domains"), i)
		df := d.object(dn, p, keySets["domain"], keySets["domain"])
		if df == nil {
			continue
		}
		var dom Domain
		dom.Host, _ = d.str(df, p, "host", true, nil)
		dom.Port = d.integerPtr(df, p, "port", &ports[0], &ports[1])
		a.Domains = append(a.Domains, dom)
	}
	for i, an := range d.list(f, path, "attach") {
		p := child(child(path, "attach"), i)
		af := d.object(an, p, keySets["attach"], keySets["attach"])
		if af == nil {
			continue
		}
		var at Attach
		at.Database, _ = d.str(af, p, "database", false, keyRule)
		at.Store, _ = d.str(af, p, "store", false, keyRule)
		at.Bucket, _ = d.str(af, p, "bucket", false, nil)
		at.Prefix, _ = d.str(af, p, "prefix", false, nil)
		a.Attach = append(a.Attach, at)
	}

	if en, given := f["env"]; given {
		p := child(path, "env")
		if en.Kind != yaml.MappingNode {
			d.fail("invalid_type", "env is a mapping of names to values", p, "")
		} else {
			for i := 0; i+1 < len(en.Content); i += 2 {
				k, v := en.Content[i], en.Content[i+1]
				vp := child(p, k.Value)
				if !envKeyPattern.MatchString(k.Value) {
					d.fail("invalid_format", "a variable name is letters, digits and underscores, not starting with a digit", vp, "")
					continue
				}
				// A container's environment is strings, so a number or a
				// boolean written bare is one here too.
				if v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
					d.fail("invalid_type", "a variable's value is a string, a number or a boolean", vp, "")
					continue
				}
				a.Env[k.Value] = v.Value
			}
		}
	}

	a.Limits = d.limits(f, path)
	one := 1.0
	a.Scale = d.integerPtr(f, path, "scale", &one, nil)
	if v, ok := d.boolean(f, path, "spread"); ok {
		a.Spread = &v
	}
	if sn, given := f["autoscale"]; given {
		p := child(path, "autoscale")
		if sf := d.object(sn, p, keySets["autoscale"], keySets["autoscale"]); sf != nil {
			as := Autoscale{Min: d.integerPtr(sf, p, "min", nil, nil)}
			if v := d.integerPtr(sf, p, "max", nil, nil); v != nil {
				as.Max = *v
			} else if _, given := sf["max"]; !given {
				d.fail("invalid_type", "max is required", child(p, "max"), "")
			}
			if v, ok := d.number(sf, p, "cpu"); ok {
				as.CPU = v
			} else if _, given := sf["cpu"]; !given {
				d.fail("invalid_type", "cpu is required", child(p, "cpu"), "")
			}
			a.Autoscale = &as
		}
	}
	for i, vn := range d.list(f, path, "volumes") {
		p := child(child(path, "volumes"), i)
		vf := d.object(vn, p, keySets["volume"], keySets["volume"])
		if vf == nil {
			continue
		}
		var v Volume
		v.Path, _ = d.str(vf, p, "path", true, nil)
		a.Volumes = append(a.Volumes, v)
	}
	return a
}

func (d *decoder) limits(f map[string]*yaml.Node, path []any) *Limits {
	n, given := f["limits"]
	if !given {
		return nil
	}
	p := child(path, "limits")
	lf := d.object(n, p, keySets["limits"], keySets["limits"])
	if lf == nil {
		return nil
	}
	l := &Limits{CPU: d.numberPtr(lf, p, "cpu")}
	if mn, given := lf["memory"]; given {
		if mn.Kind == yaml.ScalarNode && (mn.Tag == "!!str" || mn.Tag == "!!int" || mn.Tag == "!!float") {
			v := mn.Value
			l.Memory = &v
		} else {
			d.fail("invalid_type", "memory is a size, like 512Mi, or a number of bytes", child(p, "memory"), "")
		}
	}
	return l
}

// object reads a mapping, reporting every key outside allowed with the
// nearest one in suggest. Nil when n is not a mapping at all.
func (d *decoder) object(n *yaml.Node, path []any, allowed, suggest []string) map[string]*yaml.Node {
	if n.Kind != yaml.MappingNode {
		d.fail("invalid_type", "expected a mapping", nonNil(path), "")
		return nil
	}
	fields := map[string]*yaml.Node{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if !slices.Contains(allowed, k) {
			hint := ""
			if s := nearest(k, suggest); s != "" {
				hint = fmt.Sprintf("did you mean %q?", s)
			}
			d.diags = append(d.diags, d.doc.diag(Error, "schema.unknown-key", fmt.Sprintf("unknown key %q", k), child(path, k), hint))
			continue
		}
		fields[k] = n.Content[i+1]
	}
	return fields
}

func (d *decoder) field(f map[string]*yaml.Node, path []any, key string, required bool) (*yaml.Node, bool) {
	n, given := f[key]
	if !given && required {
		d.fail("invalid_type", key+" is required", child(path, key), "")
	}
	return n, given
}

type rule struct {
	match   func(string) bool
	message string
}

var (
	slugRule = &rule{validSlugShape, "lowercase letters, digits and dashes"}
	keyRule  = &rule{keyPattern.MatchString, "a letter, then letters, digits, dash or underscore"}
	tagRule  = &rule{tagPattern.MatchString, "a tag is letters, digits, _ . and -, at most 128 characters"}
)

func (d *decoder) str(f map[string]*yaml.Node, path []any, key string, required bool, r *rule) (string, bool) {
	n, given := d.field(f, path, key, required)
	if !given {
		return "", false
	}
	p := child(path, key)
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		hint := ""
		if n.Kind == yaml.ScalarNode && (n.Tag == "!!int" || n.Tag == "!!float") {
			hint = fmt.Sprintf("quote it: %q", n.Value)
		}
		d.fail("invalid_type", key+" is a string", p, hint)
		return "", false
	}
	if n.Value == "" && key != "prefix" && key != "help" && key != "default" {
		d.fail("too_small", key+" cannot be empty", p, "")
		return "", false
	}
	if r != nil && !r.match(n.Value) {
		d.fail("invalid_format", r.message, p, "")
		return "", false
	}
	return n.Value, true
}

func (d *decoder) number(f map[string]*yaml.Node, path []any, key string) (float64, bool) {
	n, given := f[key]
	if !given {
		return 0, false
	}
	if n.Kind == yaml.ScalarNode && (n.Tag == "!!int" || n.Tag == "!!float") {
		if v, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64); err == nil {
			return v, true
		}
	}
	d.fail("invalid_type", key+" is a number", child(path, key), "")
	return 0, false
}

func (d *decoder) numberPtr(f map[string]*yaml.Node, path []any, key string) *float64 {
	if v, ok := d.number(f, path, key); ok {
		return &v
	}
	return nil
}

func (d *decoder) integerPtr(f map[string]*yaml.Node, path []any, key string, lo, hi *float64) *int {
	v, ok := d.number(f, path, key)
	if !ok {
		return nil
	}
	p := child(path, key)
	if v != float64(int(v)) {
		d.fail("invalid_type", key+" is a whole number", p, "")
		return nil
	}
	if lo != nil && v < *lo {
		d.fail("too_small", fmt.Sprintf("%s is at least %v", key, *lo), p, "")
		return nil
	}
	if hi != nil && v > *hi {
		d.fail("too_big", fmt.Sprintf("%s is at most %v", key, *hi), p, "")
		return nil
	}
	i := int(v)
	return &i
}

func (d *decoder) boolean(f map[string]*yaml.Node, path []any, key string) (bool, bool) {
	n, given := f[key]
	if !given {
		return false, false
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!bool" {
		return strings.EqualFold(n.Value, "true"), true
	}
	d.fail("invalid_type", key+" is true or false", child(path, key), "")
	return false, false
}

func (d *decoder) list(f map[string]*yaml.Node, path []any, key string) []*yaml.Node {
	n, given := f[key]
	if !given {
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		d.fail("invalid_type", key+" is a list", child(path, key), "")
		return nil
	}
	return n.Content
}

func (d *decoder) strings(f map[string]*yaml.Node, path []any, key string) []string {
	items := d.list(f, path, key)
	if items == nil {
		return nil
	}
	out := []string{}
	for i, n := range items {
		if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
			d.fail("invalid_type", "expected a string", child(child(path, key), i), "")
			continue
		}
		out = append(out, n.Value)
	}
	return out
}

// nearest is the key somebody probably meant. Containment first,
// because "healthcheck" for "health" is the old name rather than a typo
// and edit distance alone would place it too far away — but bounded to
// candidates within double the shorter length, or "environment" would
// suggest the app's unrelated "env".
func nearest(key string, candidates []string) string {
	lower := strings.ToLower(key)
	best, bestD := "", -1
	for _, c := range candidates {
		cl := strings.ToLower(c)
		if !strings.Contains(lower, cl) && !strings.Contains(cl, lower) {
			continue
		}
		if max(len(cl), len(lower)) > 2*min(len(cl), len(lower)) {
			continue
		}
		if dist := distance(lower, cl); bestD < 0 || dist < bestD {
			best, bestD = c, dist
		}
	}
	if best != "" {
		return best
	}
	for _, c := range candidates {
		if dist := distance(lower, strings.ToLower(c)); dist <= 2 && (bestD < 0 || dist < bestD) {
			best, bestD = c, dist
		}
	}
	return best
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
