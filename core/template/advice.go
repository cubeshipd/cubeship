package template

import "strings"

// advise is what never refuses a file but is worth telling its author.
func advise(m Manifest, doc *document) []Diagnostic {
	c := &checker{doc: doc}

	for i, db := range m.Databases {
		if db.Version == "" {
			c.add(Warning, "advice.unpinned-engine", "without a version, two installs of this template run different engines",
				[]any{"databases", i, "engine"}, "")
		}
	}

	// An app nothing addresses is usually a mistake: it has no domain and
	// no other app names it.
	referenced := map[string]bool{}
	for _, a := range m.Apps {
		for _, v := range a.Env {
			for _, r := range findReferences(v) {
				if r.kind == "app" {
					referenced[r.key] = true
				}
			}
		}
	}

	for i, a := range m.Apps {
		at := func(k string) []any { return []any{"apps", i, k} }
		if a.Health == "" {
			c.add(Warning, "advice.no-health", "with no health path, a deploy cannot tell whether this started", at("key"), "")
		}
		if a.Image != "" && a.Tag == "" {
			c.add(Warning, "advice.floating-tag", "without a tag this follows the registry: two installs may differ", at("image"), "")
		}
		if a.Image != "" && !strings.Contains(a.Image, "/") {
			c.add(Info, "advice.dockerhub", "an unqualified image comes from Docker Hub, which rate-limits anonymous pulls", at("image"), "")
		}
		if a.Limits == nil {
			c.add(Warning, "advice.no-limits", "with no limits, one copy of this app can take the whole machine", at("key"), "")
		}
		if len(a.Domains) == 0 && !referenced[a.Key] {
			c.add(Warning, "advice.unreachable-app", "nothing reaches this app: it has no domain and no other app names it", at("key"), "")
		}
	}
	return c.found
}
