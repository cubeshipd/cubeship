package discovery

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"slices"
	"time"
)

type fakeGitHub struct {
	repos   []Repo
	lookup  map[string]Repo
	files   map[string][]byte // "owner/name@commit:path"
	failing map[string]bool   // same key: a transient error
	looked  []string
}

func (f *fakeGitHub) Search(context.Context, string) ([]Repo, error) { return f.repos, nil }

func (f *fakeGitHub) Lookup(_ context.Context, ids []string) (map[string]Repo, error) {
	f.looked = append(f.looked, ids...)
	out := map[string]Repo{}
	for _, id := range ids {
		if r, ok := f.lookup[id]; ok {
			out[id] = r
		}
	}
	return out, nil
}

func (f *fakeGitHub) File(_ context.Context, owner, name, commit, path string, limit int64) ([]byte, error) {
	key := owner + "/" + name + "@" + commit + ":" + path
	if f.failing[key] {
		return nil, errors.New("GitHub answered 502")
	}
	b, ok := f.files[key]
	if !ok {
		return nil, ErrNotFound
	}
	if int64(len(b)) > limit {
		return nil, ErrTooLarge
	}
	return b, nil
}

type fakeStore struct {
	blocked  map[string]bool
	repos    map[int64]Repo
	hidden   map[int64]string
	releases []Indexed
}

func newStore() *fakeStore {
	return &fakeStore{blocked: map[string]bool{}, repos: map[int64]Repo{}, hidden: map[int64]string{}}
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

type fakeBucket struct{ objects map[string][]byte }

func (b *fakeBucket) Put(_ context.Context, key string, body []byte, _ string) error {
	b.objects[key] = body
	return nil
}

func squarePNG(side int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	img.Set(1, 1, color.RGBA{R: 45, G: 226, B: 230, A: 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func quietLog() *log.Logger { return log.New(io.Discard, "", 0) }

var published = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
