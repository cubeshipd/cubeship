package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
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

// A route registered as internal must stay out of the document. Without
// this, "document everything" and "document only what a consumer calls"
// would quietly become "document whatever happens to be there".
func TestInternalRoutesAreNotDocumented(t *testing.T) {
	f := servertest.New(t)
	doc := f.Server.OpenAPI()

	for _, pattern := range f.Server.InternalPatterns() {
		method, path := httpx.SplitPattern(pattern)
		if op, ok := doc.Paths[path][strings.ToLower(method)]; ok {
			t.Errorf("%s is registered as internal but documented as %q", pattern, op.OperationID)
		}
	}
}

// The document is the product's surface, not an inventory of routes.
// Pinning the list here means removing something from it — or slipping
// something machine-facing back in — is a deliberate edit.
func TestDocumentedSurfaceIsTheProductAPI(t *testing.T) {
	f := servertest.New(t)

	want := []string{
		"DELETE /apps/{project}/{env}/{name}",
		"DELETE /apps/{project}/{env}/{name}/domains/{domainID}",
		"DELETE /credentials/{id}",
		"DELETE /datastores/{name}",
		"DELETE /datastores/{name}/attachments/{project}/{env}/{app}",
		"DELETE /datastores/{name}/expose",
		"DELETE /dns/{id}/records",
		"DELETE /firewall/docker",
		"DELETE /firewall/rules/{index}",
		"DELETE /github/{id}",
		"DELETE /projects/{projectSlug}",
		"DELETE /projects/{projectSlug}/environments/{envSlug}",
		"DELETE /registries/{id}",
		"DELETE /registries/{id}/images",
		"DELETE /registries/{id}/repositories",
		"DELETE /registry/images",
		"DELETE /registry/repositories",
		"DELETE /users/{username}",
		"DELETE /users/{username}/credentials",
		"GET /apps",
		"GET /apps/{project}/{env}/{name}",
		"GET /apps/{project}/{env}/{name}/deployments",
		"GET /apps/{project}/{env}/{name}/deployments/{id}",
		"GET /apps/{project}/{env}/{name}/env",
		"GET /apps/{project}/{env}/{name}/logs",
		"GET /apps/{project}/{env}/{name}/metrics",
		"GET /certificates",
		"GET /credentials",
		"GET /credentials/providers",
		"GET /datastores",
		"GET /datastores/engines",
		"GET /datastores/{name}",
		"GET /datastores/{name}/credentials",
		"GET /datastores/{name}/logs",
		"GET /datastores/{name}/metrics",
		"GET /dns/{id}/records",
		"GET /dns/{id}/status",
		"GET /dns/{id}/zones",
		"GET /firewall",
		"GET /github",
		"GET /github/branches",
		"GET /github/repositories",
		"GET /projects",
		"GET /projects/{projectSlug}/env",
		"GET /projects/{projectSlug}/environments",
		"GET /projects/{projectSlug}/environments/{envSlug}/env",
		"GET /registries",
		"GET /registries/{id}/images",
		"GET /registries/{id}/repositories",
		"GET /registries/{id}/status",
		"GET /registries/{id}/usage",
		"GET /registry/images",
		"GET /registry/repositories",
		"GET /settings",
		"GET /users",
		"GET /users/me",
		"PATCH /apps/{project}/{env}/{name}",
		"PATCH /apps/{project}/{env}/{name}/domains/{domainID}",
		"PATCH /apps/{project}/{env}/{name}/env",
		"PATCH /credentials/{id}",
		"PATCH /datastores/{name}",
		"PATCH /projects/{projectSlug}",
		"PATCH /projects/{projectSlug}/env",
		"PATCH /projects/{projectSlug}/environments/{envSlug}",
		"PATCH /projects/{projectSlug}/environments/{envSlug}/env",
		"POST /apps",
		"POST /apps/{project}/{env}/{name}/deploy",
		"POST /apps/{project}/{env}/{name}/domains",
		"POST /credentials",
		"POST /datastores",
		"POST /datastores/{name}/attachments",
		"POST /datastores/{name}/expose",
		"POST /datastores/{name}/start",
		"POST /datastores/{name}/stop",
		"POST /firewall/disable",
		"POST /firewall/docker",
		"POST /firewall/enable",
		"POST /firewall/rules",
		"POST /github",
		"POST /projects",
		"POST /projects/{projectSlug}/environments",
		"POST /registries",
		"POST /registry/garbage-collect",
		"POST /settings/github/manifest",
		"POST /settings/github/manifest/state",
		"POST /users",
		"PUT /apps/{project}/{env}/{name}/env",
		"PUT /dns/{id}/records",
		"PUT /projects/{projectSlug}/env",
		"PUT /projects/{projectSlug}/environments/{envSlug}/env",
		"PUT /registries/{id}",
		"PUT /settings",
	}
	if got := f.Server.Patterns(); !slices.Equal(got, want) {
		t.Errorf("the documented API changed.\n got: %v\nwant: %v", got, want)
	}

	// The daemon still serves the rest; it just doesn't advertise it.
	wantInternal := []string{
		"/",
		"/api/",
		"DELETE /users/me/api-keys/{id}",
		"GET /docs",
		"GET /healthz",
		"GET /openapi.json",
		"GET /setup",
		"GET /users/me/api-keys",
		"GET /v2/token",
		"POST /auth/login",
		"POST /auth/logout",
		"POST /hooks/github",
		"POST /hooks/registry",
		"POST /mcp",
		"POST /setup",
		"POST /users/me/api-key/rotate",
		"POST /users/me/api-keys",
		"PUT /users/me/password",
	}
	if got := f.Server.InternalPatterns(); !slices.Equal(got, wantInternal) {
		t.Errorf("the internal routes changed.\n got: %v\nwant: %v", got, wantInternal)
	}
}

