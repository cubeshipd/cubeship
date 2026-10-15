package catalog

import (
	"archive/tar"
	"bytes"
	"cubeship/template"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotValidatesWholeCommit(t *testing.T) {
	source, err := os.ReadFile("../../../template/testdata/umami.yaml")
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.Replace(source, []byte("version: 1\n"), []byte("version: 1\nname: Umami\n"), 1)
	files := map[string]templateFiles{"umami": {source: source, readme: []byte("# Umami\n\nAnalytics.\n"), icon: squarePNG(128)}}
	s, err := snapshot(strings.Repeat("a", 40), time.Now(), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Templates) != 1 || s.Templates[0].Name != "Umami" || s.Templates[0].Description != "Analytics." || s.Templates[0].ID == "" {
		t.Fatalf("bad snapshot: %+v", s)
	}
	files["broken"] = templateFiles{source: []byte("bad yaml")}
	if _, err := snapshot(s.Commit, s.PublishedAt, files); err == nil {
		t.Fatal("accepted a partial snapshot")
	}
}

func TestArchiveIgnoresPublicationAndRefusesMissingSource(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, name := range []string{"templates/_catalog/snapshot.json", "templates/umami/README.md"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: 2, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte("{}"))
	}
	tw.Close()
	files, err := readArchive(tar.NewReader(&b))
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v error=%v", files, err)
	}
	if _, err := snapshot(strings.Repeat("a", 40), time.Now(), files); err == nil {
		t.Fatal("accepted a missing manifest")
	}
}

func TestMonorepoRequiresBuildContextInTemplateDirectory(t *testing.T) {
	ref, dockerfile := "master:templates/signoz", "collector/Dockerfile"
	m := &template.Normalized{Apps: []template.NormalizedApp{{Source: template.NormalizedSource{
		Type: "dockerfile", Repo: "https://github.com/cubeshipd/cubeship", Ref: &ref, Dockerfile: &dockerfile,
	}}}}
	if err := checkBuildFiles(m, "signoz", map[string]bool{dockerfile: true}); err != nil {
		t.Fatal(err)
	}
	if err := checkBuildFiles(m, "signoz", nil); err == nil {
		t.Fatal("accepted a missing Dockerfile")
	}
}

func TestBuildReadsOnlyCommittedTemplates(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	source, err := os.ReadFile("../../../template/testdata/umami.yaml")
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.Replace(source, []byte("version: 1\n"), []byte("version: 1\nname: Umami\n"), 1)
	for name, data := range map[string][]byte{
		"templates/umami/template.yaml": source,
		"templates/umami/README.md":     []byte("# Umami\n\nAnalytics.\n"),
		"templates/umami/icon.png":      squarePNG(128),
		"core/invalid/template.yaml":    []byte("invalid"),
	} {
		target := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "Fixture")
	sha := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "templates/umami/template.yaml"), []byte("invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := Build(repo)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != 2 || s.Commit != sha || len(s.Templates) != 1 || s.Templates[0].Slug != "umami" {
		t.Fatalf("unexpected snapshot: %+v", s)
	}
}
