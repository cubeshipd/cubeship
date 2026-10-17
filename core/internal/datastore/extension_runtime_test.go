package datastore

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// A database with no extensions runs nothing inside its container. The
// path exists for a handful of datastores and must cost the rest
// nothing — not even a `docker exec` per provision.
func TestNoExtensionsRunsNothingInTheContainer(t *testing.T) {
	docker := &execDocker{}
	p := quickProvisioner(docker)
	if err := p.installExtensions(context.Background(), withExtensions(), "id"); err != nil {
		t.Fatal(err)
	}
	if got := docker.commands(); len(got) != 0 {
		t.Errorf("it ran %v", got)
	}
}

// The order that matters: nothing is created until the engine answers on
// its own port. A CREATE EXTENSION sent to the temporary server the
// Postgres image runs during initdb is thrown away with it, and the
// datastore would come up reporting an extension it does not have.
func TestNothingIsCreatedBeforeTheDatabaseAnswers(t *testing.T) {
	docker := &execDocker{readyAfter: 2}
	p := quickProvisioner(docker)
	d := withExtensions(ExtensionPgvector, ExtensionVectorChord)
	if err := p.installExtensions(context.Background(), d, "id"); err != nil {
		t.Fatal(err)
	}

	cmds := docker.commands()
	var firstSQL, lastReady = -1, -1
	for i, c := range cmds {
		if c[0] == "pg_isready" {
			lastReady = i
		}
		if c[0] == "psql" && firstSQL < 0 {
			firstSQL = i
		}
	}
	if lastReady < 0 || firstSQL < 0 {
		t.Fatalf("expected both kinds of command: %v", cmds)
	}
	if lastReady > firstSQL {
		t.Errorf("it was still waiting after it had started creating things: %v", cmds)
	}
	// Over TCP, which is the whole point: the socket answers during
	// initdb and the port does not.
	if !slices.Contains(cmds[0], "127.0.0.1") {
		t.Errorf("readiness is not checked over the port: %v", cmds[0])
	}
}

func TestExtensionsAreCreatedInOrder(t *testing.T) {
	docker := &execDocker{}
	p := quickProvisioner(docker)
	d := withExtensions(ExtensionVectorChord, ExtensionPgvector)
	if err := p.installExtensions(context.Background(), d, "id"); err != nil {
		t.Fatal(err)
	}

	var sql []string
	for _, c := range docker.commands() {
		if c[0] == "psql" {
			sql = append(sql, c[len(c)-1])
		}
	}
	want := []string{
		`CREATE EXTENSION IF NOT EXISTS "vector"`,
		`CREATE EXTENSION IF NOT EXISTS vchord CASCADE`,
	}
	if !slices.Equal(sql, want) {
		t.Errorf("ran %v, want %v", sql, want)
	}
}

