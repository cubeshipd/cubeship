package mcpx_test

import (
	"context"
	"encoding/json"
	"testing"

	"cubeship/internal/mcpx"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type row struct {
	Name string `json:"name"`
}

// The point of the wrapper: the SDK infers a tool's output schema from
// the type its handler returns, and a bare []row infers as
// {"type": ["null", "array"]} — not an object schema, which the MCP
// schema requires. A client validating tools/list rejects the whole list
// over one such tool.
func TestListInfersAnObjectSchema(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "v1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "list_rows", Description: "rows"},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, mcpx.List[row], error) {
			return nil, mcpx.Of([]row{{Name: "a"}}), nil
		})

	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "v1"}, nil)
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), st, nil); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	session, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	raw, err := json.Marshal(tools.Tools[0].OutputSchema)
	if err != nil {
		t.Fatalf("marshal output schema: %v", err)
	}
	var schema struct {
		Type any `json:"type"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal output schema: %v", err)
	}
	if schema.Type != "object" {
		t.Fatalf("output schema type is %v, want object", schema.Type)
	}
}

// A nil slice would marshal to JSON null, which is not what an empty list
// means to a reader on the other end.
func TestOfTurnsNilIntoAnEmptyList(t *testing.T) {
	b, err := json.Marshal(mcpx.Of[row](nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"items":[]}` {
		t.Fatalf("got %s, want {\"items\":[]}", b)
	}
}
