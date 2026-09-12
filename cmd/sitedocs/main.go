// Command sitedocs writes the MCP tool reference into the site's docs,
// from the tools the daemon registers.
//
// **Generated, so the reference is the tools.** Each module registers
// its tools on a server exactly as the daemon does for a request; a
// client on an in-memory transport lists them back — name, description
// and the input schema the SDK derives from each handler's argument
// type. `-check` writes nothing and fails when the page on disk is not
// what this would write, which is what CI runs.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/machine"
	"cubeship/internal/node"
	"cubeship/internal/objectstore"
	"cubeship/internal/project"
	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const header = `---
title: Tool reference
description: Every tool /mcp offers, with what each one takes — generated from the daemon.
---

{/* Generated from the daemon's MCP tools by cmd/sitedocs — run ` + "`make reference`" + `.
    Editing this file is editing the copy rather than the thing. */}

Every tool an agent finds at ` + "`/mcp`" + `, grouped the way the daemon
registers them. A tool answers as the API key that called it: a member's
key deploys and reads, an admin's also configures the instance. What a
tool needs is in its inputs; what it refuses to do is in [MCP](/docs/mcp).

`

// A module is one Register call, named for the section it becomes. The
// services are nil: registering a tool stores a closure and calls
// nothing, and listing is all that happens here.
type module struct {
	title    string
	register func(*mcp.Server)
}

func modules() []module {
	caller := &user.User{}
	return []module{
		{"Account", func(s *mcp.Server) { user.NewTools(nil, caller, "").Register(s) }},
		{"Projects and environments", project.NewTools(nil, caller).Register},
		{"Apps", app.NewTools(nil, caller).Register},
		{"Databases", datastore.NewTools(nil, caller).Register},
		{"Object storage", objectstore.NewTools(nil, caller).Register},
		{"The machine", machine.NewTools(nil, caller).Register},
		{"Servers", node.NewTools(nil, caller).Register},
	}
}

// schema is the part of a tool's JSON Schema the reference shows.
type schema struct {
	Properties map[string]struct {
		Type        any    `json:"type"`
		Description string `json:"description"`
		Enum        []any  `json:"enum"`
	} `json:"properties"`
	Required []string `json:"required"`
}

func main() {
	check := flag.Bool("check", false, "fail if the page is not what this would write")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: sitedocs [-check] <path to tools.mdx>")
		os.Exit(2)
	}
	path := flag.Arg(0)

	body, err := render(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "sitedocs:", err)
		os.Exit(1)
	}
	if *check {
		have, err := os.ReadFile(path)
		if err != nil || string(have) != body {
			fmt.Fprintf(os.Stderr, "%s is stale, run `make reference`\n", path)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sitedocs:", err)
		os.Exit(1)
	}
}

func render(ctx context.Context) (string, error) {
	var b strings.Builder
	b.WriteString(header)
	for _, m := range modules() {
		tools, err := list(ctx, m.register)
		if err != nil {
			return "", fmt.Errorf("%s: %w", m.title, err)
		}
		fmt.Fprintf(&b, "## %s\n\n", m.title)
		for _, t := range tools {
			if err := renderTool(&b, t); err != nil {
				return "", fmt.Errorf("%s: %w", t.Name, err)
			}
		}
	}
	return b.String(), nil
}

// list registers one module's tools on a fresh server and reads them
// back through a client, which is the only way the SDK exposes them and
// is also the shape an agent sees.
func list(ctx context.Context, register func(*mcp.Server)) ([]*mcp.Tool, error) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "cubeship", Version: "docs"}, nil)
	register(srv)
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "sitedocs", Version: "docs"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	tools := res.Tools
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func renderTool(b *strings.Builder, t *mcp.Tool) error {
	fmt.Fprintf(b, "### %s\n\n%s\n\n", t.Name, mdx(t.Description))
	raw, err := json.Marshal(t.InputSchema)
	if err != nil {
		return err
	}
	var s schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if len(s.Properties) == 0 {
		b.WriteString("Takes no input.\n\n")
		return nil
	}
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	b.WriteString("| Input | Type | What it is |\n| --- | --- | --- |\n")
	for _, n := range names {
		p := s.Properties[n]
		typ := typeName(p.Type)
		if len(p.Enum) > 0 {
			vals := make([]string, len(p.Enum))
			for i, e := range p.Enum {
				vals[i] = fmt.Sprintf("`%v`", e)
			}
			typ = strings.Join(vals, " · ")
		}
		name := "`" + n + "`"
		if required[n] {
			name += " *"
		}
		fmt.Fprintf(b, "| %s | %s | %s |\n", name, typ, mdx(p.Description))
	}
	b.WriteString("\n\\* required\n\n")
	return nil
}

// typeName flattens a JSON Schema type, which is a string or a list of
// them when the field may be null.
func typeName(t any) string {
	switch v := t.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, p := range v {
			if p != "null" {
				parts = append(parts, fmt.Sprint(p))
			}
		}
		return strings.Join(parts, " or ")
	}
	return ""
}

// mdx escapes what MDX would read as JSX or an expression: a description
// says `<name>` and means the angle brackets.
func mdx(s string) string {
	r := strings.NewReplacer("<", "\\<", ">", "\\>", "{", "\\{", "}", "\\}", "|", "\\|")
	return r.Replace(s)
}
