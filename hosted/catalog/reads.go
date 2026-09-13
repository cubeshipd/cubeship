package catalog

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ReleaseRef is one release, as a listing names it.
type ReleaseRef struct {
	Tag         string    `json:"tag"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Commit      string    `json:"commit"`
	PublishedAt time.Time `json:"published_at"`
}

// Summary is a template as the catalog lists it: its repository and the
// newest release that was accepted.
type Summary struct {
	Owner       string     `json:"owner"`
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	URL         string     `json:"url"`
	Stars       int        `json:"stars"`
	Tags        []string   `json:"tags"`
	AvatarURL   string     `json:"avatar_url"`
	IconURL     *string    `json:"icon_url"`
	Release     ReleaseRef `json:"release"`

	RepositoryID int64 `json:"-"`
	HasIcon      bool  `json:"-"`
}

// Detail is one template with everything its page shows.
type Detail struct {
	Summary
	Readme string `json:"readme"`
	Source string `json:"source"`
	// SourceURL is template.yaml at the release's commit, which is what an
	// instance will fetch when it does not trust us.
	SourceURL string          `json:"source_url"`
	Manifest  json.RawMessage `json:"manifest"`
}

// ReleaseRecord is a release in a repository's history, refused ones
// included, so an author can see why.
type ReleaseRecord struct {
	ReleaseRef
	Status   string          `json:"status"`
	Problems json.RawMessage `json:"problems"`
}

// Query is a page of the catalog.
type Query struct {
	Q, Tag string
	Sort   string // "recent" or "stars"
	Cursor string
	Limit  int
}

// ErrBadCursor is a cursor this API did not give out.
var ErrBadCursor = errors.New("the cursor is not one this API gave out")

const summaryColumns = `p.id, p.owner, p.name, p.description, p.url, p.stars, to_json(p.topics)::text,
	p.owner_avatar_url, r.tag, r.name, r.url, r.commit_sha, r.published_at, r.icon IS NOT NULL`

func (p *Postgres) topic() string {
	if p.Topic == "" {
		return Topic
	}
	return p.Topic
}

func (p *Postgres) scanSummary(row interface{ Scan(...any) error }, extra ...any) (Summary, error) {
	var s Summary
	var topics string
	dest := append([]any{
		&s.RepositoryID, &s.Owner, &s.Name, &s.Description, &s.URL, &s.Stars, &topics, &s.AvatarURL,
		&s.Release.Tag, &s.Release.Name, &s.Release.URL, &s.Release.Commit, &s.Release.PublishedAt, &s.HasIcon,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return s, err
	}
	var list []string
	if err := json.Unmarshal([]byte(topics), &list); err != nil {
		return s, err
	}
	s.Title = DisplayName(s.Name)
	s.Tags = tagsOf(list, p.topic())
	return s, nil
}

// Keyset paging, not offset: the catalog is sorted by something that
// changes between two page loads.
func encodeCursor(key, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", key, id)))
}

func decodeCursor(cursor string) (int64, int64, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, 0, false
	}
	a, b, found := strings.Cut(string(raw), ":")
	key, err1 := strconv.ParseInt(a, 10, 64)
	id, err2 := strconv.ParseInt(b, 10, 64)
	return key, id, found && err1 == nil && err2 == nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// A template is listed when its repository is not hidden and one of its
// releases was accepted.
const listed = `FROM repositories p JOIN releases r ON r.id = p.latest_release_id WHERE p.hidden IS NULL`

func (p *Postgres) List(ctx context.Context, q Query) ([]Summary, string, error) {
	var args []any
	arg := func(v any, cast string) string {
		args = append(args, v)
		return fmt.Sprintf("$%d::%s", len(args), cast)
	}
	where := []string{}
	if q.Q != "" {
		like := arg("%"+escapeLike(strings.ToLower(q.Q))+"%", "text")
		where = append(where, fmt.Sprintf("(lower(p.name) LIKE %[1]s OR lower(p.description) LIKE %[1]s)", like))
	}
	if q.Tag != "" {
		where = append(where, arg(q.Tag, "text")+" = ANY(p.topics)")
	}
	key := "extract(epoch FROM r.published_at)::bigint"
	if q.Sort == "stars" {
		key = "p.stars::bigint"
	}
	if q.Cursor != "" {
		k, id, ok := decodeCursor(q.Cursor)
		if !ok {
			return nil, "", ErrBadCursor
		}
		where = append(where, fmt.Sprintf("(%s, p.id) < (%s, %s)", key, arg(k, "bigint"), arg(id, "bigint")))
	}
	filter := ""
	if len(where) > 0 {
		filter = " AND " + strings.Join(where, " AND ")
	}
	query := fmt.Sprintf(`SELECT %s, %s %s%s ORDER BY %s DESC, p.id DESC LIMIT %s`,
		summaryColumns, key, listed, filter, key, arg(q.Limit+1, "int"))

	rows, err := p.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []Summary
	var keys []int64
	for rows.Next() {
		var k int64
		s, err := p.scanSummary(rows, &k)
		if err != nil {
			return nil, "", err
		}
		out, keys = append(out, s), append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > q.Limit {
		out = out[:q.Limit]
		next = encodeCursor(keys[q.Limit-1], out[q.Limit-1].RepositoryID)
	}
	return out, next, nil
}

// Template is nil when no listed template answers to owner/name.
func (p *Postgres) Template(ctx context.Context, owner, name string) (*Detail, error) {
	var d Detail
	var manifest string
	row := p.DB.QueryRowContext(ctx, `
		SELECT `+summaryColumns+`, COALESCE(r.readme, ''), COALESCE(r.source, ''), COALESCE(r.manifest::text, 'null')
		`+listed+` AND lower(p.owner) = lower($1) AND lower(p.name) = lower($2)
		-- Two repositories can briefly answer to one name across a rename.
		ORDER BY p.checked_at DESC LIMIT 1`, owner, name)
	s, err := p.scanSummary(row, &d.Readme, &d.Source, &manifest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Summary = s
	d.Manifest = json.RawMessage(manifest)
	d.SourceURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", s.Owner, s.Name, s.Release.Commit, TemplateFile)
	return &d, nil
}

// Releases is a repository's history, newest first; found is false when
// no visible repository answers to owner/name. It does not need an
// accepted release, so an author whose first one was refused can see why.
func (p *Postgres) Releases(ctx context.Context, owner, name string) ([]ReleaseRecord, bool, error) {
	var id int64
	err := p.DB.QueryRowContext(ctx, `
		SELECT id FROM repositories
		WHERE hidden IS NULL AND lower(owner) = lower($1) AND lower(name) = lower($2)
		ORDER BY checked_at DESC LIMIT 1`, owner, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	rows, err := p.DB.QueryContext(ctx, `
		SELECT tag, name, url, commit_sha, published_at, status, problems::text
		FROM releases WHERE repository_id = $1
		ORDER BY published_at DESC, id DESC LIMIT 50`, id)
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()
	out := []ReleaseRecord{}
	for rows.Next() {
		var r ReleaseRecord
		var problems string
		if err := rows.Scan(&r.Tag, &r.Name, &r.URL, &r.Commit, &r.PublishedAt, &r.Status, &problems); err != nil {
			return nil, true, err
		}
		r.Problems = json.RawMessage(problems)
		out = append(out, r)
	}
	return out, true, rows.Err()
}

// Manifest is the normalized manifest of an accepted release — the
// listed one when tag is empty — or nil.
func (p *Postgres) Manifest(ctx context.Context, owner, name, tag string) (json.RawMessage, error) {
	var manifest string
	var err error
	if tag == "" {
		err = p.DB.QueryRowContext(ctx, `SELECT r.manifest::text `+listed+`
			AND lower(p.owner) = lower($1) AND lower(p.name) = lower($2)
			ORDER BY p.checked_at DESC LIMIT 1`, owner, name).Scan(&manifest)
	} else {
		err = p.DB.QueryRowContext(ctx, `
			SELECT r.manifest::text FROM repositories p JOIN releases r ON r.repository_id = p.id
			WHERE p.hidden IS NULL AND lower(p.owner) = lower($1) AND lower(p.name) = lower($2)
			  AND r.tag = $3 AND r.status = 'accepted'
			ORDER BY r.published_at DESC, r.id DESC LIMIT 1`, owner, name, tag).Scan(&manifest)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return json.RawMessage(manifest), err
}

// Tags is every topic a listed template carries.
func (p *Postgres) Tags(ctx context.Context) ([]string, error) {
	rows, err := p.DB.QueryContext(ctx, `
		SELECT DISTINCT t FROM repositories p, unnest(p.topics) t
		WHERE p.hidden IS NULL AND p.latest_release_id IS NOT NULL
		ORDER BY t LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		all = append(all, t)
	}
	return tagsOf(all, p.topic()), rows.Err()
}

// Icon is an accepted release's icon, or nil.
func (p *Postgres) Icon(ctx context.Context, repositoryID int64, commit string) ([]byte, error) {
	var icon []byte
	err := p.DB.QueryRowContext(ctx, `
		SELECT r.icon FROM releases r JOIN repositories p ON p.id = r.repository_id
		WHERE r.repository_id = $1 AND r.commit_sha = $2 AND r.status = 'accepted'
		  AND r.icon IS NOT NULL AND p.hidden IS NULL
		LIMIT 1`, repositoryID, commit).Scan(&icon)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return icon, err
}
