package templateinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Catalog is where templates come from.
type Catalog interface {
	// List is a page of the catalog, for query's q, tag, sort and cursor.
	List(ctx context.Context, query url.Values) (json.RawMessage, error)
	// Tags is every tag a listed template carries.
	Tags(ctx context.Context) (json.RawMessage, error)
	// Template is one template with its README, file and manifest.
	Template(ctx context.Context, owner, repo string) (json.RawMessage, error)
	// Releases is a template's history, newest first.
	Releases(ctx context.Context, owner, repo string) ([]CatalogRelease, error)
	// Icon is a release's icon, as the catalog serves it.
	Icon(ctx context.Context, repository, file string) ([]byte, error)
	// Source is template.yaml at commit, from the repository itself.
	Source(ctx context.Context, owner, repo, commit string) ([]byte, error)
}

// CatalogRelease is a release as the catalog recorded it.
type CatalogRelease struct {
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
	Status string `json:"status"`
}

// HTTPCatalog reads cubeship.dev's API — or wherever CUBESHIP_CATALOG_URL
// points — and a template's file from GitHub, at the commit the catalog
// recorded. Never the file the catalog stored: the instance validates
// what the repository holds, not somebody else's copy of it.
type HTTPCatalog struct {
	// API is the catalog's /v1, e.g. https://cubeship.dev/api/v1.
	API string
	// Raw is where a repository's files are read, e.g.
	// https://raw.githubusercontent.com.
	Raw string
	// IconPath is what an icon address in a listing is rewritten to
	// start with, so the dashboard fetches icons from the instance and
	// the browser never talks to the catalog.
	IconPath string
	HTTP     *http.Client
}

// DefaultCatalogURL is the catalog an instance reads unless told otherwise.
const DefaultCatalogURL = "https://cubeship.dev/api/v1"

func NewHTTPCatalog(api, iconPath string) *HTTPCatalog {
	return &HTTPCatalog{
		API:      strings.TrimRight(api, "/"),
		Raw:      "https://raw.githubusercontent.com",
		IconPath: iconPath,
		HTTP:     &http.Client{Timeout: 20 * time.Second},
	}
}

var (
	segment    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	commitSHA  = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	numericID  = regexp.MustCompile(`^[0-9]{1,20}$`)
	iconFile   = regexp.MustCompile(`^[0-9a-f]{7,64}\.png$`)
	listParams = []string{"q", "tag", "sort", "cursor", "limit"}
)

const (
	jsonLimit   = 4 << 20
	iconLimit   = 1 << 20
	sourceLimit = 1 << 20
)

func (c *HTTPCatalog) get(ctx context.Context, address string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)
	}
	return body, resp.StatusCode, nil
}

func (c *HTTPCatalog) readJSON(ctx context.Context, address string) (json.RawMessage, error) {
	body, status, err := c.get(ctx, address, jsonLimit)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusNotFound:
		return nil, ErrTemplateNotFound
	case status != http.StatusOK:
		return nil, fmt.Errorf("%w: it answered %d", ErrCatalogUnavailable, status)
	}
	if c.IconPath != "" {
		body = bytes.ReplaceAll(body, []byte(`"`+c.API+`/icons/`), []byte(`"`+c.IconPath))
	}
	return body, nil
}

func (c *HTTPCatalog) List(ctx context.Context, query url.Values) (json.RawMessage, error) {
	kept := url.Values{}
	for _, name := range listParams {
		if v := query.Get(name); v != "" {
			kept.Set(name, v)
		}
	}
	address := c.API + "/templates"
	if len(kept) > 0 {
		address += "?" + kept.Encode()
	}
	return c.readJSON(ctx, address)
}

func (c *HTTPCatalog) Tags(ctx context.Context) (json.RawMessage, error) {
	return c.readJSON(ctx, c.API+"/tags")
}

func (c *HTTPCatalog) Template(ctx context.Context, owner, repo string) (json.RawMessage, error) {
	if !segment.MatchString(owner) || !segment.MatchString(repo) {
		return nil, ErrTemplateNotFound
	}
	return c.readJSON(ctx, c.API+"/templates/"+owner+"/"+repo)
}

func (c *HTTPCatalog) Releases(ctx context.Context, owner, repo string) ([]CatalogRelease, error) {
	if !segment.MatchString(owner) || !segment.MatchString(repo) {
		return nil, ErrTemplateNotFound
	}
	body, err := c.readJSON(ctx, c.API+"/templates/"+owner+"/"+repo+"/releases")
	if err != nil {
		return nil, err
	}
	var page struct {
		Releases []CatalogRelease `json:"releases"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)
	}
	return page.Releases, nil
}

func (c *HTTPCatalog) Icon(ctx context.Context, repository, file string) ([]byte, error) {
	if !numericID.MatchString(repository) || !iconFile.MatchString(file) {
		return nil, ErrTemplateNotFound
	}
	body, status, err := c.get(ctx, c.API+"/icons/"+repository+"/"+file, iconLimit)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusNotFound:
		return nil, ErrTemplateNotFound
	case status != http.StatusOK:
		return nil, fmt.Errorf("%w: it answered %d", ErrCatalogUnavailable, status)
	}
	return body, nil
}

func (c *HTTPCatalog) Source(ctx context.Context, owner, repo, commit string) ([]byte, error) {
	if !segment.MatchString(owner) || !segment.MatchString(repo) || !commitSHA.MatchString(commit) {
		return nil, ErrReleaseNotFound
	}
	body, status, err := c.get(ctx, c.Raw+"/"+owner+"/"+repo+"/"+commit+"/template.yaml", sourceLimit+1)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusNotFound:
		return nil, ErrReleaseNotFound
	case status != http.StatusOK:
		return nil, fmt.Errorf("%w: GitHub answered %d for template.yaml", ErrCatalogUnavailable, status)
	case len(body) > sourceLimit:
		return nil, errors.New("template.yaml is larger than 1 MiB")
	}
	return body, nil
}