// Running it again is what happens on every `start`, and every time a
// port is published: both replace the container over a data directory
// where the extensions already exist.
func TestInstallingTwiceIsTheSameAsOnce(t *testing.T) {
	docker := &execDocker{}
	p := quickProvisioner(docker)
	d := withExtensions(ExtensionPgvector)
	for range 2 {
		if err := p.installExtensions(context.Background(), d, "id"); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range docker.commands() {
		if c[0] == "psql" && !strings.Contains(c[len(c)-1], "IF NOT EXISTS") {
			t.Errorf("a second pass would fail: %q", c[len(c)-1])
		}
	}
}

// psql has to stop on the first error, or a failed CREATE EXTENSION is
// printed and the command still exits 0 — which would report a database
// with extensions that has none.
func TestPsqlStopsOnTheFirstError(t *testing.T) {
	docker := &execDocker{}
	p := quickProvisioner(docker)
	if err := p.installExtensions(context.Background(), withExtensions(ExtensionPgvector), "id"); err != nil {
		t.Fatal(err)
	}
	for _, c := range docker.commands() {
		if c[0] != "psql" {
			continue
		}
		if !slices.Contains(c, "ON_ERROR_STOP=1") {
			t.Errorf("psql would carry on past an error: %v", c)
		}
	}
}

func TestAFailedExtensionIsAnError(t *testing.T) {
	docker := &execDocker{
		failSQL: `CREATE EXTENSION IF NOT EXISTS vchord CASCADE`,
		says:    `ERROR:  extension "vchord" is not available`,
	}
	p := quickProvisioner(docker)
	err := p.installExtensions(context.Background(),
		withExtensions(ExtensionPgvector, ExtensionVectorChord), "id")
	if err == nil {
		t.Fatal("a refused statement was not reported")
	}
	if !strings.Contains(err.Error(), "vectorchord") {
		t.Errorf("the error does not say which extension: %v", err)
	}
	if !strings.Contains(err.Error(), "is not available") {
		t.Errorf("the error does not carry what the engine said: %v", err)
	}
}

// A database that never comes up fails the provision rather than being
// called running with no extensions in it.
func TestADatabaseThatNeverAnswersIsAFailure(t *testing.T) {
	docker := &execDocker{readyAfter: 1000}
	p := quickProvisioner(docker)
	err := p.installExtensions(context.Background(), withExtensions(ExtensionPgvector), "id")
	if err == nil {
		t.Fatal("it reported success without ever connecting")
	}
	for _, c := range docker.commands() {
		if c[0] == "psql" {
			t.Fatalf("it ran a statement anyway: %v", c)
		}
	}
}

// **The password never leaves the row.** psql connects over the
// container's own socket, which the image trusts, so nothing here has a
// credential to leak into an argv, a log line or an error message.
func TestNothingRunOrReportedCarriesThePassword(t *testing.T) {
	const password = "s3cr3t-and-never-logged"
	docker := &execDocker{
		failSQL: `CREATE EXTENSION IF NOT EXISTS "vector"`,
		says:    `ERROR:  permission denied`,
	}
	p := quickProvisioner(docker)
	err := p.installExtensions(context.Background(), withExtensions(ExtensionPgvector), "id")
	if err == nil {
		t.Fatal("expected a failure to inspect")
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("the password is in the error: %v", err)
	}
	for _, c := range docker.commands() {
		for _, arg := range c {
			if strings.Contains(arg, password) {
				t.Errorf("the password is in argv: %v", c)
			}
			if arg == "env" || strings.HasPrefix(arg, "PGPASSWORD") {
				t.Errorf("a credential was put in the environment: %v", c)
			}
		}
	}
}

// --- what goes on the container ---

func TestTheContainerSaysWhichExtensionsItHas(t *testing.T) {
	p := NewProvisioner(nil, &execDocker{}, "")
	opts := p.containerOpts(context.Background(),
		withExtensions(ExtensionPgvector, ExtensionVectorChord))
	if got := opts.Labels["cubeship.extensions"]; got != "pgvector,vectorchord" {
		t.Errorf("label is %q", got)
	}
	for k, v := range opts.Labels {
		if strings.Contains(v, "s3cr3t") {
			t.Errorf("label %s carries the password", k)
		}
	}

	plain := p.containerOpts(context.Background(), withExtensions())
	if _, ok := plain.Labels["cubeship.extensions"]; ok {
		t.Error("a database with none carries an empty label")
	}
}

// --- the column ---

func TestExtensionsRoundTripThroughTheColumn(t *testing.T) {
	for _, in := range []Extensions{nil, {}, {ExtensionPgvector}, {ExtensionPgvector, ExtensionVectorChord}} {
		encoded, err := in.Value()
		if err != nil {
			t.Fatal(err)
		}
		text, ok := encoded.(string)
		if !ok {
			t.Fatalf("Value gave %T", encoded)
		}
		var out Extensions
		if err := out.Scan([]byte(text)); err != nil {
			t.Fatal(err)
		}
		if len(in) != len(out) || (len(in) > 0 && !slices.Equal(in, out)) {
			t.Errorf("%v came back as %v", in, out)
		}
	}
}

// A nil list is `[]`, not JSON null: the column is NOT NULL, and a
// reader that had to tell one from the other is a reader that gets it
// wrong once.
func TestANilListIsWrittenAsAnEmptyArray(t *testing.T) {
	v, err := Extensions(nil).Value()
	if err != nil {
		t.Fatal(err)
	}
	if v != "[]" {
		t.Errorf("nil became %q", v)
	}
}

// A row written before the column existed reads as no extensions rather
// than as an error that would take every read of the table with it.
func TestANullColumnIsNoExtensions(t *testing.T) {
	var out Extensions
	if err := out.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("NULL became %v", out)
	}
}

