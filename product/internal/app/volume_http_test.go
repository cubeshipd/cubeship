package app_test

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"cubeship/internal/server/servertest"
)

type volumeView struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
	Node string `json:"node"`
}

type orphanView struct {
	ID   int64  `json:"id"`
	App  string `json:"app"`
	Path string `json:"path"`
}

func addVolume(t *testing.T, f *servertest.Fixture, ref, path string) volumeView {
	t.Helper()
	var v volumeView
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps/"+ref+"/volumes",
		map[string]any{"path": path}, f.AdminKey, &v), http.StatusCreated)
	return v
}

// A volume is added, listed and removed, and removing it keeps the data
// until somebody deletes what was kept.
func TestAVolumeIsRemovedKeepingItsDataUntilThatIsDeleted(t *testing.T) {
	f := servertest.New(t)
	created := createExternalApp(t, f, "queue")

	v := addVolume(t, f, created.Reference, "/var/lib/rabbitmq/")
	if v.Path != "/var/lib/rabbitmq" || v.Node != "control-plane" {
		t.Fatalf("added %+v", v)
	}
	dir := filepath.Join(f.DataDir, "volumes", strconv.FormatInt(v.ID, 10))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the volume's directory was not made: %v", err)
	}

	var listed []volumeView
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+created.Reference+"/volumes",
		nil, f.AdminKey, &listed), http.StatusOK)
	if len(listed) != 1 || listed[0].ID != v.ID {
		t.Fatalf("listed %+v", listed)
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		fmt.Sprintf("/apps/%s/volumes/%d", created.Reference, v.ID), nil, f.AdminKey), http.StatusNoContent)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("removing the volume deleted its data: %v", err)
	}

	var kept []orphanView
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/volumes/orphans", nil, f.AdminKey, &kept),
		http.StatusOK)
	if len(kept) != 1 || kept[0].ID != v.ID || kept[0].App != created.Reference || kept[0].Path != "/var/lib/rabbitmq" {
		t.Fatalf("kept data is %+v", kept)
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		fmt.Sprintf("/volumes/orphans/%d", v.ID), nil, f.AdminKey), http.StatusNoContent)
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the kept data is still there: %v", err)
	}
}

// Deleting an app can take its volumes' data with it, when asked.
func TestDeletingAnAppCanDeleteItsVolumeData(t *testing.T) {
	f := servertest.New(t)
	created := createExternalApp(t, f, "search")
	v := addVolume(t, f, created.Reference, "/usr/share/elasticsearch/data")
	dir := filepath.Join(f.DataDir, "volumes", strconv.FormatInt(v.ID, 10))

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/apps/"+created.Reference+"?delete_volume_data=true", nil, f.AdminKey), http.StatusOK)
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the data outlived the app it was asked to go with: %v", err)
	}
}

// A volume's data is on one machine, so an app with one runs as one copy
// there, and an app that is not one copy cannot be given one.
func TestAVolumePinsTheAppToOneCopyOnOneMachine(t *testing.T) {
	f := servertest.New(t)
	_ = addServer(t, f, "eu-1")

	pinned := createExternalApp(t, f, "queue")
	addVolume(t, f, pinned.Reference, "/data")
	for name, body := range map[string]map[string]any{
		"two copies":      {"scale": 2},
		"another machine": {"nodes": []string{"eu-1"}},
		"spread":          {"spread": true},
		"autoscaling":     {"autoscale": map[string]any{"min": 1, "max": 3, "cpu": 70}},
	} {
		servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/"+pinned.Reference, body, f.AdminKey),
			http.StatusConflict)
		_ = name
	}

	spread := createExternalApp(t, f, "api")
	place(t, f, spread.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+spread.Reference+"/volumes",
		map[string]any{"path": "/data"}, f.AdminKey), http.StatusConflict)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+pinned.Reference+"/volumes",
		map[string]any{"path": "/proc/x"}, f.AdminKey), http.StatusBadRequest)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+pinned.Reference+"/volumes",
		map[string]any{"path": "/data/"}, f.AdminKey), http.StatusConflict)
}
