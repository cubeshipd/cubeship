package datastore

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cubeship/internal/envvar"
)

// Engine says which database server a datastore runs.
//
// It is the discriminator behind every difference between them: the
// image, the port, where the data lives, how the image is told which
// login to create, and what an attached app's variables are called.
// Adding one is filling in a spec, not editing the service.
type Engine string

const (
	EnginePostgres Engine = "postgres"
	EngineMySQL    Engine = "mysql"
	EngineMariaDB  Engine = "mariadb"
	EngineRedis    Engine = "redis"
	EngineMongoDB  Engine = "mongodb"
)

// Engines is every engine this version can run, in the order a form
// should offer them: the relational ones first, because that is what
// most things mean by "a database".
func Engines() []Engine {
	return []Engine{EnginePostgres, EngineMySQL, EngineMariaDB, EngineRedis, EngineMongoDB}
}

// DefaultUsername is the login offered when nobody names one, for the
// engines that let you choose. Ours rather than the engine's own —
// `postgres` and `root` are the two names every scanner on the internet
// tries first, and "root" is one MySQL refuses to create through its
// environment anyway.
//
// Redis is the exception, and it is the engine's own rule rather than a
// preference: its password belongs to the ACL user `default`, which
// already exists and cannot be renamed. See spec.fixedUser.
const DefaultUsername = "cubeship"

// spec is everything that differs between one engine and another.
type spec struct {
	// image is the repository, without a tag.
	image string
	// versions are the tags this release offers, newest first. The
	// first is what a datastore created without naming one gets.
	//
	// Pinned rather than open: a version is not editable after
	// creation, so offering one is a promise to keep running it.
	versions []string
	// port is what the server listens on inside its container.
	port int
	// dataPath is where the engine keeps its files, and so what the
	// host directory is mounted over.
	dataPath string
	// scheme is what a connection URL for this engine starts with.
	scheme string
	// query is appended to that URL. Postgres clients that default to
	// requiring TLS need telling; there is none on this network, and
	// there is none over an exposed port either — see Service.Expose.
	query string
	// stem names the variables an attached app receives: DATABASE_URL
	// for the engines that hold tables, REDIS_URL for one that does not.
	stem string
	// defaultUser is the login an empty username becomes.
	defaultUser string
	// fixedUser says the engine has exactly one login and it cannot be
	// renamed — Redis's `default`. A request naming another is refused
	// rather than quietly overwritten: silently ignoring what somebody
	// typed is how a connection string comes out different from what
	// they thought they asked for.
	fixedUser bool
	// hasDatabase reports whether a named database inside the server
	// means anything here.
	hasDatabase bool
	// env is how this image is told what to create on first start.
	// Nil for an engine configured through its command line instead.
	env func(d *Datastore) []string
	// cmd replaces the image's own command, for an engine that takes
	// its password as an argument rather than from the environment.
	cmd func(d *Datastore) []string
	// checkUsername refuses a login this engine will not create, beyond
	// the shape every engine requires.
	checkUsername func(name string) error
}

