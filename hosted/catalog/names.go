package catalog

import "strings"

// DisplayName is what a template is called. A repository is named
// cubeship-<app>-template by convention, and the catalog calls it by the
// <app>: "cubeship-uptime-kuma-template" is Uptime Kuma. A name outside
// the convention is shown as it is, capitalized.
func DisplayName(repository string) string {
	core := repository
	if len(core) > len("cubeship-") && strings.EqualFold(core[:len("cubeship-")], "cubeship-") {
		core = core[len("cubeship-"):]
	}
	if strings.HasSuffix(strings.ToLower(core), "-template") {
		core = core[:len(core)-len("-template")]
	}
	words := strings.FieldsFunc(core, func(r rune) bool { return r == '-' || r == '_' })
	if len(words) == 0 {
		return repository
	}
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// tagsOf drops the topic that lists a repository, which says nothing
// about what it is.
func tagsOf(topics []string, topic string) []string {
	out := []string{}
	for _, t := range topics {
		if t != Topic && t != topic {
			out = append(out, t)
		}
	}
	return out
}
