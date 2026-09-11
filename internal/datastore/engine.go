package datastore

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/authkey"
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

	// dump is the command that writes this engine's contents to stdout,
	// and restore is the one that reads them back from stdin. Both run
	// inside the container.
	//
	// The password goes in the environment wherever the engine's tools
	// read one — PGPASSWORD, MYSQL_PWD — because argv is visible in the
	// host's process list while the command runs. **Mongo's tools have
	// no such variable**, so there it is on the command line and this
	// comment says so rather than implying otherwise. It costs little
	// in practice: that password is already in the container's own
	// environment, where `docker inspect` reads it.
	//
	// Nil for an engine with no logical dump at all, which is Redis:
	// what it has is a file, and dumpFile names it.
	dump    func(d *Datastore) (cmd []string, env []string)
	restore func(d *Datastore) (cmd []string, env []string)

	// dumpFile is the file, relative to the data directory, that a
	// restore has to replace for an engine with no restore command.
	// Redis's RDB. Empty for every engine that can be restored by
	// feeding a stream to a running server.
	dumpFile string

	// consistency says what a dump of this engine actually promises,
	// and it is on the screen rather than only in a comment: two of
	// these give a real snapshot, one gives one per collection and
	// cannot do better without a replica set, and one is a file copied
	// after a fork.
	consistency string
}

// postgresDataPath is where Postgres keeps its files inside the
// container. It is the bind mount and it is PGDATA, which is one fact
// rather than two — see the env below for why saying so matters.
const postgresDataPath = "/var/lib/postgresql/data"

// specs is the whole of what Cubeship knows about running a database.
//
// TestEveryEngineHasASpec pins it against Engines(), so adding an engine
// is a decision about what it needs rather than something half done.
var specs = map[Engine]spec{
	EnginePostgres: {
		image:       "postgres",
		versions:    []string{"18", "17", "16", "15"},
		port:        5432,
		dataPath:    postgresDataPath,
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
				// PGDATA is the mount itself, and both halves of that
				// are load-bearing.
				//
				// **Not below it.** The image's own advice is to point
				// PGDATA at a subdirectory when the data is on a bind
				// mount, and following that broke every Postgres this
				// module provisioned. The entrypoint chowns PGDATA and
				// below to the postgres user and nothing above it, so
				// the mount stayed root-owned and 0700 — the mode the
				// daemon creates it with — and the postgres user could
				// not traverse into its own data directory. Permission
				// denied, on a loop, from a container that was root a
				// moment earlier.
				//
				// **And said rather than left to the image**, which is
				// what 18 changed: its default moved to
				// /var/lib/postgresql/<major>/docker, with the declared
				// volume one level up at /var/lib/postgresql. Silent,
				// and the worst kind — the container comes up, the
				// database works, and none of it is in the bind mount.
				// It is in an anonymous volume nothing on this instance
				// names, so it is in no backup of the data directory
				// and it is orphaned the next time the container is
				// replaced, which is what publishing a port does.
				//
				// Saying it is a no-op on 15 through 17, where it is
				// already the default, and is the whole of what makes
				// 18 keep its data where this instance keeps it.
				"PGDATA=" + postgresDataPath,
			}
		},
		// --clean --if-exists so a restore over a database that already
		// has tables replaces them rather than failing on every CREATE.
		// Without it the only restore that works is into an empty
		// database, which is not the one anybody needs.
		dump: func(d *Datastore) ([]string, []string) {
			return []string{"pg_dump", "--clean", "--if-exists", "-U", d.Username, d.Database},
				[]string{"PGPASSWORD=" + d.Password}
		},
		restore: func(d *Datastore) ([]string, []string) {
			return []string{"psql", "-U", d.Username, "-d", d.Database},
				[]string{"PGPASSWORD=" + d.Password}
		},
		consistency: "One transaction, so the dump is the database as it was at a single moment.",
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
		// --single-transaction is what makes this a snapshot, and it
		// only works for InnoDB, which is every table anybody creates
		// without asking for otherwise. A MyISAM table in there is
		// dumped as it was read rather than as it was at the start.
		dump: func(d *Datastore) ([]string, []string) {
			return []string{"mysqldump", "--single-transaction", "-u", d.Username, d.Database},
				[]string{"MYSQL_PWD=" + d.Password}
		},
		restore: func(d *Datastore) ([]string, []string) {
			return []string{"mysql", "-u", d.Username, d.Database},
				[]string{"MYSQL_PWD=" + d.Password}
		},
		consistency: "One transaction, which covers InnoDB tables. A MyISAM table is dumped as it was read rather than as it was when the dump began.",
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
		dump: func(d *Datastore) ([]string, []string) {
			return []string{"mariadb-dump", "--single-transaction", "-u", d.Username, d.Database},
				[]string{"MYSQL_PWD=" + d.Password}
		},
		restore: func(d *Datastore) ([]string, []string) {
			return []string{"mariadb", "-u", d.Username, d.Database},
				[]string{"MYSQL_PWD=" + d.Password}
		},
		consistency: "One transaction, which covers InnoDB tables. A MyISAM table is dumped as it was read rather than as it was when the dump began.",
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
		// `--rdb -` is the replica-sync path pointed at stdout: it asks
		// the server to save and streams the file as it is written, so
		// there is no second step waiting for a BGSAVE to land.
		//
		// REDISCLI_AUTH rather than `-a`, which redis-cli itself warns
		// about: the password would be in the host's process list.
		dump: func(d *Datastore) ([]string, []string) {
			return []string{"redis-cli", "--rdb", "-"},
				[]string{"REDISCLI_AUTH=" + d.Password}
		},
		// **And no restore command, because Redis has none.** An RDB is
		// read once, at startup — there is nothing to stream it into.
		// Putting one back means stopping the server, replacing the
		// file and starting it again, which this module can do without
		// the container's help: the data directory is a host bind
		// mount. dumpFile is that file, named from inside the
		// container so it reads like the rest of the spec; what the
		// daemon writes is the same name under the mount.
		dumpFile:    "dump.rdb",
		consistency: "A fork of the server's memory at the moment the save began, which is the only snapshot Redis has.",
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
		// The password is in argv here, which is the one engine where
		// that is true: mongodump and mongorestore read no environment
		// variable for it. See the note on `dump` above.
		//
		// --archive writes one stream instead of a directory tree,
		// which is what makes this a file that can be shipped to a
		// bucket. --drop on the way back, for the reason Postgres takes
		// --clean.
		dump: func(d *Datastore) ([]string, []string) {
			return []string{
				"mongodump", "--archive", "--quiet",
				"-u", d.Username, "-p", d.Password,
				"--authenticationDatabase", "admin", "--db", d.Database,
			}, nil
		},
		restore: func(d *Datastore) ([]string, []string) {
			return []string{
				"mongorestore", "--archive", "--drop", "--quiet",
				"-u", d.Username, "-p", d.Password,
				"--authenticationDatabase", "admin",
			}, nil
		},
		// Said plainly because it cannot be fixed here: a standalone
		// mongod has no oplog, and --oplog is what a consistent dump
		// across collections needs.
		consistency: "Each collection as it was read, not all of them at one moment: a consistent snapshot needs a replica set, which a single server is not.",
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