// Rendered for an API, never nil: a client should not have to tell `[]`
// from `null` to find out that a database has no extensions.
func TestStringsIsNeverNil(t *testing.T) {
	if got := Extensions(nil).Strings(); got == nil || len(got) != 0 {
		t.Errorf("got %#v", got)
	}
}

// The install order is the list order, so an extension has to appear
// after everything it needs — cube before earthdistance, pgvector before
// VectorChord. Adding one is then a decision about where it goes rather
// than a second table to keep in step.
func TestEveryExtensionComesAfterWhatItNeeds(t *testing.T) {
	for i, x := range extensionOrder {
		for _, dep := range x.Requires() {
			at := slices.Index(extensionOrder, dep)
			if at < 0 {
				t.Errorf("%s needs %s, which is not offered", x, dep)
				continue
			}
			if at > i {
				t.Errorf("%s is created before %s, which it needs", x, dep)
			}
		}
	}
}

// Every extension has a spec, and every spec is in the order — two
// halves of one table, and a name in one and not the other is a name
// that reaches a CREATE EXTENSION with an empty argument.
func TestTheExtensionTableIsWholeAndUnique(t *testing.T) {
	seen := map[Extension]bool{}
	for _, x := range extensionOrder {
		if seen[x] {
			t.Errorf("%s is listed twice", x)
		}
		seen[x] = true
		if extensionSpecs[x].sqlName == "" {
			t.Errorf("%s has no SQL name", x)
		}
		if extensionSpecs[x].summary == "" {
			t.Errorf("%s has nothing to show beside it", x)
		}
	}
	for x := range extensionSpecs {
		if !seen[x] {
			t.Errorf("%s has a spec and is offered nowhere", x)
		}
	}
}

// Every statement is `CREATE EXTENSION IF NOT EXISTS` and nothing else.
// The quoting matters for one of them — `uuid-ossp` is not a bare
// identifier — and quoting all of them is the rule with no exception to
// forget.
func TestEveryStatementIsTheSameShape(t *testing.T) {
	for _, x := range extensionOrder {
		sql := x.install()
		if !strings.HasPrefix(sql, "CREATE EXTENSION IF NOT EXISTS ") {
			t.Errorf("%s runs %q", x, sql)
		}
		if strings.Count(sql, ";") != 0 {
			t.Errorf("%s runs more than one statement: %q", x, sql)
		}
	}
	if got := ExtensionUUIDOSSP.install(); got != `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"` {
		t.Errorf("a name with a dash is not quoted: %q", got)
	}
}

// The contrib modules are in the image already, so a database that has
// only those runs the plain image and needs no command — which is what
// makes installing one cost no downtime.
func TestContribExtensionsChangeNeitherImageNorCommand(t *testing.T) {
	var contrib Extensions
	for _, x := range AllExtensions() {
		if x.Builtin() {
			contrib = append(contrib, x)
		}
	}
	if len(contrib) < 10 {
		t.Fatalf("only %d extensions come with the image", len(contrib))
	}
	plain := &Datastore{Engine: EnginePostgres, Version: "16"}
	loaded := &Datastore{Engine: EnginePostgres, Version: "16", Extensions: contrib}
	if plain.Image() != loaded.Image() {
		t.Errorf("contrib changed the image: %q then %q", plain.Image(), loaded.Image())
	}
	if loaded.ContainerCmd() != nil {
		t.Errorf("contrib took a command: %v", loaded.ContainerCmd())
	}
	if NeedsReplacement(plain, loaded) {
		t.Error("adding contrib modules would replace the container")
	}
}

