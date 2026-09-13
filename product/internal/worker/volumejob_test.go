package worker

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"cubeship/internal/node"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/dockerx"
)

// bucket is an S3 client holding objects in memory.
type bucket struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (b *bucket) Buckets(context.Context) ([]objectstore.Bucket, error) { return nil, nil }
func (b *bucket) MakeBucket(context.Context, string) error              { return nil }
func (b *bucket) RemoveBucket(context.Context, string) error            { return nil }
func (b *bucket) List(context.Context, string, string, string, int) (objectstore.Listing, error) {
	return objectstore.Listing{}, nil
}
func (b *bucket) RemoveAll(context.Context, string, string) (int, error) { return 0, nil }

func (b *bucket) Put(_ context.Context, _, key string, r io.Reader, _ int64, _ string) error {
	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = content
	return nil
}

func (b *bucket) Get(_ context.Context, _, key string) (io.ReadCloser, objectstore.Object, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	content, ok := b.objects[key]
	if !ok {
		return nil, objectstore.Object{}, objectstore.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(content)), objectstore.Object{}, nil
}

func (b *bucket) Remove(_ context.Context, _, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, key)
	return nil
}

// A worker backs its own volume up straight to the bucket, with the app
// stopped for the copy and started again after, and restores it from
// there. The answer goes home with the archive's size.
func TestAWorkerBacksUpAndRestoresItsOwnVolume(t *testing.T) {
	answers := make(chan string, 2)
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		answers <- r.URL.RawQuery + string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer home.Close()

	engine := &fakeEngine{running: []dockerx.Running{{
		ID: "queue", Name: "cubeship-web-production-queue-7",
		Labels: map[string]string{node.LabelApp: "web/production/queue"},
	}}}
	dataDir := t.TempDir()
	a := New(home.URL, "token", "test", dataDir, nil, engine, nil, nil)
	store := &bucket{objects: map[string][]byte{}}
	a.s3 = func(context.Context, node.S3Object) (objectstore.Client, error) { return store, nil }

	file := filepath.Join(dataDir, "volumes", "5", "queue.dat")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := &node.VolumeJob{ID: 5, App: "web/production/queue", S3: node.S3Object{Bucket: "b", Key: "k.tar.gz"}}

	a.volumeJob(node.Command{ID: "1", Kind: node.CommandVolumeBackup, Volume: job})
	if got := <-answers; !bytes.Contains([]byte(got), []byte(`"size":`)) {
		t.Fatalf("the backup answered %q", got)
	}
	if len(store.objects["k.tar.gz"]) == 0 {
		t.Fatal("nothing was uploaded")
	}
	if want := []string{"stop queue", "start queue"}; !slices.Equal(engine.events, want) {
		t.Errorf("events = %v, want %v", engine.events, want)
	}
	if a.isPaused(job.App) {
		t.Error("the app is still paused after its backup")
	}

	if err := os.WriteFile(file, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.volumeJob(node.Command{ID: "2", Kind: node.CommandVolumeRestore, Volume: job})
	if got := <-answers; bytes.Contains([]byte(got), []byte("error=")) {
		t.Fatalf("the restore answered %q", got)
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "before" {
		t.Errorf("after the restore the file is %q, %v", got, err)
	}
}

// While a volume job has an app stopped, apply leaves it alone: a stopped
// container is not a missing one to start again.
func TestApplyLeavesAPausedAppAlone(t *testing.T) {
	engine := &fakeEngine{}
	a := New("https://cubeship.example.com", "token", "test", t.TempDir(), nil, engine, nil, nil)
	a.pause("web/production/queue")

	results := a.apply(context.Background(), []node.Placement{{
		App: "web/production/queue", Deploy: 7, Ordinal: 1,
		Container: "cubeship-web-production-queue-7", Image: "rabbitmq",
	}}, "")
	if len(results) != 0 || len(engine.created) != 0 {
		t.Errorf("apply started a paused app: results %+v, created %v", results, engine.createdNames())
	}
}