// Every schema the document defines is pointed at, and every schema it
// points at is defined.
//
// Both halves catch the same edit from opposite sides: removing the last
// operation that used a schema leaves it defined and unreachable, and
// removing a schema still referenced leaves the reference rendering
// blank in the browser. Neither is a compile error, and neither shows up
// anywhere but in the published document.
func TestEverySchemaIsDefinedAndUsed(t *testing.T) {
	f := servertest.New(t)
	doc := f.Server.OpenAPI()

	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode the document: %v", err)
	}
	refs := map[string]bool{}
	for _, m := range regexp.MustCompile(`#/components/schemas/([A-Za-z0-9_]+)`).
		FindAllStringSubmatch(string(encoded), -1) {
		refs[m[1]] = true
	}

	for name := range refs {
		if _, ok := doc.Components.Schemas[name]; !ok {
			t.Errorf("the document references schema %q, which it does not define", name)
		}
	}
	for name := range doc.Components.Schemas {
		if !refs[name] {
			t.Errorf("schema %q is defined but nothing references it", name)
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

	doc := f.DoRoot(t, http.MethodGet, server.OpenAPIPath)
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

	docs := f.DoRoot(t, http.MethodGet, server.DocsPath)
	servertest.RequireStatus(t, docs, http.StatusOK)
	body := docs.Body.String()
	if !strings.Contains(body, `data-url="`+server.OpenAPIPath+`"`) {
		t.Errorf("the reference page does not point at %s", server.OpenAPIPath)
	}
	if !strings.Contains(body, "@scalar/api-reference@") {
		t.Error("the reference page does not load a pinned Scalar build")
	}

	// The version says which file to ask the CDN for; the hash says what
	// the file is. Without the second, a CDN serving something else
	// under that version would run its code on the same origin as the
	// session cookie — /docs is this daemon's own address.
	if !strings.Contains(body, `integrity="sha384-`) || !strings.Contains(body, `crossorigin="anonymous"`) {
		t.Error("the Scalar build is loaded without subresource integrity")
	}

	csp := docs.Header().Get("Content-Security-Policy")
	scripts, _, _ := strings.Cut(strings.TrimPrefix(csp, "default-src 'self'; "), ";")
	if strings.Contains(scripts, "unsafe-inline") {
		t.Errorf("the reference page allows inline scripts; it has none and Scalar needs none: %q", scripts)
	}
	if !strings.Contains(csp, "connect-src 'self'") {
		t.Errorf("the reference page may talk to somewhere other than this daemon: %q", csp)
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

// The reference page's "try it" has to target a real address. It used to
// advertise the literal template "https://api.{domain}", which Scalar
// renders as a field the reader has to fill in by hand.
func TestServedDocumentTargetsTheAddressItWasFetchedFrom(t *testing.T) {
	f := servertest.New(t)

	type serverEntry struct {
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	fetch := func(t *testing.T, mutate func(*http.Request)) []serverEntry {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, server.OpenAPIPath, nil)
		if mutate != nil {
			mutate(req)
		}
		rec := httptest.NewRecorder()
		f.Server.Router().ServeHTTP(rec, req)
		servertest.RequireStatus(t, rec, http.StatusOK)

		var doc struct {
			Servers []serverEntry `json:"servers"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("decode document: %v", err)
		}
		if len(doc.Servers) == 0 {
			t.Fatal("the document offers no server at all")
		}
		for _, s := range doc.Servers {
			if strings.ContainsAny(s.URL, "{}") {
				t.Errorf("server URL %q still carries a placeholder", s.URL)
			}
			// The paths in the document are the ones the modules
			// register; the prefix that says where they live is here.
			if !strings.HasSuffix(s.URL, httpx.APIPrefix) {
				t.Errorf("server URL %q does not end in %s, so every path in the document is wrong",
					s.URL, httpx.APIPrefix)
			}
		}
		return doc.Servers
	}

	t.Run("plain HTTP, as over an SSH tunnel", func(t *testing.T) {
		servers := fetch(t, func(r *http.Request) { r.Host = "127.0.0.1:9000" })
		if servers[0].URL != "http://127.0.0.1:9000"+httpx.APIPrefix {
			t.Errorf("first server is %q, want the address the request arrived on", servers[0].URL)
		}
		// The configured public address is still offered as an alternative.
		if len(servers) != 2 || servers[1].URL != "https://"+servertest.APIHost+httpx.APIPrefix {
			t.Errorf("expected the canonical address as a second option, got %v", servers)
		}
	})

	t.Run("through Traefik, which terminates TLS", func(t *testing.T) {
		servers := fetch(t, func(r *http.Request) {
			r.Host = servertest.APIHost
			r.Header.Set("X-Forwarded-Proto", "https")
		})
		if servers[0].URL != "https://"+servertest.APIHost+httpx.APIPrefix {
			t.Errorf("first server is %q, want https://%s%s", servers[0].URL, servertest.APIHost, httpx.APIPrefix)
		}
		// Origin and canonical coincide, so it is offered once, not twice.
		if len(servers) != 1 {
			t.Errorf("expected a single server entry, got %v", servers)
		}
	})

	t.Run("a chained proxy appends to X-Forwarded-Proto", func(t *testing.T) {
		servers := fetch(t, func(r *http.Request) {
			r.Host = servertest.APIHost
			r.Header.Set("X-Forwarded-Proto", "https, http")
		})
		if servers[0].URL != "https://"+servertest.APIHost+httpx.APIPrefix {
			t.Errorf("first server is %q; only the client-facing scheme counts", servers[0].URL)
		}
	})
}
