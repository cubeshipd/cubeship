package datastore

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/platform/dockerx"
)

// --- the matrix itself ---

// Every image in the support matrix is pinned by digest, because an
// unpinned tag is somebody else deciding which bytes this instance runs
// under a database that already has data in it.
func TestEveryExtensionImageIsPinned(t *testing.T) {
	for version, builds := range postgresBuilds {
		for _, b := range builds {
			if b.image == "" || b.tag == "" {
				t.Errorf("%s: a build with no reference: %+v", version, b)
			}
			if !strings.HasPrefix(b.digest, "sha256:") || len(b.digest) != 71 {
				t.Errorf("%s %s: digest %q is not a sha256", version, b.tag, b.digest)
			}
		}
	}
}

// A version in the matrix that the engine does not offer is a support
// promise nothing can take up, and — worse — a version *removed* from
// the engine leaves a row here saying it still works.
func TestTheMatrixOnlyCoversVersionsPostgresOffers(t *testing.T) {
	for version := range postgresBuilds {
		if !EnginePostgres.KnowsVersion(version) {
			t.Errorf("the matrix has postgres %s, which this release does not offer", version)
		}
	}
}

// Every build's `provides` has to be closed under `requires`: an image
// carrying VectorChord without pgvector would be picked for a request
// that then fails on a CREATE EXTENSION nothing can satisfy.
func TestEveryBuildCarriesWhatItsExtensionsNeed(t *testing.T) {
	for version, builds := range postgresBuilds {
		for _, b := range builds {
			for _, x := range b.provides {
				for _, dep := range x.Requires() {
					if !slices.Contains(b.provides, dep) {
						t.Errorf("%s %s provides %s without %s", version, b.tag, x, dep)
					}
				}
			}
		}
	}
}

// Builds are searched in order and the first that covers a request wins,
// so a build providing more than another has to come after it — or
// asking for pgvector alone would pull the larger image.
func TestBuildsAreOrderedFewestFirst(t *testing.T) {
	for version, builds := range postgresBuilds {
		for i := 1; i < len(builds); i++ {
			if len(builds[i].provides) < len(builds[i-1].provides) {
				t.Errorf("%s: %s comes after a build that provides more", version, builds[i].tag)
			}
		}
	}
}

// --- normalizing ---

