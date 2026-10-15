// Package mcpx holds the small vocabulary every module's MCP tools share.
package mcpx

// List wraps a tool's results so they leave as a JSON object.
//
// A tool's output schema is inferred from the type its handler returns,
// and a Go slice infers as {"type": ["null", "array"]}, because a nil
// slice marshals to JSON null. The MCP schema says an outputSchema is an
// object schema, and a strict client — anything validating tools/list
// against the spec's own types — rejects the entire tool list over one
// tool that isn't. So a list result goes inside this.
type List[T any] struct {
	Items []T `json:"items"`
}

// Of wraps items, turning a nil slice into an empty one so the field is
// never JSON null.
func Of[T any](items []T) List[T] {
	if items == nil {
		items = []T{}
	}
	return List[T]{Items: items}
}
