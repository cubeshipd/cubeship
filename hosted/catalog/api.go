package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Reader is what the API reads. *Postgres is one.
type Reader interface {
	List(ctx context.Context, q Query) ([]Summary, string, error)
	Template(ctx context.Context, owner, name string) (*Detail, error)
	Releases(ctx context.Context, owner, name string) ([]ReleaseRecord, bool, error)
	Manifest(ctx context.Context, owner, name, tag string) (json.RawMessage, error)
	Tags(ctx context.Context) ([]string, error)
	Icon(ctx context.Context, repositoryID int64, commit string) ([]byte, error)
}

// API is the catalog's public, read-only HTTP surface. Its address is
// pinned by instances in the field, so a route under /v1 never changes
// meaning; anything that would is /v2.
type API struct {
	Reader Reader
	// PublicURL is where /v1 is reached from outside, and what the icon
	// URLs in a response start with: https://cubeship.dev/api/v1.
	PublicURL string
	Log       *log.Logger
}

// Routes mounts every /v1 route on mux.
func (a *API) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/templates", a.list)
	mux.HandleFunc("GET /v1/templates/{owner}/{name}", a.template)
	mux.HandleFunc("GET /v1/templates/{owner}/{name}/releases", a.releases)
	mux.HandleFunc("GET /v1/templates/{owner}/{name}/manifest", a.manifest)
	mux.HandleFunc("GET /v1/tags", a.tags)
	mux.HandleFunc("GET /v1/icons/{repository}/{file}", a.icon)
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		a.fail(w, http.StatusNotFound, "not_found", "no such route")
	})
}

const (
	shortCache = "public, max-age=60"
	longCache  = "public, max-age=31536000, immutable"
)

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := Query{
		Q:      strings.TrimSpace(params.Get("q")),
		Tag:    params.Get("tag"),
		Sort:   params.Get("sort"),
		Cursor: params.Get("cursor"),
		Limit:  24,
	}
	if q.Sort == "" {
		q.Sort = "recent"
	}
	if q.Sort != "recent" && q.Sort != "stars" {
		a.fail(w, http.StatusBadRequest, "invalid_query", "sort is recent or stars")
		return
	}
	if len(q.Q) > 100 {
		a.fail(w, http.StatusBadRequest, "invalid_query", "q is at most 100 characters")
		return
	}
	if v := params.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 48 {
			a.fail(w, http.StatusBadRequest, "invalid_query", "limit is between 1 and 48")
			return
		}
		q.Limit = n
	}

	found, next, err := a.Reader.List(r.Context(), q)
	if errors.Is(err, ErrBadCursor) {
		a.fail(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	if err != nil {
		a.unavailable(w, err)
		return
	}
	for i := range found {
		a.withIcon(&found[i])
	}
	body := struct {
		Templates  []Summary `json:"templates"`
		NextCursor *string   `json:"next_cursor"`
	}{Templates: found}
	if body.Templates == nil {
		body.Templates = []Summary{}
	}
	if next != "" {
		body.NextCursor = &next
	}
	a.write(w, shortCache, body)
}

func (a *API) template(w http.ResponseWriter, r *http.Request) {
	d, err := a.Reader.Template(r.Context(), r.PathValue("owner"), r.PathValue("name"))
	if err != nil {
		a.unavailable(w, err)
		return
	}
	if d == nil {
		a.fail(w, http.StatusNotFound, "not_found", "no template is listed at that address")
		return
	}
	a.withIcon(&d.Summary)
	a.write(w, shortCache, d)
}

func (a *API) releases(w http.ResponseWriter, r *http.Request) {
	history, found, err := a.Reader.Releases(r.Context(), r.PathValue("owner"), r.PathValue("name"))
	if err != nil {
		a.unavailable(w, err)
		return
	}
	if !found {
		a.fail(w, http.StatusNotFound, "not_found", "no repository is known at that address")
		return
	}
	a.write(w, shortCache, struct {
		Releases []ReleaseRecord `json:"releases"`
	}{history})
}

func (a *API) manifest(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("release")
	m, err := a.Reader.Manifest(r.Context(), r.PathValue("owner"), r.PathValue("name"), tag)
	if err != nil {
		a.unavailable(w, err)
		return
	}
	if m == nil {
		a.fail(w, http.StatusNotFound, "not_found", "no accepted release at that address")
		return
	}
	cache := shortCache
	if tag != "" {
		// A tag can move, which is a new release; five minutes is the pass.
		cache = "public, max-age=300"
	}
	a.headers(w, cache)
	w.Write(m)
}

func (a *API) tags(w http.ResponseWriter, r *http.Request) {
	tags, err := a.Reader.Tags(r.Context())
	if err != nil {
		a.unavailable(w, err)
		return
	}
	if tags == nil {
		tags = []string{}
	}
	a.write(w, shortCache, struct {
		Tags []string `json:"tags"`
	}{tags})
}

var iconFile = regexp.MustCompile(`^([0-9a-f]{7,64})\.png$`)

func (a *API) icon(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("repository"), 10, 64)
	m := iconFile.FindStringSubmatch(r.PathValue("file"))
	if err != nil || m == nil {
		a.fail(w, http.StatusNotFound, "not_found", "no such icon")
		return
	}
	icon, err := a.Reader.Icon(r.Context(), id, m[1])
	if err != nil {
		a.unavailable(w, err)
		return
	}
	if icon == nil {
		a.fail(w, http.StatusNotFound, "not_found", "no such icon")
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", longCache)
	w.Header().Set("Content-Type", "image/png")
	w.Write(icon)
}

// withIcon fills the icon's address. The path carries the commit, so it
// never changes content and is cached forever.
func (a *API) withIcon(s *Summary) {
	if !s.HasIcon {
		return
	}
	u := fmt.Sprintf("%s/icons/%d/%s.png", strings.TrimSuffix(a.PublicURL, "/"), s.RepositoryID, s.Release.Commit)
	s.IconURL = &u
}

func (a *API) headers(w http.ResponseWriter, cache string) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cache-Control", cache)
	h.Set("Content-Type", "application/json")
}

func (a *API) write(w http.ResponseWriter, cache string, v any) {
	a.headers(w, cache)
	json.NewEncoder(w).Encode(v)
}

func (a *API) fail(w http.ResponseWriter, status int, code, message string) {
	a.headers(w, "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// unavailable is every read that failed: the database, in practice. Its
// message stays in the log, not in the response.
func (a *API) unavailable(w http.ResponseWriter, err error) {
	a.Log.Printf("api: %v", err)
	a.fail(w, http.StatusServiceUnavailable, "unavailable", "the template catalog is unavailable right now")
}