func TestNormalizeExtensions(t *testing.T) {
	cases := []struct {
		name    string
		engine  Engine
		version string
		in      []string
		want    Extensions
		err     error
	}{{
		name: "nothing asked for is nothing stored",
		// And it is the case that matters most: it is every database
		// that existed before this feature, and it keeps the plain image.
		engine: EnginePostgres, version: "16", in: nil, want: nil,
	}, {
		name:   "an empty list is not an engine check either",
		engine: EngineRedis, version: "7.4", in: []string{}, want: nil,
	}, {
		name:   "duplicates collapse",
		engine: EnginePostgres, version: "16",
		in:   []string{"pgvector", "pgvector"},
		want: Extensions{ExtensionPgvector},
	}, {
		name:   "the order asked for does not matter",
		engine: EnginePostgres, version: "16",
		in:   []string{"vectorchord", "pgvector"},
		want: Extensions{ExtensionPgvector, ExtensionVectorChord},
	}, {
		name: "vectorchord brings pgvector",
		// Added rather than demanded: there is one right answer, so
		// Cubeship gives it instead of refusing the request.
		engine: EnginePostgres, version: "17",
		in:   []string{"vectorchord"},
		want: Extensions{ExtensionPgvector, ExtensionVectorChord},
	}, {
		name:   "case and surrounding space are forgiven",
		engine: EnginePostgres, version: "18",
		in:   []string{" PgVector "},
		want: Extensions{ExtensionPgvector},
	}, {
		name:   "a name nobody reviewed is refused",
		engine: EnginePostgres, version: "16",
		in:  []string{"postgis"},
		err: ErrUnknownExtension,
	}, {
		name: "an image reference is a name, and not one we know",
		// The whole security model in one case: nothing about a request
		// reaches a repository, a tag or a statement.
		engine: EnginePostgres, version: "16",
		in:  []string{"ghcr.io/evil/pg:latest"},
		err: ErrUnknownExtension,
	}, {
		name:   "a SQL statement is not a name either",
		engine: EnginePostgres, version: "16",
		in:  []string{"vector; DROP TABLE users"},
		err: ErrUnknownExtension,
	}, {
		name:   "the SQL name is not the name to ask by",
		engine: EnginePostgres, version: "16",
		in:  []string{"vector"},
		err: ErrUnknownExtension,
	}, {
		name:   "no engine but postgres has any",
		engine: EngineMySQL, version: "8.4",
		in:  []string{"pgvector"},
		err: ErrExtensionsUnsupported,
	}, {
		name:   "nor redis, which is not even relational",
		engine: EngineRedis, version: "7.4",
		in:  []string{"pgvector"},
		err: ErrExtensionsUnsupported,
	}, {
		name: "a version with no image offers nothing",
		// There is no such version today. The case is here because the
		// answer has to be a refusal rather than a datastore that comes
		// up on an image nobody picked.
		engine: EnginePostgres, version: "14",
		in:  []string{"pgvector"},
		err: ErrExtensionVersion,
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeExtensions(c.engine, c.version, c.in)
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("got %v, want %v", err, c.err)
				}
				if got != nil {
					t.Errorf("a refusal returned %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// Normalizing twice has to give the same answer, because the stored list
// is normalized and is normalized again every time it is read back with
// something added to it.
func TestNormalizingIsIdempotent(t *testing.T) {
	once, err := NormalizeExtensions(EnginePostgres, "16", []string{"vectorchord"})
	if err != nil {
		t.Fatal(err)
	}
	twice, err := NormalizeExtensions(EnginePostgres, "16", once.Strings())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(once, twice) {
		t.Errorf("%v then %v", once, twice)
	}
}

// A refused request names what it refused, so somebody can fix it
// without reading this file.
func TestRefusalsSayWhatIsOffered(t *testing.T) {
	_, err := NormalizeExtensions(EnginePostgres, "16", []string{"postgis"})
	if err == nil || !strings.Contains(err.Error(), "pgvector") {
		t.Errorf("an unknown extension should name the ones that exist: %v", err)
	}
	_, err = NormalizeExtensions(EngineMySQL, "8.4", []string{"pgvector"})
	if err == nil || !strings.Contains(err.Error(), "mysql") {
		t.Errorf("a wrong engine should name it: %v", err)
	}
}

func TestSupportedExtensions(t *testing.T) {
	// Every version this release runs has an image for both of the
	// extensions that need one, so every version offers the lot.
	for _, v := range EnginePostgres.Versions() {
		got := SupportedExtensions(EnginePostgres, v)
		if !slices.Equal(got, AllExtensions()) {
			t.Errorf("postgres %s offers %v, want %v", v, got, AllExtensions())
		}
	}
	// A version this release does not run offers nothing, whatever the
	// matrix happens to hold.
	if got := SupportedExtensions(EnginePostgres, "14"); len(got) != 0 {
		t.Errorf("postgres 14 offers %v", got)
	}
	for _, e := range Engines() {
		if e == EnginePostgres {
			continue
		}
		for _, v := range e.Versions() {
			if got := SupportedExtensions(e, v); len(got) != 0 {
				t.Errorf("%s %s offers %v", e, v, got)
			}
		}
	}
}

// --- what the container runs ---

// A datastore with no extensions runs exactly what it ran before this
// existed. Every database on every instance that upgrades is this case,
// and an image that changed under one of them would be a container that
// has to re-read a data directory it did not write.
func TestNoExtensionsKeepsThePlainImage(t *testing.T) {
	for _, e := range Engines() {
		for _, v := range e.Versions() {
			d := &Datastore{Engine: e, Version: v}
			if got, want := d.Image(), string(e)+":"+v; got != want {
				// mongodb's image is `mongo`, so compare through the
				// engine rather than the name for that one.
				if got != e.Image(v) {
					t.Errorf("%s %s runs %q, want %q", e, v, got, want)
				}
			}
			if cmd := d.ContainerCmd(); e == EnginePostgres && cmd != nil {
				t.Errorf("postgres %s took a command: %v", v, cmd)
			}
		}
	}
}

func TestTheImageFollowsTheExtensions(t *testing.T) {
	cases := []struct {
		version string
		want    []Extension
		image   string
		preload string
	}{
		{"16", []Extension{ExtensionPgvector}, "pgvector/pgvector:0.8.6-pg16", ""},
		{"18", []Extension{ExtensionPgvector}, "pgvector/pgvector:0.8.6-pg18", ""},
		{"16", []Extension{ExtensionPgvector, ExtensionVectorChord},
			"ghcr.io/tensorchord/vchord-postgres:pg16-v1.1.1", "vchord,vector"},
		{"17", []Extension{ExtensionPgvector, ExtensionVectorChord},
			"ghcr.io/tensorchord/vchord-postgres:pg17-v1.1.1", "vchord,vector"},
	}
	for _, c := range cases {
		d := &Datastore{Engine: EnginePostgres, Version: c.version, Extensions: c.want}
		image := d.Image()
		if !strings.HasPrefix(image, c.image+"@sha256:") {
			t.Errorf("postgres %s with %v runs %q, want %s pinned", c.version, c.want, image, c.image)
		}
		cmd := d.ContainerCmd()
		if c.preload == "" {
			if cmd != nil {
				t.Errorf("postgres %s with %v took a command: %v", c.version, c.want, cmd)
			}
			continue
		}
		want := []string{"postgres", "-c", "shared_preload_libraries=" + c.preload}
		if !slices.Equal(cmd, want) {
			t.Errorf("postgres %s with %v runs %v, want %v", c.version, c.want, cmd, want)
		}
	}
}

// pgvector alone must not pull the VectorChord image. It is a bigger
// image and a wider surface, and the reason the matrix is searched
// fewest-first.
func TestPgvectorAloneDoesNotTakeTheVectorChordImage(t *testing.T) {
	d := &Datastore{Engine: EnginePostgres, Version: "16", Extensions: Extensions{ExtensionPgvector}}
	if strings.Contains(d.Image(), "vchord") {
		t.Errorf("pgvector alone runs %q", d.Image())
	}
}

// The data path is the mount and PGDATA whatever image is running. An
// extension build that moved it would put somebody's database in an
// anonymous volume — the failure TestPostgresSaysWhereItsDataGoes exists
// for, reached the other way.
func TestExtensionBuildsKeepTheDataWhereItWas(t *testing.T) {
	for _, version := range EnginePostgres.Versions() {
		plain := &Datastore{Engine: EnginePostgres, Version: version}
		withExt := &Datastore{Engine: EnginePostgres, Version: version,
			Extensions: Extensions{ExtensionPgvector, ExtensionVectorChord}}
		if plain.DataPath() != withExt.DataPath() {
			t.Errorf("%s: %q vs %q", version, plain.DataPath(), withExt.DataPath())
		}
		if !slices.Contains(withExt.ContainerEnv(), "PGDATA="+postgresDataPath) {
			t.Errorf("%s: an extension build is not told where its data goes: %v",
				version, withExt.ContainerEnv())
		}
	}
}

func TestTheInstallPlanIsOrderedAndFixed(t *testing.T) {
	// Asked for in the other order on purpose: the plan's order is the
	// matrix's, not the list's.
	d := &Datastore{Engine: EnginePostgres, Version: "16",
		Extensions: Extensions{ExtensionVectorChord, ExtensionPgvector}}
	plan := d.InstallPlan()
	if len(plan) != 2 {
		t.Fatalf("plan is %v", plan)
	}
	if plan[0].Extension != ExtensionPgvector || plan[1].Extension != ExtensionVectorChord {
		t.Fatalf("pgvector has to be created first: %v", plan)
	}
	if plan[0].SQL != `CREATE EXTENSION IF NOT EXISTS "vector"` {
		t.Errorf("pgvector runs %q", plan[0].SQL)
	}
	if plan[1].SQL != `CREATE EXTENSION IF NOT EXISTS vchord CASCADE` {
		t.Errorf("vectorchord runs %q", plan[1].SQL)
	}
	for _, step := range plan {
		if !strings.Contains(step.SQL, "IF NOT EXISTS") {
			t.Errorf("%s is not idempotent: %q", step.Extension, step.SQL)
		}
	}
}

// --- installing ---

// execDocker records what was run inside the container and answers as
// the test tells it to. Only the calls installExtensions makes are
// implemented; the rest of DockerAPI is here to satisfy the interface.
type execDocker struct {
	mu   sync.Mutex
	runs [][]string
	// readyAfter is how many pg_isready calls fail before one succeeds.
	readyAfter int
	// failSQL is a statement to refuse, and what psql says about it.
	failSQL, says string
}

func (d *execDocker) commands() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([][]string(nil), d.runs...)
}

func (d *execDocker) ExecStream(_ context.Context, _ string, cmd []string, _ io.Reader, out io.Writer) (string, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.runs = append(d.runs, append([]string(nil), cmd...))
	if cmd[0] == "pg_isready" {
		if d.readyAfter > 0 {
			d.readyAfter--
			return "no response", 2, nil
		}
		return "", 0, nil
	}
	if d.failSQL != "" && slices.Contains(cmd, d.failSQL) {
		if out != nil {
			_, _ = io.WriteString(out, d.says)
		}
		return "", 1, nil
	}
	return "", 0, nil
}

func (d *execDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return nil }
func (d *execDocker) CreateContainer(context.Context, dockerx.ContainerOpts) (string, error) {
	return "id", nil
}
func (d *execDocker) StartContainer(context.Context, string) error  { return nil }
func (d *execDocker) StopContainer(context.Context, string) error   { return nil }
func (d *execDocker) RemoveContainer(context.Context, string) error { return nil }
func (d *execDocker) SetResources(context.Context, string, dockerx.Resources) error {
	return nil
}
func (d *execDocker) IsRunning(context.Context, string) (bool, error) { return true, nil }
func (d *execDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func quickProvisioner(docker DockerAPI) *Provisioner {
	p := NewProvisioner(nil, docker, "")
	p.ExtensionInterval = time.Millisecond
	p.ExtensionAttempts = 5
	return p
}

func withExtensions(x ...Extension) *Datastore {
	return &Datastore{
		ID: 1, Slug: "vec", Engine: EnginePostgres, Version: "16",
		Username: "cubeship", Password: "s3cr3t-and-never-logged", Database: "vec",
		Extensions: x,
	}
}
