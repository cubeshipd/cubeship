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
		// Told through the environment or through its command line —
		// one or the other, or the password it comes up with is not one
		// anybody holds.
		if sp.env == nil && sp.cmd == nil {
			t.Errorf("%s cannot be told what login to create", e)
		}
		if sp.defaultUser == "" {
			t.Errorf("%s has no default login, so an empty username would create nothing", e)
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
		user := "app"
		if !e.HasUser() {
			user = e.DefaultUsername()
		}
		d := &Datastore{Engine: e, Username: user, Password: "s3cret", Database: "app_db"}
		// Environment or command line — Redis takes its password as an
		// argument, because the official image has no variable for it.
		told := strings.Join(append(d.ContainerEnv(), d.ContainerCmd()...), "\n")

		if !strings.Contains(told, "s3cret") {
			t.Errorf("%s: the container is never told the password:\n%s", e, told)
		}
		if e.HasUser() && !strings.Contains(told, "app") {
			t.Errorf("%s: the container is never told the login:\n%s", e, told)
		}
		if e.HasDatabase() && !strings.Contains(told, "app_db") {
			t.Errorf("%s: the container is never told which database to create:\n%s", e, told)
		}
	}
}

// Redis has one login, called `default`, and the password belongs to
// it. Accepting another name would produce a connection string nothing
// accepts — refused rather than quietly overwritten, because silently
// ignoring what somebody typed is how a credential comes out different
// from what they thought they asked for.
func TestAnEngineWithOneLoginRefusesAnother(t *testing.T) {
	if EngineRedis.HasUser() {
		t.Error("redis reports a choosable login")
	}
	if got := EngineRedis.DefaultUsername(); got != "default" {
		t.Errorf("redis defaults to %q", got)
	}
	if err := CheckUsername(EngineRedis, "cubeship"); err == nil {
		t.Error("redis accepted a login it does not have")
	}
	if err := CheckUsername(EngineRedis, "default"); err != nil {
		t.Errorf("redis refused its own login: %v", err)
	}
	// Everything else is somebody's to choose.
	for _, e := range []Engine{EnginePostgres, EngineMySQL, EngineMariaDB, EngineMongoDB} {
		if !e.HasUser() {
			t.Errorf("%s reports a fixed login", e)
		}
	}
}

// Mongo's root user lives in the `admin` database whatever database the
// connection names, so every client has to be told where to
// authenticate. Left out, every connection fails on credentials that
// are perfectly correct.
func TestMongoTellsClientsWhereToAuthenticate(t *testing.T) {
	d := &Datastore{Engine: EngineMongoDB, Username: "app", Password: "secret", Database: "app_db"}
	uri := d.URI("cubeship-db-mongo", 27017)
	if !strings.Contains(uri, "authSource=admin") {
		t.Errorf("mongo URI is %q, and would fail on correct credentials", uri)
	}
	if !strings.HasPrefix(uri, "mongodb://") {
		t.Errorf("mongo URI is %q", uri)
	}
}

// An engine with no named databases is handed no database name, and its
// variables do not carry one — there is nothing for a client to put in
// it.
func TestAnEngineWithoutDatabasesOffersNoDatabaseVariable(t *testing.T) {
	d := &Datastore{Engine: EngineRedis, Username: "default", Password: "secret"}
	vars := d.Vars("", "cubeship-db-cache", 6379)

	if _, ok := vars["REDIS_NAME"]; ok {
		t.Errorf("redis offered a database name: %v", vars)
	}
	for _, key := range []string{"REDIS_URL", "REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD"} {
		if vars[key] == "" {
			t.Errorf("%s is missing: %v", key, vars)
		}
	}
	// The stem is the engine's, not DATABASE_ for everything: an app
	// with a Postgres and a Redis attached needs both, unprefixed.
	if _, clash := vars["DATABASE_URL"]; clash {
		t.Error("redis wrote DATABASE_URL, which is the SQL database's")
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

// A datastore's container name must not be able to collide with an
// app's. People name things the same way in both, and two containers
// under one name is one that will not start.
//
// The name is also the host every attached app resolves, which is why
// it is the whole of the addressing: there is nothing above a datastore
// to qualify it with.
func TestADatastoreCannotCollideWithAnAppsContainer(t *testing.T) {
	name := ContainerName("api")
	if !strings.HasPrefix(name, containerPrefix) {
		t.Fatalf("container name %q does not carry the datastore prefix", name)
	}
	// The app orchestrator names containers "cubeship-<project>-...",
	// so nothing an app produces can start with this prefix.
	if strings.HasPrefix(name, "cubeship-api") {
		t.Errorf("container name %q is in the same namespace an app's is", name)
	}
	if name != "cubeship-db-api" {
		t.Errorf("container name is %q", name)
	}
}

// The API answers at /datastores/engines, and Go's mux prefers a
// literal over a wildcard — so a datastore called "engines" would be
// one nothing could fetch. It is refused where the person who typed it
// can still type another.
func TestTheApisOwnSegmentIsRefusedAsAName(t *testing.T) {
	if !reservedSlugs["engines"] {
		t.Error(`"engines" is not reserved, and would be a datastore with no address of its own`)
	}
	// Only the exact word. A name that merely contains it is fine.
	for _, ok := range []string{"engines-cache", "my-engines", "engine"} {
		if reservedSlugs[ok] {
			t.Errorf("%q was reported as reserved", ok)
		}
	}
}
