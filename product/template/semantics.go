package template

import (
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
)

// checkSemantics is everything the shape cannot say.
func checkSemantics(m Manifest, doc *document) []Diagnostic {
	c := &checker{doc: doc}

	type item struct{ key, name string }
	kinds := []struct {
		field string
		items []item
	}{
		{"inputs", mapItems(m.Inputs, func(i Input) item { return item{i.Key, ""} })},
		{"databases", mapItems(m.Databases, func(d Database) item { return item{d.Key, d.Name} })},
		{"stores", mapItems(m.Stores, func(s Store) item { return item{s.Key, s.Name} })},
		{"apps", mapItems(m.Apps, func(a App) item { return item{a.Key, a.Name} })},
	}

	// Keys are unique within their kind; a name is unique across the
	// whole instance for a database, so within the file at the very least.
	for _, kind := range kinds {
		keys, names := map[string]bool{}, map[string]bool{}
		for i, it := range kind.items {
			if keys[it.key] {
				c.error("key.duplicate", fmt.Sprintf("two %s share the key %q", kind.field, it.key), []any{kind.field, i, "key"}, "")
			}
			keys[it.key] = true
			if kind.field == "inputs" {
				continue
			}
			name := it.name
			if name == "" {
				name = it.key
			}
			at := []any{kind.field, i, "name"}
			if !validSlugShape(name) {
				c.error("name.reserved", fmt.Sprintf("%q is not a valid name: lowercase letters, digits and dashes", name), at, "")
			}
			if reserved[kind.field][name] {
				c.error("name.reserved", fmt.Sprintf("%q is a reserved name on an instance", name), at, "")
			}
			if names[name] {
				c.error("name.duplicate", fmt.Sprintf("two %s would be called %q", kind.field, name), at, "")
			}
			names[name] = true
		}
	}

	if m.MinCubeship != "" && !validRange(m.MinCubeship) {
		c.error("version.range", fmt.Sprintf("%q is not a version", m.MinCubeship), []any{"minCubeship"}, "")
	}

	for i, in := range m.Inputs {
		if in.Type == "secret" && in.Generate != nil && *in.Generate < 8 {
			c.error("input.generate", "a generated secret is at least 8 characters", []any{"inputs", i, "generate"}, "")
		}
	}

	for i, db := range m.Databases {
		at := func(k string) []any { return []any{"databases", i, k} }
		e := findEngine(db.Engine)
		if e == nil {
			c.error("engine.unknown", fmt.Sprintf("%q is not an engine this platform runs", db.Engine), at("engine"),
				"postgres, mysql, mariadb, redis or mongodb")
			continue
		}
		if db.Version != "" && !slices.Contains(e.versions, db.Version) {
			c.error("engine.version", fmt.Sprintf("%s %s is not offered", e.name, db.Version), at("version"),
				"try "+strings.Join(e.versions, ", "))
		}
		if db.Username != "" {
			if e.fixedUser != "" && db.Username != e.fixedUser {
				c.error("engine.username", fmt.Sprintf("%s only has the user %q", e.name, e.fixedUser), at("username"), "")
			}
			if slices.Contains(e.refused, db.Username) {
				c.error("engine.username", fmt.Sprintf("%s refuses the user %q", e.name, db.Username), at("username"), "")
			}
		}
		if db.Database != "" && !e.hasDatabase {
			c.add(Warning, "database.ignored", fmt.Sprintf("%s has no named databases, so this is ignored", e.name), at("database"), "")
		}
		c.limits(db.Limits, []any{"databases", i, "limits"})
	}
	for i, s := range m.Stores {
		c.limits(s.Limits, []any{"stores", i, "limits"})
	}

	known := map[string]map[string]bool{"input": {}, "app": {}, "db": {}, "store": {}}
	domainInputs := map[string]bool{}
	for _, in := range m.Inputs {
		known["input"][in.Key] = true
		if in.Type == "domain" {
			domainInputs[in.Key] = true
		}
	}
	for _, a := range m.Apps {
		known["app"][a.Key] = true
	}
	for _, db := range m.Databases {
		known["db"][db.Key] = true
	}
	for _, s := range m.Stores {
		known["store"][s.Key] = true
	}

	checkText := func(text string, path []any) {
		for _, r := range findReferences(text) {
			if !known[r.kind][r.key] {
				c.error("reference.unknown", fmt.Sprintf("%s names no %s in this template", r.raw, r.kind), path, "")
				continue
			}
			attrs := attributesFor[r.kind]
			switch {
			case len(attrs) == 0 && r.attr != "":
				c.error("reference.attribute", r.raw+": an input has no attributes", path, "")
			case len(attrs) > 0 && !slices.Contains(attrs, r.attr):
				c.error("reference.attribute", fmt.Sprintf("%s: a %s has %s", r.raw, r.kind, strings.Join(attrs, ", ")), path, "")
			}
		}
	}

	inputsByKey := map[string]Input{}
	for _, in := range m.Inputs {
		inputsByKey[in.Key] = in
	}
	// Host ports as written, across every app: the instance binds each once.
	hostPorts := map[string]bool{}

	for i, a := range m.Apps {
		at := func(rest ...any) []any { return append([]any{"apps", i}, rest...) }

		building := a.Repo != "" || a.Build != ""
		if a.Image == "" && !building {
			c.error("source.missing", "an app runs an image or builds a repository", at("image"), "give image, or repo with build")
		}
		if a.Image != "" && building {
			c.error("source.conflict", "an app is either an image or a build, not both", at("image"), "")
		}
		if strings.Contains(a.Image, ":") {
			c.error("image.tagged", "the image carries no tag: the tag is its own field", at("image"), "")
		}
		if building {
			if a.Repo == "" {
				c.error("source.missing", "a build needs a repo", at("repo"), "")
			}
			if a.Build == "" {
				c.error("build.missing", "a repo needs build: dockerfile or railpack", at("build"), "")
			}
			if a.Repo != "" && !hasScheme(a.Repo) {
				c.error("repo.scheme", "a repository is an http, https or git URL", at("repo"), "")
			}
			if strings.Contains(a.Repo, "#") {
				c.error("repo.ref", "the branch goes in ref, not after a #", at("repo"), "")
			}
			if a.Tag != "" {
				c.error("source.conflict", "a built app has no tag", at("tag"), "")
			}
			if a.Dockerfile != "" && a.Build != "dockerfile" {
				c.error("dockerfile.misplaced", "dockerfile only means something with build: dockerfile", at("dockerfile"), "")
			}
		}

		if a.Health != "" {
			if problem := healthPathProblem(a.Health); problem != "" {
				c.error("health.path", problem, at("health"), "")
			}
		}

		for d, dom := range a.Domains {
			p := at("domains", d, "host")
			refs := findReferences(dom.Host)
			if len(refs) != 1 || refs[0].raw != strings.TrimSpace(dom.Host) || refs[0].kind != "input" {
				c.error("domain.literal", "a domain comes from an input, never a literal host", p,
					"add an input of type domain and use ${input.<key>}")
				continue
			}
			// Existence and attribute shape go through the same check as
			// everything else; only "is this input a domain" is specific here.
			checkText(dom.Host, p)
			if known["input"][refs[0].key] && !domainInputs[refs[0].key] {
				c.error("domain.literal", refs[0].raw+" is not an input of type domain", p, "")
			}
		}

		writes := map[string]string{}
		claim := func(names []string, owner string, path []any) {
			for _, name := range names {
				if prev, taken := writes[name]; taken {
					c.error("attach.collision", fmt.Sprintf("%s and %s both write %s: give one a prefix", prev, owner, name), path, "")
					return
				}
				writes[name] = owner
			}
		}
		for x, att := range a.Attach {
			if att.Prefix != "" && !prefixPattern.MatchString(att.Prefix) {
				c.error("attach.prefix", "a prefix is upper case and ends in an underscore, like ANALYTICS_", at("attach", x, "prefix"), "")
			}
			if (att.Database == "") == (att.Store == "") {
				c.error("attach.kind", "an attachment names a database or a store, exactly one", at("attach", x), "")
				continue
			}
			if att.Database != "" {
				idx := slices.IndexFunc(m.Databases, func(d Database) bool { return d.Key == att.Database })
				if idx < 0 {
					c.error("attach.unknown", fmt.Sprintf("no database in this template has the key %q", att.Database), at("attach", x, "database"), "")
					continue
				}
				if e := findEngine(m.Databases[idx].Engine); e != nil {
					claim(databaseVarNames(e, att.Prefix), att.Database, at("attach", x, "prefix"))
				}
				continue
			}
			isStore := slices.ContainsFunc(m.Stores, func(s Store) bool { return s.Key == att.Store })
			isInput := slices.ContainsFunc(m.Inputs, func(in Input) bool { return in.Key == att.Store && in.Type == "store" })
			if !isStore && !isInput {
				c.error("attach.unknown", fmt.Sprintf("no store or store input has the key %q", att.Store), at("attach", x, "store"), "")
				continue
			}
			if att.Bucket == "" {
				c.error("attach.bucket", "a store attachment names the bucket it gives the app", at("attach", x, "bucket"), "")
			}
			claim(storeVarNames(att.Prefix), att.Store, at("attach", x, "prefix"))
		}

		for _, name := range sortedKeys(a.Env) {
			checkText(a.Env[name], at("env", name))
			if owner, taken := writes[name]; taken {
				c.add(Warning, "attach.collision", fmt.Sprintf("this overrides %s, which attaching %s already writes", name, owner), at("env", name), "")
			}
		}

		c.limits(a.Limits, at("limits"))

		paths := map[string]bool{}
		for x, v := range a.Volumes {
			clean, problem := volumePath(v.Path)
			if problem != "" {
				c.error("volume.path", problem, at("volumes", x, "path"), "")
				continue
			}
			if paths[clean] {
				c.error("volume.duplicate", "two volumes of this app are at "+clean, at("volumes", x, "path"), "")
			}
			paths[clean] = true
		}
		if len(a.Volumes) > 0 {
			if (a.Scale != nil && *a.Scale > 1) || (a.Spread != nil && *a.Spread) || a.Autoscale != nil {
				c.error("volume.one-copy", "an app with a volume runs as one copy on one machine: drop scale, spread and autoscale",
					at("volumes"), "a volume's data is on one machine, and two copies writing it corrupt it")
			}
			if m.MinCubeship == "" || slices.ContainsFunc(withoutVolumes, func(v string) bool {
				ok, _ := Satisfies(m.MinCubeship, v)
				return ok
			}) {
				c.error("volume.min-cubeship", "a template with a volume needs a minCubeship that "+
					"excludes releases before volumes, or an older instance installs it without its data surviving",
					at("volumes"), `set minCubeship: "`+volumesSince+`"`)
			}
		}

		containerPorts := map[int]bool{}
		for x, tp := range a.TCP {
			if containerPorts[tp.Port] {
				c.error("tcp.duplicate", fmt.Sprintf("two entries of this app publish port %d", tp.Port), at("tcp", x, "port"), "")
			}
			containerPorts[tp.Port] = true
			if tp.Host == "" {
				continue
			}
			p := at("tcp", x, "host")
			if n, err := strconv.Atoi(tp.Host); err == nil {
				if n < minTCPHostPort || n > 65535 {
					c.error("tcp.host", fmt.Sprintf("a host port is from %d to 65535", minTCPHostPort), p, "")
				}
			} else {
				refs := findReferences(tp.Host)
				if len(refs) != 1 || refs[0].raw != tp.Host || refs[0].kind != "input" || refs[0].attr != "" {
					c.error("tcp.host", fmt.Sprintf("a host port is a number from %d to 65535, or ${input.<key>} naming a number input", minTCPHostPort), p, "")
					continue
				}
				in, ok := inputsByKey[refs[0].key]
				if !ok {
					c.error("reference.unknown", refs[0].raw+" names no input in this template", p, "")
					continue
				}
				if in.Type != "number" {
					c.error("tcp.host", fmt.Sprintf("%s is a %s input, and a host port is answered by a number", refs[0].raw, in.Type), p,
						"add an input of type number")
					continue
				}
				if in.Min == nil || *in.Min < minTCPHostPort || in.Max == nil || *in.Max > 65535 {
					c.add(Warning, "tcp.host-range", fmt.Sprintf("give %s min: %d and max: 65535, so an answer no instance publishes on is asked again rather than failing the install", refs[0].raw, minTCPHostPort), p, "")
				}
			}
			if hostPorts[tp.Host] {
				c.error("tcp.host-duplicate", "two ports in this template are published on the same host port, "+tp.Host, p, "")
			}
			hostPorts[tp.Host] = true
		}
		if len(a.TCP) > 0 {
			if (a.Scale != nil && *a.Scale > 1) || (a.Spread != nil && *a.Spread) || a.Autoscale != nil {
				c.error("tcp.one-copy", "an app publishing a TCP port runs as one copy on one machine: drop scale, spread and autoscale",
					at("tcp"), "a host port is bound by one container")
			}
			if m.MinCubeship == "" || slices.ContainsFunc(withoutTCP, func(v string) bool {
				ok, _ := Satisfies(m.MinCubeship, v)
				return ok
			}) {
				c.error("tcp.min-cubeship", "a template publishing a TCP port needs a minCubeship that "+
					"excludes releases before TCP ports, or an older instance installs it without the port",
					at("tcp"), `set minCubeship: "`+tcpSince+`"`)
			}
		}

		if as := a.Autoscale; as != nil {
			lo := 1
			if as.Min != nil {
				lo = *as.Min
			}
			if as.Max < 1 || as.Max > maxAutoscale {
				c.error("autoscale.range", fmt.Sprintf("max is between 1 and %d", maxAutoscale), at("autoscale", "max"), "")
			}
			if lo < 1 || lo > as.Max {
				c.error("autoscale.range", "min is between 1 and max", at("autoscale", "min"), "")
			}
			if as.CPU <= 0 {
				c.error("autoscale.range", "cpu is a target above 0, where 100 is one core", at("autoscale", "cpu"), "")
			}
		}
	}

	return c.found
}