// specs is the whole of what Cubeship knows about running a database.
//
// TestEveryEngineHasASpec pins it against Engines(), so adding an engine
// is a decision about what it needs rather than something half done.
var specs = map[Engine]spec{
	EnginePostgres: {
		image:       "postgres",
		versions:    []string{"17", "16", "15"},
		port:        5432,
		dataPath:    "/var/lib/postgresql/data",
		scheme:      "postgresql",
		query:       "sslmode=disable",
		stem:        "DATABASE",
		defaultUser: DefaultUsername,
		hasDatabase: true,
		env: func(d *Datastore) []string {
			return []string{
				"POSTGRES_USER=" + d.Username,
				"POSTGRES_PASSWORD=" + d.Password,
				"POSTGRES_DB=" + d.Database,
				// No PGDATA. The image's own advice is to point it at a
				// subdirectory of the mount, and following it broke
				// every Postgres this ever provisioned.
				//
				// The entrypoint chowns PGDATA *and below* to the
				// postgres user, and nothing above it. With PGDATA a
				// subdirectory, the mount itself stayed root-owned and
				// 0700 — the mode the daemon creates it with — and the
				// postgres user could not traverse into its own data
				// directory. Permission denied, on a loop, from a
				// container running as root a moment earlier.
				//
				// Left alone, PGDATA is the mount, the entrypoint
				// chowns the mount, and it works. That is what the
				// daemon's own Postgres has always done — see
				// bootstrap.PostgresContainerOpts, which is the same
				// image on the same kind of directory and has never had
				// this problem.
			}
		},
	},
	EngineMySQL: {
		image:       "mysql",
		versions:    []string{"8.4", "8.0"},
		port:        3306,
		dataPath:    "/var/lib/mysql",
		scheme:      "mysql",
		stem:        "DATABASE",
		defaultUser: DefaultUsername,
		hasDatabase: true,
		env: func(d *Datastore) []string {
			return []string{
				// Cubeship never connects as root, so nothing here needs
				// to know that password. Letting the image invent one it
				// prints once keeps a credential nobody stored out of a
				// container's environment, where `docker inspect` reads
				// it.
				"MYSQL_RANDOM_ROOT_PASSWORD=yes",
				"MYSQL_USER=" + d.Username,
				"MYSQL_PASSWORD=" + d.Password,
				"MYSQL_DATABASE=" + d.Database,
			}
		},
		checkUsername: refuseRoot,
	},
	EngineMariaDB: {
		image:    "mariadb",
		versions: []string{"11.4", "10.11"},
		port:     3306,
		dataPath: "/var/lib/mysql",
		// MariaDB speaks the MySQL wire protocol and every client
		// addresses it the same way, so the URL says so too.
		scheme:      "mysql",
		stem:        "DATABASE",
		defaultUser: DefaultUsername,
		hasDatabase: true,
		env: func(d *Datastore) []string {
			return []string{
				"MARIADB_RANDOM_ROOT_PASSWORD=yes",
				"MARIADB_USER=" + d.Username,
				"MARIADB_PASSWORD=" + d.Password,
				"MARIADB_DATABASE=" + d.Database,
			}
		},
		checkUsername: refuseRoot,
	},
	EngineRedis: {
		image:    "redis",
		versions: []string{"7.4", "7.2"},
		port:     6379,
		dataPath: "/data",
		scheme:   "redis",
		stem:     "REDIS",
		// Redis has one login and it is called `default`. The password
		// set below belongs to it; naming any other user would be a
		// connection string nothing accepts.
		defaultUser: "default",
		fixedUser:   true,
		// No named databases. Redis has numbered ones, which are not
		// the same idea and are not something to provision.
		hasDatabase: false,
		cmd: func(d *Datastore) []string {
			// Through the command line because the official image has
			// no password environment variable — the ones that do are
			// somebody else's build of it.
			//
			// appendonly makes it write to the data directory this
			// instance mounts. Without it Redis keeps everything in
			// memory and snapshots on its own schedule, and a restart
			// is a database that lost the last few minutes. Somebody
			// running Redis purely as a cache loses nothing by having
			// it on; somebody running it as a queue loses their queue
			// by having it off.
			return []string{
				"redis-server",
				"--requirepass", d.Password,
				"--appendonly", "yes",
			}
		},
	},
	EngineMongoDB: {
		image:    "mongo",
		versions: []string{"8.0", "7.0"},
		port:     27017,
		dataPath: "/data/db",
		scheme:   "mongodb",
		// The root user lives in the `admin` database whatever database
		// the connection names, so every client has to be told where to
		// authenticate. Left out, every connection fails on credentials
		// that are perfectly correct.
		query:       "authSource=admin",
		stem:        "MONGO",
		defaultUser: DefaultUsername,
		hasDatabase: true,
		env: func(d *Datastore) []string {
			return []string{
				"MONGO_INITDB_ROOT_USERNAME=" + d.Username,
				"MONGO_INITDB_ROOT_PASSWORD=" + d.Password,
				"MONGO_INITDB_DATABASE=" + d.Database,
			}
		},
	},
}

// ErrFixedUsername is a login an engine will not let you choose.
var ErrFixedUsername = errors.New("this engine's login cannot be changed")

// refuseRoot is MySQL's and MariaDB's own rule, said where the person
// who typed it can still type another. Their images refuse to create
// root through the environment — root already exists — and the failure
// is a line in a container log nobody is reading.
func refuseRoot(name string) error {
	if strings.EqualFold(name, "root") {
		return fmt.Errorf("%w: MySQL and MariaDB will not create a user called \"root\" — it already exists, and Cubeship does not hold its password", ErrBadUsername)
	}
	return nil
}

// Valid reports whether e is an engine this version can run.
func (e Engine) Valid() bool {
	_, ok := specs[e]
	return ok
}

// Versions are the tags offered for e, newest first.
func (e Engine) Versions() []string { return specs[e].versions }

