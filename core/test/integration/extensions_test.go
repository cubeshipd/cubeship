//go:build integration

// Postgres extensions against a real engine.
//
// Everything below this file proves that Cubeship *asks* for the right
// image and runs the right statement. None of it proves the statement
// works — that the image actually carries the library, that VectorChord
// loads when it is preloaded, that a vector column survives the
// container being replaced. Those are different questions, and the
// difference is the one `TestTraefikAcceptsABalancedRoute` was written
// for: rendering a document and reading the string back says nothing
// about whether the thing on the other side accepts it.
//
// It boots containers, so it is behind the build tag and runs on the
// Linux/Docker step in CI rather than in `make check`.

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"cubeship/internal/datastore"
)

// TestTheExtensionImageRunsWhatCubeshipAsksOfIt takes the image and the
// statements this release would choose for a Postgres with pgvector and
// VectorChord, runs them, and then does the thing that actually breaks:
// destroys the container and creates it again over the same directory.
func TestTheExtensionImageRunsWhatCubeshipAsksOfIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// The version and the extensions Cubeship would use for an Immich:
	// everything below is read from the daemon's own tables, so a change
	// to the matrix is tested rather than described.
	want, err := datastore.NormalizeExtensions(datastore.EnginePostgres, "16",
		[]string{"vectorchord"})
	if err != nil {
		t.Fatal(err)
	}
	d := &datastore.Datastore{
		Slug: "itest", Engine: datastore.EnginePostgres, Version: "16",
		Username: "cubeship", Password: "integration-password", Database: "vec",
		Extensions: want,
	}

	// The daemon creates it 0700 and root-owned, and the image chowns it
	// to the postgres user on the way past. Reproduced here because it is
	// half of what makes a Postgres on a bind mount work at all — and it
	// is why the directory has to be emptied from inside a container
	// afterwards: the test process cannot read what postgres now owns.
	dir := dataDir(t)

	name := "cs-ext-" + fmt.Sprint(time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	run := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	start := func() {
		t.Helper()
		args := []string{"run", "-d", "--name", name,
			"-v", dir + ":" + d.DataPath(),
		}
		for _, env := range d.ContainerEnv() {
			args = append(args, "-e", env)
		}
		args = append(args, d.Image())
		args = append(args, d.ContainerCmd()...)
		run(args...)
	}

	// waitAccepting is the provisioner's rule, and it is the point of
	// this half of the test: the image starts a temporary server on the
	// socket while it initializes, so anything sent before the port
	// answers is thrown away with it.
	waitAccepting := func() {
		t.Helper()
		for attempt := range 120 {
			out, err := exec.CommandContext(ctx, "docker", "exec", name,
				"pg_isready", "-h", "127.0.0.1", "-p", "5432", "-U", d.Username, "-q").CombinedOutput()
			if err == nil {
				return
			}
			if attempt == 119 {
				logs, _ := exec.CommandContext(ctx, "docker", "logs", name).CombinedOutput()
				t.Fatalf("it never accepted connections: %s\n%s", out, logs)
			}
			time.Sleep(time.Second)
		}
	}

	// psql over the container's own socket, with no password anywhere —
	// exactly what the provisioner does.
	psql := func(sql string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", "exec", name,
			"psql", "--no-psqlrc", "-v", "ON_ERROR_STOP=1", "-tA",
			"-h", "/var/run/postgresql", "-U", d.Username, "-d", d.Database,
			"-c", sql).CombinedOutput()
		if err != nil {
			t.Fatalf("psql %q: %v\n%s", sql, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	install := func() {
		t.Helper()
		for _, step := range d.InstallPlan() {
			psql(step.SQL)
		}
	}

	start()
	waitAccepting()
	install()

	// In pg_extension, by their SQL names — which are not the names
	// anybody asked by.
	for _, x := range d.Extensions {
		if got := psql(`SELECT extname FROM pg_extension WHERE extname = '` + x.SQLName() + `'`); got != x.SQLName() {
			t.Fatalf("%s was asked for and %q is in pg_extension", x, got)
		}
	}

	// A vector column, an index over it and a query that uses the
	// operator: the smallest thing that fails if the library is present
	// but not loaded.
	psql(`CREATE TABLE items (id int primary key, embedding vector(3))`)
	psql(`INSERT INTO items VALUES (1, '[1,2,3]'), (2, '[4,5,6]')`)
	psql(`CREATE INDEX ON items USING vchordrq (embedding vector_l2_ops) WITH (options = $$residual_quantization = false$$)`)
	if got := psql(`SELECT id FROM items ORDER BY embedding <-> '[3,1,2]' LIMIT 1`); got != "1" {
		t.Fatalf("nearest neighbour is %q, want 1", got)
	}

	// **The part nothing else can test.** Publishing a port replaces the
	// container and `start` recreates it, both over this same directory.
	// A container that could not read what the last one wrote — or an
	// extension that has to be created again by hand — is the failure
	// that would only ever appear on somebody's instance.
	run("rm", "-f", name)
	start()
	waitAccepting()

	if got := psql(`SELECT count(*) FROM items`); got != "2" {
		t.Fatalf("after a recreate the table has %s rows, want 2", got)
	}
	for _, x := range d.Extensions {
		if got := psql(`SELECT extname FROM pg_extension WHERE extname = '` + x.SQLName() + `'`); got != x.SQLName() {
			t.Fatalf("after a recreate %s is gone", x)
		}
	}
	// And running the plan again is a no-op rather than a failure, which
	// is what every provision after the first one does.
	install()
	if got := psql(`SELECT id FROM items ORDER BY embedding <-> '[3,1,2]' LIMIT 1`); got != "1" {
		t.Fatalf("nearest neighbour is %q after reinstalling, want 1", got)
	}

	// The directory is the instance's, not an anonymous volume: the files
	// are in the bind mount, which is what puts them in a backup of the
	// data directory. Read from inside the container, because postgres
	// owns them now and this process does not.
	if out := run("exec", name, "sh", "-c", "ls "+d.DataPath()+"/base | wc -l"); out == "0" {
		t.Fatal("the data is not in the bind mount")
	}
}

// dataDir is a directory for a Postgres bind mount, emptied from inside
// a container when the test ends.
//
// The engine chowns the mount point and everything under it to its own
// unprivileged user, so the test process can neither list it nor delete
// what is in it — TempDir's own cleanup fails with "permission denied"
// and fails the test with it. The mode goes back as well as the
// contents, because removing a directory needs to open it first.
//
// Registered after TempDir, so it runs before TempDir's cleanup:
// t.Cleanup is LIFO.
func dataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		out, err := exec.Command("docker", "run", "--rm", "-v", dir+":/data",
			"busybox:1.37", "sh", "-c",
			"rm -rf /data/* /data/..?* /data/.[!.]* ; chmod 0777 /data").CombinedOutput()
		if err != nil {
			t.Logf("emptying the data directory: %v\n%s", err, out)
		}
	})
	return dir
}

