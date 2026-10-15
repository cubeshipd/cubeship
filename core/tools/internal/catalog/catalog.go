// Package catalog builds the published snapshot with the instance's validator.
package catalog

import (
	"archive/tar"
	"bytes"
	"cubeship/template"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const templatesRepo = "cubeshipd/cubeship"
const (
	TemplateFile  = "template.yaml"
	ReadmeFile    = "README.md"
	IconFile      = "icon.png"
	templateLimit = 256 << 10
	readmeLimit   = 512 << 10
	iconLimit     = 512 << 10
)

var templateDir = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type templateFiles struct {
	source, readme, icon []byte
	paths                map[string]bool
}

type Entry struct {
	Slug        string               `json:"slug"`
	ID          string               `json:"id"` // Decimal string preserves the legacy int64 icon identity in JavaScript.
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Source      string               `json:"source"`
	Readme      string               `json:"readme"`
	Manifest    *template.Normalized `json:"manifest"`
	Icon        []byte               `json:"icon"`
}
type Snapshot struct {
	Version     int       `json:"version"`
	Commit      string    `json:"commit"`
	PublishedAt time.Time `json:"published_at"`
	Templates   []Entry   `json:"templates"`
}

// Build reads the committed tree: metadata and files always name the same revision.
func Build(repo string) (*Snapshot, error) {
	git := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		return cmd.Output()
	}
	revision, err := git("rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	sha := strings.TrimSpace(string(revision))
	date, err := git("show", "-s", "--format=%cI", sha)
	if err != nil {
		return nil, err
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(string(date)))
	if err != nil {
		return nil, err
	}
	archive, err := git("archive", "--format=tar", sha, "templates")
	if err != nil {
		return nil, err
	}
	files, err := readArchive(tar.NewReader(bytes.NewReader(archive)))
	if err != nil {
		return nil, err
	}
	return snapshot(sha, at, files)
}

func readArchive(tr *tar.Reader) (map[string]templateFiles, error) {
	files := map[string]templateFiles{}
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		parts := strings.Split(hdr.Name, "/")
		if len(parts) < 3 || path.Clean(hdr.Name) != hdr.Name || parts[1] == ".github" || parts[1] == "_cubeship" || parts[1] == "_catalog" {
			continue
		}
		if !templateDir.MatchString(parts[1]) {
			return nil, fmt.Errorf("invalid template directory: %s", parts[1])
		}
		if hdr.Size < 0 || hdr.Size > 64<<20-total {
			return nil, fmt.Errorf("invalid archive file size: %s", hdr.Name)
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
			return nil, fmt.Errorf("invalid archive file size: %s", hdr.Name)
		}
		b, err := io.ReadAll(io.LimitReader(tr, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(b)) != hdr.Size {
			return nil, fmt.Errorf("incomplete archive file: %s", hdr.Name)
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
		return nil, errors.New("the templates archive is empty")
	}
	return files, nil
}

func snapshot(sha string, at time.Time, files map[string]templateFiles) (*Snapshot, error) {
	out := &Snapshot{Version: 2, Commit: sha, PublishedAt: at}
	for slug, f := range files {
		if len(f.source) == 0 || len(f.readme) == 0 || len(f.icon) == 0 {
			return nil, fmt.Errorf("%s is missing template.yaml, README.md or icon.png", slug)
		}
		parsed := template.Validate(f.source)
		if !parsed.OK || parsed.Manifest.Name == "" {
			return nil, fmt.Errorf("%s has an invalid template.yaml: %v", slug, parsed.Diagnostics)
		}
		if err := checkBuildFiles(parsed.Manifest, slug, f.paths); err != nil {
			return nil, err
		}
		icon, code, _ := checkIcon(f.icon)
		if code != "" {
			return nil, fmt.Errorf("%s: %s", slug, code)
		}
		out.Templates = append(out.Templates, Entry{Slug: slug, ID: fmt.Sprint(templateID(slug)), Name: parsed.Manifest.Name, Description: readmeDescription(string(f.readme)), Source: string(f.source), Readme: string(f.readme), Manifest: parsed.Manifest, Icon: icon})
	}
	if len(out.Templates) == 0 {
		return nil, errors.New("the templates archive is empty")
	}
	sort.Slice(out.Templates, func(i, j int) bool { return out.Templates[i].Slug < out.Templates[j].Slug })
	return out, nil
}

func checkBuildFiles(m *template.Normalized, slug string, paths map[string]bool) error {
	for _, app := range m.Apps {
		src := app.Source
		if src.Repo != "https://github.com/"+templatesRepo {
			continue
		}
		if src.Ref == nil || *src.Ref != "master:templates/"+slug {
			return fmt.Errorf("%s: monorepo source must use ref master:templates/%s", slug, slug)
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
	if len([]rune(description)) > 300 {
		description = string([]rune(description)[:300])
	}
	return description
}
