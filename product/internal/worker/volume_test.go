package worker

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
)

// A copy with a volume is replaced by stopping the old container first:
// two containers on one data directory corrupt it. And the new one mounts
// the directory from this machine's own data directory.
func TestACopyWithAVolumeStopsTheOldContainerBeforeTheNewOneStarts(t *testing.T) {
	engine := &fakeEngine{running: []dockerx.Running{{
		ID:     "old",
		Name:   "cubeship-web-production-queue-6",
		Labels: map[string]string{node.LabelApp: "web/production/queue"},
	}}}
	dataDir := t.TempDir()
	a := New("https://cubeship.example.com", "token", "test", dataDir, nil, engine, nil, nil)
	a.healthInterval = 0

	results := a.apply(context.Background(), []node.Placement{{
		App: "web/production/queue", Deploy: 7, Ordinal: 1,
		Container: "cubeship-web-production-queue-7", Image: "rabbitmq",
		Volumes: []node.VolumeMount{{ID: 5, Path: "/var/lib/rabbitmq"}},
	}}, "")

	if len(results) != 1 || results[0].Error != "" {
		t.Fatalf("results = %+v", results)
	}
	want := []string{"stop old", "create cubeship-web-production-queue-7"}
	if !slices.Equal(engine.events[:2], want) {
		t.Errorf("events = %v, want %v first", engine.events, want)
	}
	if !slices.Contains(engine.removed, "old") {
		t.Errorf("the old container was not removed once the new one was up: %v", engine.removed)
	}
	bind := filepath.Join(dataDir, "volumes", "5") + ":/var/lib/rabbitmq"
	if got := engine.created[0].Binds; len(got) != 1 || got[0] != bind {
		t.Errorf("binds = %v, want %s", got, bind)
	}
}

// A copy without a volume keeps the order every other deploy has: the new
// container starts before the old one goes.
func TestACopyWithoutAVolumeIsNotStoppedFirst(t *testing.T) {
	engine := &fakeEngine{running: []dockerx.Running{{
		ID:     "old",
		Name:   "cubeship-web-production-api-6",
		Labels: map[string]string{node.LabelApp: "web/production/api"},
	}}}
	a := New("https://cubeship.example.com", "token", "test", t.TempDir(), nil, engine, nil, nil)
	a.healthInterval = 0

	a.apply(context.Background(), []node.Placement{{
		App: "web/production/api", Deploy: 7, Ordinal: 1,
		Container: "cubeship-web-production-api-7", Image: "nginx",
	}}, "")

	if len(engine.events) == 0 || engine.events[0] != "create cubeship-web-production-api-7" {
		t.Errorf("events = %v, want the create first", engine.events)
	}
}
