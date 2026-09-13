package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeReader struct {
	query    Query
	list     []Summary
	next     string
	err      error
	detail   *Detail
	history  []ReleaseRecord
	manifest json.RawMessage
	tag      string
	icon     []byte
}

func (f *fakeReader) List(_ context.Context, q Query) ([]Summary, string, error) {
	f.query = q
	if q.Cursor == "bogus" {
		return nil, "", ErrBadCursor
	}
	return f.list, f.next, f.err
}
func (f *fakeReader) Template(context.Context, string, string) (*Detail, error) {
	return f.detail, f.err
}
func (f *fakeReader) Releases(context.Context, string, string) ([]ReleaseRecord, bool, error) {
	return f.history, f.history != nil, f.err
}
func (f *fakeReader) Manifest(_ context.Context, _, _, tag string) (json.RawMessage, error) {
	f.tag = tag
	return f.manifest, f.err
}
func (f *fakeReader) Tags(context.Context) ([]string, error) { return []string{"analytics"}, f.err }
func (f *fakeReader) Icon(_ context.Context, id int64, commit string) ([]byte, error) {
	if id == 7 && commit == "abc1234" {
		return f.icon, f.err
	}
	return nil, f.err
}

func serve(t *testing.T, reader Reader, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&API{Reader: reader, PublicURL: "https://cubeship.dev/api/v1", Log: quietLog()}).Routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func umamiSummary() Summary {
	return Summary{Owner: "cubeshipd", Name: "cubeship-umami-template", Title: "Umami", Tags: []string{},
		RepositoryID: 7, HasIcon: true, Release: ReleaseRef{Tag: "v1.0.0", Commit: "abc1234"}}
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error.Code
}

func TestListingPassesTheQueryAndFillsIconURLs(t *testing.T) {
	f := &fakeReader{list: []Summary{umamiSummary()}, next: "c2"}
	rec := serve(t, f, "/v1/templates?q=umami&tag=analytics&sort=stars&limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if f.query != (Query{Q: "umami", Tag: "analytics", Sort: "stars", Limit: 10}) {
		t.Errorf("query = %+v", f.query)
	}
	var body struct {
		Templates []struct {
			IconURL string `json:"icon_url"`
			Title   string `json:"title"`
		} `json:"templates"`
		NextCursor string `json:"next_cursor"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Templates[0].IconURL != "https://cubeship.dev/api/v1/icons/7/abc1234.png" || body.NextCursor != "c2" {
		t.Errorf("body = %s", rec.Body)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Cache-Control") != shortCache {
		t.Errorf("headers = %v", rec.Header())
	}
	if strings.Contains(rec.Body.String(), "RepositoryID") {
		t.Error("an internal field leaked into the response")
	}
}

func TestAnEmptyCatalogIsAnEmptyList(t *testing.T) {
	rec := serve(t, &fakeReader{}, "/v1/templates")
	if !strings.Contains(rec.Body.String(), `"templates":[]`) || !strings.Contains(rec.Body.String(), `"next_cursor":null`) {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestListingRefusesWhatItCannotAnswer(t *testing.T) {
	for _, path := range []string{"/v1/templates?sort=likes", "/v1/templates?limit=0", "/v1/templates?limit=500", "/v1/templates?cursor=bogus"} {
		if rec := serve(t, &fakeReader{}, path); rec.Code != http.StatusBadRequest || errorCode(t, rec) != "invalid_query" {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestAMissingTemplateIsANotFound(t *testing.T) {
	rec := serve(t, &fakeReader{}, "/v1/templates/cubeshipd/nothing")
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != "not_found" {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, &fakeReader{}, "/v1/templates/cubeshipd/nothing/releases"); rec.Code != http.StatusNotFound {
		t.Errorf("releases: %d", rec.Code)
	}
	if rec := serve(t, &fakeReader{}, "/v1/nowhere"); rec.Code != http.StatusNotFound || errorCode(t, rec) != "not_found" {
		t.Errorf("unknown route: %d %s", rec.Code, rec.Body)
	}
}

func TestATemplateCarriesItsPage(t *testing.T) {
	f := &fakeReader{detail: &Detail{Summary: umamiSummary(), Readme: "# Umami", Manifest: json.RawMessage(`{"project":"umami"}`)}}
	rec := serve(t, f, "/v1/templates/cubeshipd/cubeship-umami-template")
	for _, want := range []string{`"readme":"# Umami"`, `"manifest":{"project":"umami"}`, `"icon_url":"https://cubeship.dev/api/v1/icons/7/abc1234.png"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("no %s in %s", want, rec.Body)
		}
	}
}

func TestTheManifestIsTheDocumentItself(t *testing.T) {
	f := &fakeReader{manifest: json.RawMessage(`{"schema_version":1}`)}
	rec := serve(t, f, "/v1/templates/cubeshipd/cubeship-umami-template/manifest?release=v1.0.0")
	if rec.Body.String() != `{"schema_version":1}` || f.tag != "v1.0.0" || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("%d %s tag=%q", rec.Code, rec.Body, f.tag)
	}
}

func TestIconsAreImmutablePNGs(t *testing.T) {
	f := &fakeReader{icon: squarePNG(128)}
	rec := serve(t, f, "/v1/icons/7/abc1234.png")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Cache-Control") != longCache {
		t.Errorf("%d %v", rec.Code, rec.Header())
	}
	for _, path := range []string{"/v1/icons/7/other12.png", "/v1/icons/x/abc1234.png", "/v1/icons/7/abc1234.svg"} {
		if rec := serve(t, f, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestADatabaseFailureIsUnavailableAndSaysNothingElse(t *testing.T) {
	rec := serve(t, &fakeReader{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")}, "/v1/tags")
	if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "unavailable" || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}
