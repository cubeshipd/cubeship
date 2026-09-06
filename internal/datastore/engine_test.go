package datastore

import (
	"net/url"
	"strings"
	"testing"
)

// Every engine offered has to be one the daemon can actually run.
// Without this, adding a name to Engines() would compile, appear in the
// dashboard's dropdown and produce a datastore whose container has no
// image, no port and no way to be told a password.
func TestEveryEngineHasASpec(t *testing.T) {
	for _, e := range Engines() {
		sp, ok := specs[e]
		if !ok {
			t.Errorf("%s is offered but has no spec", e)
			continue
		}
		if sp.image == "" || sp.port == 0 || sp.dataPath == "" || sp.scheme == "" || sp.stem == "" {
			t.Errorf("%s has an incomplete spec: %+v", e, sp)
		}
		if len(sp.versions) == 0 {
			t.Errorf("%s offers no versions, so nothing can be created with it", e)
		}
		if sp.env == nil {
			t.Errorf("%s cannot be told what login to create", e)
		}
		if !e.Valid() {
			t.Errorf("%s is offered but Valid says no", e)
		}
		if got := e.DefaultVersion(); !e.KnowsVersion(got) {
			t.Errorf("%s defaults to %q, which it does not offer", e, got)
		}
	}

	// And nothing runnable is left out of the list a client is shown.
	for e := range specs {
		found := false
		for _, offered := range Engines() {
			if offered == e {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has a spec but is not offered, so nobody can create one", e)
		}
	}
}

// Each engine has to be told the login in its own image's vocabulary. A
// spec that spelled it wrong would produce a database with a password
// nobody holds — the container comes up, and every connection is
// refused.
func TestEachEngineIsToldItsLogin(t *testing.T) {
	for _, e := range Engines() {
		d := &Datastore{Engine: e, Username: "app", Password: "s3cret", Database: "app_db"}
		env := strings.Join(d.ContainerEnv(), "\n")
		for _, want := range []string{"app", "s3cret"} {
			if !strings.Contains(env, want) {
				t.Errorf("%s: the container is never told %q:\n%s", e, want, env)
			}
		}
		if e.HasDatabase() && !strings.Contains(env, "app_db") {
			t.Errorf("%s: the container is never told which database to create:\n%s", e, env)
		}
	}
}

// MySQL and MariaDB refuse to create root through their environment —
// it already exists. Catching it here is the difference between a
// sentence under the field and a container that exits with the reason
// in a log nobody is reading.
func TestRootIsRefusedWhereTheEngineWouldRefuseIt(t *testing.T) {
	for _, e := range []Engine{EngineMySQL, EngineMariaDB} {
		if err := CheckUsername(e, "root"); err == nil {
			t.Errorf("%s accepted root, which its image will not create", e)
		}
		if err := CheckUsername(e, "ROOT"); err == nil {
			t.Errorf("%s accepted ROOT: MySQL user names are not case-sensitive here", e)
		}
	}
	// Postgres has no such rule, and inventing one would refuse a login
	// that works.
	if err := CheckUsername(EnginePostgres, "root"); err != nil {
		t.Errorf("postgres refused root, which it would happily create: %v", err)
	}
}

// A login is handed to the image as an environment variable and used by
// it verbatim in SQL, so the shape check is the only thing between a
// creative username and whatever that turns into.
func TestALoginHasToBeAnIdentifier(t *testing.T) {
	for _, bad := range []string{"", "1app", "app-name", "app name", "app;DROP", "app'", strings.Repeat("a", 64)} {
		if err := CheckUsername(EnginePostgres, bad); err == nil {
			t.Errorf("%q was accepted as a username", bad)
		}
	}
	for _, ok := range []string{"cubeship", "app_1", "_private", "A"} {
		if err := CheckUsername(EnginePostgres, ok); err != nil {
			t.Errorf("%q was refused as a username: %v", ok, err)
		}
	}
}

// A slug is kebab-case because it is a path component; a database name
// is not, and an unquoted dash is a subtraction.
func TestADatabaseNameComesFromTheSlugWithoutItsDashes(t *testing.T) {
	if got := DefaultDatabaseName("public-api"); got != "public_api" {
		t.Errorf("DefaultDatabaseName(public-api) = %q, want public_api", got)
	}
	if err := CheckDatabaseName(DefaultDatabaseName("public-api")); err != nil {
		t.Errorf("the name we derive is one we then refuse: %v", err)
	}
	// A slug may start with a digit; an identifier may not.
	if err := CheckDatabaseName(DefaultDatabaseName("2fa")); err != nil {
		t.Errorf("a slug starting with a digit produced an illegal identifier: %v", err)
	}
}

// The password goes into a URL. Concatenating it would make a password
// containing "@" produce a URL that parses as a different host — which
// is the kind of thing that works in every test until somebody picks a
// password with a symbol in it.
func TestAChosenPasswordSurvivesTheConnectionURL(t *testing.T) {
	d := &Datastore{
		Engine: EnginePostgres, Username: "app",
		Password: "p@ss:w/rd?#", Database: "app_db",
	}
	raw := d.URI("db-host", 5432)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("the URI we build does not parse: %v (%s)", err, raw)
	}
	if u.Host != "db-host:5432" {
		t.Errorf("host is %q, so the password escaped into it: %s", u.Host, raw)
	}
	got, _ := u.User.Password()
	if got != "p@ss:w/rd?#" {
		t.Errorf("password came back as %q, want the one we put in: %s", got, raw)
	}
	if u.Path != "/app_db" {
		t.Errorf("database is %q, want /app_db: %s", u.Path, raw)
	}
}