// The two that do replace it, and why each one does: one needs a
// different image, the other needs its library preloaded.
func TestWhatReplacesTheContainer(t *testing.T) {
	plain := &Datastore{Engine: EnginePostgres, Version: "16"}
	for _, x := range []Extension{ExtensionPgvector, ExtensionVectorChord, ExtensionPgStatStatements} {
		if x.Builtin() {
			t.Errorf("%s says it needs nothing", x)
		}
		want, err := NormalizeExtensions(EnginePostgres, "16", []string{string(x)})
		if err != nil {
			t.Fatal(err)
		}
		after := &Datastore{Engine: EnginePostgres, Version: "16", Extensions: want}
		if !NeedsReplacement(plain, after) {
			t.Errorf("%s was added without replacing the container", x)
		}
	}
	// pg_stat_statements is the one that changes only the command.
	stats := &Datastore{Engine: EnginePostgres, Version: "16",
		Extensions: Extensions{ExtensionPgStatStatements}}
	if stats.Image() != plain.Image() {
		t.Errorf("pg_stat_statements changed the image: %q", stats.Image())
	}
	if !slices.Contains(stats.ContainerCmd(), "shared_preload_libraries=pg_stat_statements") {
		t.Errorf("its library is not preloaded: %v", stats.ContainerCmd())
	}
}

// Two preloaded libraries end up in one setting, in a fixed order.
func TestPreloadedLibrariesAreCombined(t *testing.T) {
	d := &Datastore{Engine: EnginePostgres, Version: "16",
		Extensions: Extensions{ExtensionPgStatStatements, ExtensionPgvector, ExtensionVectorChord}}
	cmd := d.ContainerCmd()
	if len(cmd) != 3 {
		t.Fatalf("command is %v", cmd)
	}
	for _, lib := range []string{"vchord", "pg_stat_statements"} {
		if !strings.Contains(cmd[2], lib) {
			t.Errorf("%s is not preloaded: %q", lib, cmd[2])
		}
	}
}

// earthdistance is built on cube, so asking for it asks for both — the
// same rule VectorChord and pgvector follow, on a pair nobody thinks of.
func TestEarthdistanceBringsCube(t *testing.T) {
	got, err := NormalizeExtensions(EnginePostgres, "16", []string{"earthdistance"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, Extensions{ExtensionCube, ExtensionEarthdistance}) {
		t.Fatalf("got %v", got)
	}
	// And in that order, because cube has to exist first.
	plan := (&Datastore{Engine: EnginePostgres, Version: "16", Extensions: got}).InstallPlan()
	if plan[0].Extension != ExtensionCube {
		t.Errorf("plan is %v", plan)
	}
}

// Postgres takes its dynamic shared memory from /dev/shm, and the
// Engine's 64 MiB default is too little for a large parallel query: it
// fails as "could not resize shared memory segment", on a database with
// nothing else wrong with it. Every Postgres compose file raises it.
func TestPostgresGetsEnoughSharedMemory(t *testing.T) {
	p := NewProvisioner(nil, &execDocker{}, "")
	pg := p.containerOpts(context.Background(), withExtensions())
	if pg.ShmSize != PostgresShmSize {
		t.Errorf("/dev/shm is %d, want %d", pg.ShmSize, PostgresShmSize)
	}
	// And only Postgres: a tmpfs per container is not free on a box this
	// size, and nothing else here uses /dev/shm.
	redis := &Datastore{ID: 2, Slug: "cache", Engine: EngineRedis, Version: "7.4"}
	if got := p.containerOpts(context.Background(), redis).ShmSize; got != 0 {
		t.Errorf("redis asked for %d of /dev/shm", got)
	}
}
