package template

import "regexp"

type reference struct {
	kind, key, attr, raw string
}

var attributesFor = map[string][]string{
	"input": {},
	"app":   {"internal", "host", "port"},
	"db":    {"host", "port", "user", "password", "name"},
	"store": {"bucket", "endpoint"},
}

var referencePattern = regexp.MustCompile(`\$\{(input|app|db|store)\.([a-zA-Z][a-zA-Z0-9_-]*)(?:\.([a-z]+))?\}`)

func findReferences(value string) []reference {
	var found []reference
	for _, m := range referencePattern.FindAllStringSubmatch(value, -1) {
		found = append(found, reference{kind: m[1], key: m[2], attr: m[3], raw: m[0]})
	}
	return found
}

// InternalHost is the network alias every replica of an app answers on
// — internal/app/reference.go is the original.
func InternalHost(project, environment, app string) string {
	return "cubeship-" + project + "-" + environment + "-" + app
}
