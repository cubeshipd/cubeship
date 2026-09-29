package catalog

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cubeship/template"
)

const templatesRepo = "cubeshipd/cubeship-templates"

var templateDir = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type MonoSource struct {
	HTTP    *http.Client
	API     string
	Archive string
	Token   string
}

type templateFiles struct {
	source, readme, icon []byte
	paths                map[string]bool
}

// Read takes one commit and one archive, so all templates in a pass share a revision.
func (m *MonoSource) Read(ctx context.Context) (string, time.Time, map[string]templateFiles, error) {
	api := m.API
	if api == "" {
		api = "https://api.github.com"
	}
	archive := m.Archive
	if archive == "" {
		archive = "https://codeload.github.com"
	}
	client := m.HTTP
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	request := func(address string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
		if err != nil {
			return nil, err
		}
		if m.Token != "" {
			req.Header.Set("Authorization", "Bearer "+m.Token)
		}
		return client.Do(req)
	}
	res, err := request(api + "/repos/" + templatesRepo + "/commits/main")
	if err != nil {
		return "", time.Time{}, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", time.Time{}, nil, fmt.Errorf("GitHub commit answered %d", res.StatusCode)
	}
	var head struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&head); err != nil {
		return "", time.Time{}, nil, err
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(head.SHA) {
		return "", time.Time{}, nil, errors.New("GitHub returned an invalid commit")
	}
	res, err = request(archive + "/" + templatesRepo + "/tar.gz/" + head.SHA)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", time.Time{}, nil, fmt.Errorf("GitHub archive answered %d", res.StatusCode)
	}
	gz, err := gzip.NewReader(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return "", time.Time{}, nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := map[string]templateFiles{}
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", time.Time{}, nil, err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		parts := strings.Split(hdr.Name, "/")
		if len(parts) < 3 || path.Clean(hdr.Name) != hdr.Name || parts[1] == ".github" || parts[1] == "_cubeship" {
			continue
		}
		if !templateDir.MatchString(parts[1]) {
			return "", time.Time{}, nil, fmt.Errorf("invalid template directory: %s", parts[1])
		}
		if hdr.Size < 0 || hdr.Size > 64<<20-total {
			return "", time.Time{}, nil, fmt.Errorf("invalid archive file size: %s", hdr.Name)
		}
		total += hdr.Size
		f := files[parts[1]]
		if f.paths == nil {
			f.paths = map[string]bool{}
		}
		f.paths[strings.Join(parts[2:], "/")] = true
		files[parts[1]] = f
		if len(parts) != 3 {
			continue
		}
		var limit int64
		switch parts[2] {
		case TemplateFile:
			limit = templateLimit
		case ReadmeFile:
			limit = readmeLimit
		case IconFile:
			limit = iconLimit
		default:
			continue
		}
		if hdr.Size > limit {
			return "", time.Time{}, nil, fmt.Errorf("invalid archive file size: %s", hdr.Name)
		}
		b, err := io.ReadAll(io.LimitReader(tr, limit+1))
		if err != nil {
			return "", time.Time{}, nil, err
		}
		if int64(len(b)) != hdr.Size {
			return "", time.Time{}, nil, fmt.Errorf("incomplete archive file: %s", hdr.Name)
		}
		switch parts[2] {
		case TemplateFile:
			f.source = b
		case ReadmeFile:
			f.readme = b
		case IconFile:
			f.icon = b
		}
		files[parts[1]] = f
	}
	if len(files) == 0 {
		return "", time.Time{}, nil, errors.New("the templates archive is empty")
	}
	return head.SHA, head.Commit.Committer.Date, files, nil
}

// MonoSyncer replaces discovery with the directories in cubeship-templates.
// A failed download changes no rows; a bad directory leaves the previous accepted copy listed.
type MonoSyncer struct {
	Source *MonoSource
	Store  Store
}

func (s *MonoSyncer) Run(ctx context.Context) (Report, error) {
	var rep Report
	sha, at, files, err := s.Source.Read(ctx)
	if err != nil {
		return rep, err
	}
	entries := make([]SnapshotEntry, 0, len(files))
	for slug, f := range files {
		if len(f.source) == 0 || len(f.readme) == 0 || len(f.icon) == 0 {
			return rep, fmt.Errorf("%s is missing template.yaml, README.md or icon.png", slug)
		}
		parsed := template.Validate(f.source)
		if !parsed.OK || parsed.Manifest.Name == "" {
			return rep, fmt.Errorf("%s has an invalid template.yaml: %v", slug, parsed.Diagnostics)
		}
		if err := checkBuildFiles(parsed.Manifest, slug, f.paths); err != nil {
			return rep, err
		}
		clean, code, _ := checkIcon(f.icon)
		if code != "" {
			return rep, fmt.Errorf("%s: %s", slug, code)
		}
		id := templateID(slug)
		node := "mono:" + slug
		url := "https://github.com/" + templatesRepo + "/tree/" + sha + "/" + slug
		r := Repo{ID: id, NodeID: node, Owner: "cubeshipd", Name: slug, URL: url, OwnerAvatar: "https://github.com/cubeshipd.png", Description: readmeDescription(string(f.readme)), Topics: []string{}}
		rec := Indexed{RepositoryID: id, Tag: sha, Commit: sha, Name: parsed.Manifest.Name, URL: url, PublishedAt: at, Accepted: true, Problems: parsed.Diagnostics, Manifest: parsed.Manifest, Source: string(f.source), Readme: string(f.readme), Icon: clean}
		entries = append(entries, SnapshotEntry{r, rec})
	}
	rep.Repositories = len(entries)
	rep.Accepted, rep.Hidden, err = s.Store.Publish(ctx, entries)
	if err != nil {
		rep.Failed = 1
		return rep, err
	}

	return rep, nil
}

func checkBuildFiles(m *template.Normalized, slug string, paths map[string]bool) error {
	for _, app := range m.Apps {
		src := app.Source
		if src.Repo != "https://github.com/"+templatesRepo {
			continue
		}
		if src.Ref == nil || *src.Ref != "main:"+slug {
			return fmt.Errorf("%s: monorepo source must use ref main:%s", slug, slug)
		}
		dockerfile := "Dockerfile"
		if src.Dockerfile != nil {
			dockerfile = *src.Dockerfile
		}
		if !filepath.IsLocal(dockerfile) || !paths[dockerfile] {
			return fmt.Errorf("%s: missing Dockerfile %s", slug, dockerfile)
		}
	}
	return nil
}

// Negative IDs cannot collide with GitHub repository IDs already stored in the catalog.
func templateID(slug string) int64 {
	var n uint64 = 14695981039346656037
	for i := 0; i < len(slug); i++ {
		n ^= uint64(slug[i])
		n *= 1099511628211
	}
	return -int64(n&0x7fffffffffffffff) - 1
}

func readmeDescription(readme string) string {
	lines := strings.Split(readme, "\n")
	var text []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(text) == 0 && (line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "![") || strings.HasPrefix(line, "<")) {
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			break
		}
		text = append(text, line)
	}
	description := strings.Join(text, " ")
	description = regexp.MustCompile(`\[([^]]+)\]\([^)]+\)`).ReplaceAllString(description, "$1")
	if len(description) > 300 {
		description = description[:300]
	}
	return description
}
