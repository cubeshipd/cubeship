package server_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/platform/openapi"
	"cubeship/internal/server"
	"cubeship/internal/server/servertest"
)

// The document has to describe exactly what the daemon serves — not
// approximately. A route with no operation is an endpoint nobody can
// discover; an operation with no route is a promise the daemon breaks.
func TestOpenAPIDescribesEveryRouteAndNoOthers(t *testing.T) {
	f := servertest.New(t)
	doc := f.Server.OpenAPI()

	documented := map[string]bool{}
	for path, item := range doc.Paths {
		for method := range item {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}

	served := map[string]bool{}
	for _, pattern := range f.Server.Patterns() {
		method, path := httpx.SplitPattern(pattern)
		if method == "" {
			t.Errorf("route %q has no method, so it cannot be documented precisely", pattern)
			continue
		}
		served[method+" "+path] = true
	}

	for route := range served {
		if !documented[route] {
			t.Errorf("route %s is served but not in the OpenAPI document", route)
		}
	}
	for route := range documented {
		if !served[route] {
			t.Errorf("OpenAPI documents %s, which the daemon does not serve", route)
		}
	}
}

// Every operation needs the parts that make a reference usable: an id to
// generate clients from, a summary to list it by, and at least one
// response.
func TestEveryOperationIsComplete(t *testing.T) {
	f := servertest.New(t)
	doc := f.Server.OpenAPI()

	seenIDs := map[string]string{}
	for path, item := range doc.Paths {
		for method, op := range item {
			where := strings.ToUpper(method) + " " + path

			if op.OperationID == "" {
				t.Errorf("%s has no operationId", where)
			} else if prev, dup := seenIDs[op.OperationID]; dup {
				t.Errorf("%s reuses the operationId %q, already used by %s", where, op.OperationID, prev)
			} else {
				seenIDs[op.OperationID] = where
			}

			if op.Summary == "" {
				t.Errorf("%s has no summary", where)
			}
			if len(op.Responses) == 0 {
				t.Errorf("%s documents no responses", where)
			}
			if len(op.Tags) == 0 {
				t.Errorf("%s has no tag, so it lands in an unnamed group", where)
			}

			// Path parameters must be declared, or a client cannot fill
			// them in and the reference renders a broken URL.
			for _, segment := range strings.Split(path, "/") {
				if !strings.HasPrefix(segment, "{") {
					continue
				}
				name := strings.Trim(segment, "{}")
				declared := slices.ContainsFunc(op.Parameters, func(p openapi.Parameter) bool {
					return p.Name == name && p.In == "path"
				})
				if !declared {
					t.Errorf("%s does not declare its path parameter %q", where, name)
				}
			}
		}
	}
}

// Every $ref must resolve, or the reference page renders empty boxes.
func TestEverySchemaReferenceResolves(t *testing.T) {
	f := servertest.New(t)

	raw, err := json.Marshal(f.Server.OpenAPI())
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	const prefix = `"$ref":"#/components/schemas/`
	for rest := string(raw); ; {
		i := strings.Index(rest, prefix)
		if i < 0 {
			break
		}
		rest = rest[i+len(prefix):]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			t.Fatal("malformed $ref in the document")
		}
		name := rest[:end]
		if _, ok := doc.Components.Schemas[name]; !ok {
			t.Errorf("$ref points at %q, which is not in components.schemas", name)
		}
	}
}

// The document and the reference page must be reachable without a key:
// Scalar fetches the document from the browser, with no credentials to
// offer.
func TestDocsAndDocumentAreServedUnauthenticated(t *testing.T) {
	f := servertest.New(t)

	doc := f.Do(t, http.MethodGet, server.OpenAPIPath, nil, "")
	servertest.RequireStatus(t, doc, http.StatusOK)
	if ct := doc.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("document Content-Type is %q, want application/json", ct)
	}
	var parsed map[string]any
	if err := json.Unmarshal(doc.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("the served document is not valid JSON: %v", err)
	}
	if parsed["openapi"] != "3.1.0" {
		t.Errorf("document declares openapi %v, want 3.1.0", parsed["openapi"])
	}

	docs := f.Do(t, http.MethodGet, server.DocsPath, nil, "")
	servertest.RequireStatus(t, docs, http.StatusOK)
	body := docs.Body.String()
	if !strings.Contains(body, `data-url="`+server.OpenAPIPath+`"`) {
		t.Errorf("the reference page does not point at %s", server.OpenAPIPath)
	}
	if !strings.Contains(body, "@scalar/api-reference@") {
		t.Error("the reference page does not load a pinned Scalar build")
	}
}

// Authentication is declared once, at the document level, and only the
// endpoints that genuinely need something else override it.
func TestSecurityIsDeclaredCorrectly(t *testing.T) {
	f := servertest.New(t)
	doc := f.Server.OpenAPI()

	if len(doc.Security) != 1 {
		t.Fatalf("expected one document-wide security requirement, got %v", doc.Security)
	}
	if _, ok := doc.Security[0][server.BearerScheme]; !ok {
		t.Errorf("the default requirement is not %q: %v", server.BearerScheme, doc.Security[0])
	}

	for _, name := range []string{server.BearerScheme} {
		if _, ok := doc.Components.SecuritySchemes[name]; !ok {
			t.Errorf("security scheme %q is required but never defined", name)
		}
	}

	// Anything that overrides the default must name a scheme that exists.
	for path, item := range doc.Paths {
		for method, op := range item {
			if op.Security == nil {
				continue
			}
			for _, requirement := range *op.Security {
				for scheme := range requirement {
					if _, ok := doc.Components.SecuritySchemes[scheme]; !ok {
						t.Errorf("%s %s requires the undefined security scheme %q",
							strings.ToUpper(method), path, scheme)
					}
				}
			}
		}
	}
}