// DefaultVersion is what a datastore created without naming one runs.
func (e Engine) DefaultVersion() string {
	v := specs[e].versions
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// Port is what this engine listens on inside its container.
func (e Engine) Port() int { return specs[e].port }

// HasDatabase reports whether a named database inside the server means
// anything for this engine.
func (e Engine) HasDatabase() bool { return specs[e].hasDatabase }

// HasUser reports whether the login is somebody's to choose. False for
// Redis, whose password belongs to a user that already exists.
func (e Engine) HasUser() bool { return !specs[e].fixedUser }

// DefaultUsername is the login an empty username becomes.
func (e Engine) DefaultUsername() string { return specs[e].defaultUser }

// Image is the reference the container runs.
func (e Engine) Image(version string) string {
	return specs[e].image + ":" + version
}

// KnowsVersion reports whether version is one this release offers for e.
func (e Engine) KnowsVersion(version string) bool {
	for _, v := range specs[e].versions {
		if v == version {
			return true
		}
	}
	return false
}

// ContainerEnv is how the image is told what to create on first start.
// Empty for an engine configured through its command line instead.
func (d *Datastore) ContainerEnv() []string {
	if build := specs[d.Engine].env; build != nil {
		return build(d)
	}
	return nil
}

// ContainerCmd replaces the image's own command, for an engine that
// takes its password as an argument. Empty for the rest, which leaves
// the image's entrypoint alone.
func (d *Datastore) ContainerCmd() []string {
	if build := specs[d.Engine].cmd; build != nil {
		return build(d)
	}
	return nil
}

// DataPath is where this engine keeps its files inside the container,
// and so what the host directory is mounted over.
func (d *Datastore) DataPath() string { return specs[d.Engine].dataPath }

// identifier is the shape a login and a database name have to take.
//
// Both are handed to the image as environment variables and used by it
// verbatim in SQL, so this is the one place that decides what may reach
// there. It is also every engine's own rule for an unquoted identifier.
var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

// CheckUsername refuses a login no engine would create, then whatever
// this engine refuses on top of that.
func CheckUsername(e Engine, name string) error {
	if specs[e].fixedUser && name != specs[e].defaultUser {
		return fmt.Errorf("%w: %s authenticates as %q, and the password belongs to it",
			ErrFixedUsername, e, specs[e].defaultUser)
	}
	if !identifier.MatchString(name) {
		return fmt.Errorf("%w: letters, digits and underscores, starting with a letter or underscore, at most 63 characters", ErrBadUsername)
	}
	if check := specs[e].checkUsername; check != nil {
		return check(name)
	}
	return nil
}

// CheckDatabaseName holds a database to the same shape a login is held
// to, and for the same reason.
func CheckDatabaseName(name string) error {
	if !identifier.MatchString(name) {
		return fmt.Errorf("database name must be letters, digits and underscores, starting with a letter or underscore: %w", ErrBadUsername)
	}
	return nil
}

// DefaultDatabaseName turns a datastore's slug into a database name.
//
// Slugs are kebab-case because they are path components; SQL
// identifiers are not, so a dash becomes an underscore rather than
// something the engine would need quoting to address.
func DefaultDatabaseName(s string) string {
	name := strings.ReplaceAll(s, "-", "_")
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = "db_" + name
	}
	return name
}

// prefixPattern is what an attachment's prefix has to look like: the
// front half of an environment variable name, ending in the separator,
// so PREFIX + "DATABASE_URL" is itself a legal name.
var prefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*_$`)

// CheckPrefix accepts an empty prefix — the usual case — and otherwise
// what would still be a legal variable name once a suffix is appended.
func CheckPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if !prefixPattern.MatchString(prefix) {
		return ErrBadPrefix
	}
	return nil
}

// URI is the connection string for this datastore at the given address.
//
// Built through net/url rather than by concatenation, so a password
// someone chose that contains an "@" or a "/" is escaped rather than
// producing a URL that parses as something else entirely.
func (d *Datastore) URI(host string, port int) string {
	sp := specs[d.Engine]
	u := url.URL{
		Scheme: sp.scheme,
		User:   url.UserPassword(d.Username, d.Password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}
	if sp.hasDatabase {
		u.Path = "/" + d.Database
	}
	u.RawQuery = sp.query
	return u.String()
}

// Vars are the variables an app attached to this datastore receives,
// under prefix.
//
// The URL and its parts both, because clients are split down the middle
// on which they want: an ORM takes one string, and a driver configured
// by hand takes five fields. Deriving either from the other is work
// every app would otherwise repeat.
func (d *Datastore) Vars(prefix, host string, port int) envvar.Map {
	sp := specs[d.Engine]
	stem := prefix + sp.stem
	vars := envvar.Map{
		stem + "_URL":      d.URI(host, port),
		stem + "_HOST":     host,
		stem + "_PORT":     strconv.Itoa(port),
		stem + "_USER":     d.Username,
		stem + "_PASSWORD": d.Password,
	}
	if sp.hasDatabase {
		vars[stem+"_NAME"] = d.Database
	}
	return vars
}

// passwordAlphabet is letters and digits and nothing else.
//
// Not because a symbol would be unsafe — URI escapes whatever it is
// given — but because a generated password is read aloud, retyped into a
// psql prompt and pasted into a client's config box, and every one of
// those is a place a quote or a backslash becomes somebody's afternoon.
// A chosen password may contain anything.
const passwordAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GeneratedPasswordLength is 24 characters of that alphabet — around
// 143 bits, which is far past anything that can be guessed at a
// database's connection rate.
const GeneratedPasswordLength = 24

// GeneratePassword returns a password for a datastore nobody chose one
// for. It is what the API fills in when a request omits the field, so a
// database without a strong password is not something anyone can create
// by leaving a box empty.
func GeneratePassword() (string, error) {
	limit := big.NewInt(int64(len(passwordAlphabet)))
	out := make([]byte, GeneratedPasswordLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", errors.New("generate password: " + err.Error())
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}
