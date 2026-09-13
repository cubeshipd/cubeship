package backup_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

type volumeRow struct {
	ID int64 `json:"id"`
}

// volume makes an app with a volume and answers the volume's backups path
// and its directory on this machine.
func (f *fixture) volume(t *testing.T, app string) (string, string) {
	t.Helper()
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": app, "project": "web", "source": "external", "image": "docker.io/library/rabbitmq",
	}, f.AdminKey), http.StatusCreated)
	var v volumeRow
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps/web/production/"+app+"/volumes",
		map[string]any{"path": "/var/lib/rabbitmq"}, f.AdminKey, &v), http.StatusCreated)
	return fmt.Sprintf("/apps/web/production/%s/volumes/%d/backups", app, v.ID),
		filepath.Join(f.DataDir, "volumes", strconv.FormatInt(v.ID, 10))
}

// A volume is backed up to a linked bucket and put back: the restore
// replaces what the app wrote since with what was there.
func TestAVolumeIsBackedUpToALinkedBucketAndRestored(t *testing.T) {
	f := newFixture(t)
	f.linkStore(t, "offsite")
	path, dir := f.volume(t, "broker")
	file := filepath.Join(dir, "queue.dat")
	if err := os.WriteFile(file, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, path,
		map[string]any{"store": "offsite", "bucket": "dumps"}, f.AdminKey), http.StatusAccepted)
	f.Server.Backups.Wait()

	var rows []backupRow
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, path, nil, f.AdminKey, &rows), http.StatusOK)
	if len(rows) != 1 || rows[0].Status != "succeeded" || !rows[0].OffMachine {
		t.Fatalf("backups %+v", rows)
	}
	if keys := f.store.keys("dumps"); len(keys) != 1 {
		t.Fatalf("objects in the bucket: %v", keys)
	}

	if err := os.WriteFile(file, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPost,
		fmt.Sprintf("/backups/%d/restore", rows[0].ID), nil, f.AdminKey), http.StatusNoContent)
	if got, err := os.ReadFile(file); err != nil || string(got) != "before" {
		t.Fatalf("after the restore the file is %q, %v", got, err)
	}

	_, member := f.AddMember(t, "member", user.RoleMember)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, path, nil, member), http.StatusForbidden)
}

// **A volume's backup leaves the instance or is not taken.** Neither this
// machine's disk nor a store the instance runs survives the machine.
func TestAVolumeIsNotBackedUpOntoTheInstance(t *testing.T) {
	f := newFixture(t)
	path, _ := f.volume(t, "search")

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, path, nil, f.AdminKey), http.StatusBadRequest)
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, path+"/schedule",
		map[string]any{"at": "03:00"}, f.AdminKey), http.StatusBadRequest)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/objectstores",
		map[string]any{"kind": "managed", "name": "local"}, f.AdminKey), http.StatusCreated)
	f.Server.ObjectStores.WaitForProvisioning()
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, path,
		map[string]any{"store": "local", "bucket": "dumps"}, f.AdminKey), http.StatusBadRequest)
}
