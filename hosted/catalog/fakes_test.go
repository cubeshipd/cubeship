package catalog

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"os"
	"slices"
	"testing"
	"time"
)

type fakeStore struct {
	blocked  map[string]bool
	repos    map[int64]Repo
	hidden   map[int64]string
	releases []Indexed
}

func newStore() *fakeStore {
	return &fakeStore{blocked: map[string]bool{}, repos: map[int64]Repo{}, hidden: map[int64]string{}}
}

func (s *fakeStore) Publish(_ context.Context, entries []SnapshotEntry) (int, int, error) {
	accepted := 0
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Repository.NodeID] = true
		s.repos[e.Repository.ID] = e.Repository
		s.hidden[e.Repository.ID] = ""
		exists, _ := s.Indexed(context.Background(), e.Repository.ID, e.Indexed.Tag, e.Indexed.Commit)
		if !exists {
			s.releases = append(s.releases, e.Indexed)
			accepted++
		}
	}
	hidden := 0
	for id, r := range s.repos {
		if !seen[r.NodeID] && s.hidden[id] != HiddenGone {
			s.hidden[id] = HiddenGone
			hidden++
		}
	}
	return accepted, hidden, nil
}

func (s *fakeStore) Blocked(context.Context) (map[string]bool, error) { return s.blocked, nil }

func (s *fakeStore) Repositories(context.Context) ([]Known, error) {
	var out []Known
	for id, r := range s.repos {
		out = append(out, Known{ID: id, NodeID: r.NodeID})
	}
	slices.SortFunc(out, func(a, b Known) int { return int(a.ID - b.ID) })
	return out, nil
}

func (s *fakeStore) SaveRepository(_ context.Context, r Repo, hidden string) error {
	s.repos[r.ID] = r
	s.hidden[r.ID] = hidden
	return nil
}

func (s *fakeStore) Hide(_ context.Context, id int64, reason string) error {
	s.hidden[id] = reason
	return nil
}

func (s *fakeStore) Indexed(_ context.Context, id int64, tag, commit string) (bool, error) {
	return slices.ContainsFunc(s.releases, func(r Indexed) bool {
		return r.RepositoryID == id && r.Tag == tag && r.Commit == commit
	}), nil
}

func (s *fakeStore) SaveRelease(_ context.Context, r Indexed) error {
	s.releases = append(s.releases, r)
	return nil
}

func squarePNG(side int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	img.Set(1, 1, color.RGBA{R: 45, G: 226, B: 230, A: 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

var published = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func quietLog() *log.Logger { return log.New(io.Discard, "", 0) }

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../product/template/testdata/umami.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func umamiRepo() Repo {
	return Repo{NodeID: "R_umami", ID: 7, Owner: "lucasaarch", Name: "cubeship-umami-template",
		URL: "https://github.com/lucasaarch/cubeship-umami-template", Stars: 3, Topics: []string{Topic, "analytics"}}
}