// volumesSince is the first release whose instances mount volumes.
const volumesSince = "0.7.0"

// withoutVolumes are the newest releases that do not: a minCubeship either
// satisfies installs on an instance that would drop the volume. Two,
// because a range leaves prereleases out unless it names one — ">=0.6.0"
// is not satisfied by 0.7.0-rc.5, and is by any 0.6.
var withoutVolumes = []string{"0.6.999", "0.7.0-rc.5"}

// tcpSince is the first release whose instances publish an app's TCP ports.
const tcpSince = "0.7.2"

// withoutTCP are the newest releases that do not, two for the reason
// withoutVolumes is two.
var withoutTCP = []string{"0.7.1", "0.7.2-rc.0"}

// reservedMounts are paths a container gets from the kernel. The daemon's
// app.CleanVolumePath refuses the same, which rules_test holds it to.
var reservedMounts = []string{"/proc", "/sys", "/dev"}

// volumePath is a volume's path cleaned, or why it is not one.
func volumePath(p string) (string, string) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, ":,\x00") {
		return "", "a volume path is absolute inside the container, without a colon or a comma"
	}
	clean := path.Clean(p)
	if clean == "/" {
		return "", "a volume cannot be the container's root"
	}
	for _, reserved := range reservedMounts {
		if clean == reserved || strings.HasPrefix(clean, reserved+"/") {
			return "", "a volume cannot be under " + reserved + ", which the kernel provides"
		}
	}
	return clean, ""
}

type checker struct {
	doc   *document
	found []Diagnostic
}

func (c *checker) add(s Severity, code, message string, path []any, hint string) {
	c.found = append(c.found, c.doc.diag(s, code, message, path, hint))
}

func (c *checker) error(code, message string, path []any, hint string) {
	c.add(Error, code, message, path, hint)
}

func (c *checker) limits(l *Limits, path []any) {
	if l == nil {
		return
	}
	if l.CPU != nil && *l.CPU < minCPU {
		c.error("limits.cpu", fmt.Sprintf("a CPU limit is at least %v of a core", minCPU), child(path, "cpu"), "")
	}
	if l.Memory != nil {
		if bytes, ok := parseSize(*l.Memory); !ok {
			c.error("limits.memory", "a memory limit is a size: 512Mi, 2Gi, 1500M", child(path, "memory"), "")
		} else if bytes < minMemory {
			c.error("limits.memory", "a memory limit is at least 6Mi", child(path, "memory"), "")
		}
	}
}

func hasScheme(repo string) bool {
	for _, s := range []string{"http://", "https://", "git://"} {
		if strings.HasPrefix(repo, s) {
			return true
		}
	}
	return false
}

func mapItems[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
