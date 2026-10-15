package update_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cubeship/internal/server/servertest"
	"cubeship/internal/update"
)

// writeRun puts a run on the fixture's disk, which is what an instance
// mid-update looks like from every process that can see the data
// directory.
func writeRun(t *testing.T, dir string, r *update.Run) {
	t.Helper()
	if err := (update.Store{DataDir: dir}).Write(r); err != nil {
		t.Fatal(err)
	}
}

// **Nothing can be changed while this instance is replacing itself**,
// and the lock is the server's rather than the screen's: a browser that
// reloads forgets everything it knew, and the moment worth protecting
// is exactly the one where the daemon has restarted underneath somebody.
func TestNothingChangesWhileTheInstanceIsUpdating(t *testing.T) {
	f := servertest.New(t)
	writeRun(t, f.DataDir, &update.Run{
		Version: "0.2.0", Status: update.StatusRunning,
		Step: "pulling the new images", StartedAt: time.Now(),
	})

	rec := f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "later"}, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusServiceUnavailable)
	if body := rec.Body.String(); body == "" {
		t.Error("it refused without saying why, which is a spinner with no explanation")
	}

	// Reading still works, and it has to: what somebody does during an
	// update is watch it, and refusing the request that shows the
	// progress would refuse the only useful thing left to do.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects", nil, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/updates", nil, f.AdminKey), http.StatusOK)
}

// A run that finished takes the lock off, and it stays on disk: a screen
// says what it just did.
func TestAFinishedRunDoesNotLockAnything(t *testing.T) {
	f := servertest.New(t)
	done := time.Now()
	writeRun(t, f.DataDir, &update.Run{
		Version: "0.2.0", Status: update.StatusDone,
		StartedAt: done.Add(-time.Minute), FinishedAt: &done,
	})
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects",
		map[string]any{"slug": "later"}, f.AdminKey), http.StatusCreated)
}

// **An instance locked for ever is worse than one that gives up.** The
// single way to leave a run behind is the updater dying between
// stopping the daemon and starting it, and nothing is coming to close
// that record — so age is what closes it.
func TestARunNothingIsComingForStopsLockingTheInstance(t *testing.T) {
	f := servertest.New(t)
	writeRun(t, f.DataDir, &update.Run{
		Version: "0.2.0", Status: update.StatusRunning,
		StartedAt: time.Now().Add(-update.StuckAfter - time.Hour),
	})
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects",
		map[string]any{"slug": "later"}, f.AdminKey), http.StatusCreated)
}

// A file nothing can read is no run: the alternative is an instance that
// refuses everything for ever because something wrote nonsense into it.
func TestNonsenseInTheStatusFileDoesNotLockTheInstance(t *testing.T) {
	f := servertest.New(t)
	if err := os.WriteFile(filepath.Join(f.DataDir, update.StatusFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects",
		map[string]any{"slug": "later"}, f.AdminKey), http.StatusCreated)
}

// A daemon built by hand has nothing to replace, and saying so beats
// killing somebody's own build and starting a container in its place.
func TestADaemonThatIsNotAContainerRefusesToUpdateItself(t *testing.T) {
	f := servertest.New(t)
	rec := f.Do(t, http.MethodPost, "/updates", map[string]any{"version": "9.9.9"}, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusConflict)
}