// TestEveryExtensionCubeshipOffersCanActuallyBeCreated is the other
// half: the long list is contrib modules that come with the image, and
// "comes with the image" is an assumption about somebody else's build.
// This creates every one of them on the plain image and fails naming the
// one that is not there.
func TestEveryExtensionCubeshipOffersCanActuallyBeCreated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	for _, version := range datastore.EnginePostgres.Versions() {
		t.Run(version, func(t *testing.T) {
			offered := datastore.SupportedExtensions(datastore.EnginePostgres, version)
			want, err := datastore.NormalizeExtensions(datastore.EnginePostgres, version,
				func() []string {
					var names []string
					for _, x := range offered {
						names = append(names, string(x))
					}
					return names
				}())
			if err != nil {
				t.Fatal(err)
			}
			d := &datastore.Datastore{
				Slug: "all", Engine: datastore.EnginePostgres, Version: version,
				Username: "cubeship", Password: "integration-password", Database: "all",
				Extensions: want,
			}

			dir := dataDir(t)
			name := "cs-ext-all-" + version + "-" + fmt.Sprint(time.Now().UnixNano())
			t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

			args := []string{"run", "-d", "--name", name, "-v", dir + ":" + d.DataPath()}
			for _, env := range d.ContainerEnv() {
				args = append(args, "-e", env)
			}
			args = append(args, d.Image())
			args = append(args, d.ContainerCmd()...)
			if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
				t.Fatalf("start: %v\n%s", err, out)
			}

			for attempt := range 120 {
				if err := exec.CommandContext(ctx, "docker", "exec", name,
					"pg_isready", "-h", "127.0.0.1", "-p", "5432", "-U", d.Username, "-q").Run(); err == nil {
					break
				}
				if attempt == 119 {
					logs, _ := exec.CommandContext(ctx, "docker", "logs", name).CombinedOutput()
					t.Fatalf("it never accepted connections\n%s", logs)
				}
				time.Sleep(time.Second)
			}

			for _, step := range d.InstallPlan() {
				out, err := exec.CommandContext(ctx, "docker", "exec", name,
					"psql", "--no-psqlrc", "-v", "ON_ERROR_STOP=1",
					"-h", "/var/run/postgresql", "-U", d.Username, "-d", d.Database,
					"-c", step.SQL).CombinedOutput()
				if err != nil {
					t.Errorf("%s on postgres %s: %v\n%s", step.Extension, version, err, out)
				}
			}
		})
	}
}