// VarStem is the middle of the variables an attached app receives:
// `<prefix>DATABASE_URL` for the engines that hold tables, `REDIS_URL`
// and `MONGO_URL` for the two that do not.
//
// It is what decides whether two attachments collide. A Redis and a
// Postgres on one app at the same prefix name nothing in common, so
// they are not a conflict — which is why the uniqueness is over this
// and not over the prefix alone.
func (e Engine) VarStem() string { return specs[e].stem }

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

// GeneratedPasswordLength is how long a generated one is.
//
// It and the generator live in authkey now: this instance invents a
// password in two places — here and an account an admin opens for
// somebody else — and two generators would be two answers to what one
// looks like. The names stay because a datastore's password is what
// everything here calls it.
const GeneratedPasswordLength = authkey.PasswordLength

// GeneratePassword returns a password for a datastore nobody chose one
// for. It is what the API fills in when a request omits the field, so a
// database without a strong password is not something anyone can create
// by leaving a box empty.
func GeneratePassword() (string, error) { return authkey.Password() }

// Dump is the command and environment that write this datastore's
// contents to stdout, and whether it has one at all.
//
// An engine with no dump command has a file instead — see DumpFile —
// and a caller has to ask which it is before it can back one up.
func (d *Datastore) Dump() (cmd []string, env []string, ok bool) {
	build := specs[d.Engine].dump
	if build == nil {
		return nil, nil, false
	}
	cmd, env = build(d)
	return cmd, env, true
}

// Restore is the command and environment that read a dump back from
// stdin.
func (d *Datastore) Restore() (cmd []string, env []string, ok bool) {
	build := specs[d.Engine].restore
	if build == nil {
		return nil, nil, false
	}
	cmd, env = build(d)
	return cmd, env, true
}

// DumpFile is the file inside the data directory that a restore has to
// replace, for an engine with no restore command — Redis's RDB. Empty
// for every engine that can be restored by feeding a running server.
func (d *Datastore) DumpFile() string { return specs[d.Engine].dumpFile }

// Consistency says what a dump of this engine actually promises, in a
// sentence meant for the screen.
//
// It is served rather than left in a comment because it is the one
// thing about a backup somebody has to know before they rely on it, and
// it differs per engine in a way no general wording can cover: one of
// these cannot give a snapshot across collections at all.
func (e Engine) Consistency() string { return specs[e].consistency }

// CanBackUp reports whether this release knows how to take a dump of
// this engine at all.
func (e Engine) CanBackUp() bool { return specs[e].dump != nil }

// RestoreStops reports an engine that cannot be restored into while it
// runs: its backup is a file read once at startup, so putting one back
// means stopping the server, replacing it, and starting again.
//
// It is on the screen before the button, because it is the difference
// between a restore that is invisible to everything connected and one
// that takes the database away for a few seconds.
func (e Engine) RestoreStops() bool { return specs[e].restore == nil }