// The variables are the whole of how an app reaches a database, so
// both halves have to be there: the one string an ORM takes, and the
// five fields a driver configured by hand takes.
func TestAnAttachedAppGetsTheURLAndItsParts(t *testing.T) {
	d := &Datastore{
		Engine: EnginePostgres, Username: "app", Password: "secret", Database: "app_db",
	}
	vars := d.Vars("", "cubeship-db-web-production-pg", 5432)

	for _, key := range []string{
		"DATABASE_URL", "DATABASE_HOST", "DATABASE_PORT",
		"DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_NAME",
	} {
		if vars[key] == "" {
			t.Errorf("%s is missing or empty: %v", key, vars)
		}
	}
	if vars["DATABASE_HOST"] != "cubeship-db-web-production-pg" {
		t.Errorf("DATABASE_HOST is %q, but the host is the container's name on the shared network", vars["DATABASE_HOST"])
	}

	// A prefix moves every one of them together. A prefix on some but
	// not all would be two half-configured databases.
	prefixed := d.Vars("ANALYTICS_", "other-host", 5432)
	for key := range prefixed {
		if !strings.HasPrefix(key, "ANALYTICS_") {
			t.Errorf("%q escaped the prefix", key)
		}
	}
	if len(prefixed) != len(vars) {
		t.Errorf("a prefix changed how many variables there are: %d vs %d", len(prefixed), len(vars))
	}
}

// A prefix is the front half of a variable name. One that is not leaves
// an app with a variable nothing can read.
func TestAPrefixHasToMakeALegalVariableName(t *testing.T) {
	if err := CheckPrefix(""); err != nil {
		t.Errorf("the empty prefix, which is the usual case, was refused: %v", err)
	}
	for _, ok := range []string{"ANALYTICS_", "A_", "PG_2_"} {
		if err := CheckPrefix(ok); err != nil {
			t.Errorf("%q was refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"analytics_", "ANALYTICS", "_ANALYTICS_", "2_", "AN-ALYTICS_"} {
		if err := CheckPrefix(bad); err == nil {
			t.Errorf("%q was accepted, and would not be a variable name", bad)
		}
	}
}

// The generated password is what stands between an exposed database and
// the internet, so it has to be long, random, and free of the
// characters that make a password painful to retype.
func TestAGeneratedPasswordIsLongRandomAndTypeable(t *testing.T) {
	a, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	b, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if a == b {
		t.Fatal("two calls produced the same password")
	}
	if len(a) != GeneratedPasswordLength {
		t.Fatalf("password is %d characters, want %d", len(a), GeneratedPasswordLength)
	}
	for _, c := range a {
		if !strings.ContainsRune(passwordAlphabet, c) {
			t.Fatalf("password contains %q, which is outside the alphabet", c)
		}
	}
}

// A reference is what becomes a container name, so a malformed one must
// not get that far.
func TestAReferenceIsThreeSlugsAndBecomesAContainerName(t *testing.T) {
	ref, err := ParseReference("web/production/pg")
	if err != nil {
		t.Fatalf("ParseReference: %v", err)
	}
	if got := ref.ContainerName(); got != "cubeship-db-web-production-pg" {
		t.Errorf("container name is %q", got)
	}

	// Two parts is production, the same shorthand an app reference takes.
	short, err := ParseReference("web/pg")
	if err != nil {
		t.Fatalf("ParseReference(two parts): %v", err)
	}
	if short.Environment != "production" {
		t.Errorf("two-part reference resolved to environment %q", short.Environment)
	}

	for _, bad := range []string{"pg", "web/production/pg/extra", "web/production/PG", "web//pg", ""} {
		if _, err := ParseReference(bad); err == nil {
			t.Errorf("%q was accepted as a reference", bad)
		}
	}
}

// A datastore's container name must not be able to collide with an
// app's. They share an environment and people name things the same way
// in both, and two containers under one name is one that will not start.
func TestADatastoreCannotCollideWithAnAppsContainer(t *testing.T) {
	ref := Reference{Project: "web", Environment: "production", Name: "api"}
	if !strings.HasPrefix(ref.ContainerName(), containerPrefix) {
		t.Fatalf("container name %q does not carry the datastore prefix", ref.ContainerName())
	}
	// The app orchestrator names containers "cubeship-<project>-...".
	if strings.HasPrefix(ref.ContainerName(), "cubeship-"+ref.Project) {
		t.Errorf("container name %q is in the same namespace an app's is", ref.ContainerName())
	}
}
