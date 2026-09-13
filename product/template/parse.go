package template

import (
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type position struct{ line, column int }

// document is the parsed file plus where every key and value sits, so a
// diagnostic about ["apps", 0, "port"] can name a line.
type document struct {
	root   *yaml.Node
	keys   map[string]position
	values map[string]position
}

func pathKey(path []any) string {
	var b strings.Builder
	for _, p := range path {
		fmt.Fprint(&b, p)
		b.WriteByte(0)
	}
	return b.String()
}

func child(path []any, next any) []any {
	return append(append(make([]any, 0, len(path)+1), path...), next)
}

// at is the position of path: its key when it has one, its value when
// it is a list item, and the nearest ancestor when it is not there at
// all — a missing key still points somewhere useful.
func (d *document) at(path []any) position {
	for p := path; ; p = p[:len(p)-1] {
		k := pathKey(p)
		if pos, ok := d.keys[k]; ok {
			return pos
		}
		if pos, ok := d.values[k]; ok {
			return pos
		}
		if len(p) == 0 {
			return position{}
		}
	}
}

func (d *document) diag(severity Severity, code, message string, path []any, hint string) Diagnostic {
	pos := d.at(path)
	if path == nil {
		path = []any{}
	}
	return Diagnostic{
		Severity: severity, Code: code, Message: message, Path: path,
		Line: pos.line, Column: pos.column, Hint: hint,
	}
}

var syntaxLine = regexp.MustCompile(`^yaml: line (\d+): `)

func parse(source []byte) (*document, []Diagnostic) {
	var file yaml.Node
	if err := yaml.Unmarshal(source, &file); err != nil {
		d := Diagnostic{Severity: Error, Code: "yaml.syntax", Path: []any{}}
		msg := err.Error()
		if m := syntaxLine.FindStringSubmatch(msg); m != nil {
			fmt.Sscan(m[1], &d.Line)
			msg = msg[len(m[0]):]
		}
		d.Message = strings.TrimPrefix(msg, "yaml: ")
		return nil, []Diagnostic{d}
	}
	if len(file.Content) == 0 || file.Content[0].Kind == yaml.ScalarNode {
		return nil, []Diagnostic{{
			Severity: Error, Code: "yaml.empty", Path: []any{},
			Message: "the file is empty: a template is a mapping with at least version, project and apps",
		}}
	}

	doc := &document{root: file.Content[0], keys: map[string]position{}, values: map[string]position{}}
	var found []Diagnostic
	doc.index(doc.root, nil, &found)
	if len(found) > 0 {
		return nil, found
	}
	return doc, nil
}

// index records every position, and refuses the two things YAML allows
// that a template has no use for: a key given twice, where the parser
// would quietly keep the second, and anchors, where one alias can
// expand to more document than anybody wrote.
func (d *document) index(n *yaml.Node, path []any, found *[]Diagnostic) {
	d.values[pathKey(path)] = position{n.Line, n.Column}
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		*found = append(*found, Diagnostic{
			Severity: Error, Code: "yaml.alias", Path: nonNil(path), Line: n.Line, Column: n.Column,
			Message: "anchors and aliases are not supported: write the value out",
		})
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		seen := map[string]int{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if first, dup := seen[k.Value]; dup {
				*found = append(*found, Diagnostic{
					Severity: Error, Code: "yaml.syntax", Path: nonNil(path), Line: k.Line, Column: k.Column,
					Message: fmt.Sprintf("%q is already defined on line %d", k.Value, first),
				})
				continue
			}
			seen[k.Value] = k.Line
			p := child(path, k.Value)
			d.keys[pathKey(p)] = position{k.Line, k.Column}
			d.index(v, p, found)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			d.index(item, child(path, i), found)
		}
	}
}

func nonNil(path []any) []any {
	if path == nil {
		return []any{}
	}
	return path
}
