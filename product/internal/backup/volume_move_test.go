package backup_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"cubeship/internal/node"
	"cubeship/internal/platform/dirarchive"
	"cubeship/internal/server/servertest"
)

// fakeMachines stands in for a worker running volume jobs on its own disk:
// a backup puts an archive of dir in the store, a restore unpacks one into
// dir.
type fakeMachines struct {
	store *fakeStore
	dir   string
	jobs  []string
}

func (m *fakeMachines) RunVolumeJob(ctx context.Context, _ int64, kind string, job node.VolumeJob) (int64, error) {
	m.jobs = append(m.jobs, kind)
	c, _ := m.store.Connect(ctx, nil)
	if kind == node.CommandVolumeBackup {
		var buf bytes.Buffer
		if _, err := dirarchive.Write(m.dir, &buf); err != nil {
			return 0, err
		}
		n := int64(buf.Len())
		return n, c.Put(ctx, job.S3.Bucket, job.S3.Key, &buf, n, "application/gzip")
	}
	r, _, err := c.Get(ctx, job.S3.Bucket, job.S3.Key)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	return 0, dirarchive.Replace(r, m.dir)
}

// **A volume moves to another server by restoring its backup there**,
// never by copying a directory between machines. The data lands on the
// new server first, then the app is placed on it.
func TestAVolumeMovesToAnotherServerByRestoringItsBackupThere(t *testing.T) {
	f := newFixture(t)
	f.linkStore(t, "offsite")
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes",
		map[string]any{"name": "eu-1"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": "broker", "project": "web", "source": "external", "image": "docker.io/library/rabbitmq",
	}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/web/production/broker",
		map[string]any{"node": "eu-1"}, f.AdminKey), http.StatusOK)

	var v struct {
		ID   int64  `json:"id"`
		Node string `json:"node"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps/web/production/broker/volumes",
		map[string]any{"path": "/var/lib/rabbitmq"}, f.AdminKey, &v), http.StatusCreated)
	if v.Node != "eu-1" {
		t.Fatalf("the volume is on %q, want eu-1", v.Node)
	}

	worker := t.TempDir()
	if err := os.WriteFile(filepath.Join(worker, "queue.dat"), []byte("on eu-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	machines := &fakeMachines{store: f.store, dir: worker}
	f.Server.Backups.SetMachines(machines)

	path := fmt.Sprintf("/apps/web/production/broker/volumes/%d/backups", v.ID)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, path,
		map[string]any{"store": "offsite", "bucket": "dumps"}, f.AdminKey), http.StatusAccepted)
	f.Server.Backups.Wait()
	var rows []backupRow
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, path, nil, f.AdminKey, &rows), http.StatusOK)
	if len(rows) != 1 || rows[0].Status != "succeeded" {
		t.Fatalf("backups %+v", rows)
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, fmt.Sprintf("/backups/%d/restore", rows[0].ID),
		map[string]any{"server": node.ControlPlaneSlug}, f.AdminKey), http.StatusNoContent)

	here := filepath.Join(f.DataDir, "volumes", strconv.FormatInt(v.ID, 10), "queue.dat")
	if got, err := os.ReadFile(here); err != nil || string(got) != "on eu-1" {
		t.Fatalf("on the control plane the volume holds %q, %v", got, err)
	}
	var volumes []struct {
		Node string `json:"node"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/web/production/broker/volumes",
		nil, f.AdminKey, &volumes), http.StatusOK)
	if len(volumes) != 1 || volumes[0].Node != node.ControlPlaneSlug {
		t.Errorf("the volume is recorded on %+v", volumes)
	}
	var moved struct {
		Nodes []string `json:"nodes"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/web/production/broker",
		nil, f.AdminKey, &moved), http.StatusOK)
	if !slices.Equal(moved.Nodes, []string{node.ControlPlaneSlug}) {
		t.Errorf("the app runs on %v", moved.Nodes)
	}
	if !slices.Equal(machines.jobs, []string{node.CommandVolumeBackup}) {
		t.Errorf("jobs sent to the worker = %v, want only the backup", machines.jobs)
	}
}
