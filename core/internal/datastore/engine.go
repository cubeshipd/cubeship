package datastore

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

	// consistency says what a dump of this engine actually promises,
	// and it is on the screen rather than only in a comment: two of
	// these give a real snapshot, one gives one per collection and
	// cannot do better without a replica set, and one is a file copied
	// after a fork.
	consistency string
}

// PostgresShmSize is how much /dev/shm a Postgres datastore gets.
//
// The Engine's default is 64 MiB, and Postgres takes its dynamic shared
// memory from there: a parallel query that wants more fails with "could
// not resize shared memory segment" on a database with nothing else
// wrong with it. It is the one setting every Postgres compose file
// raises, and the number here is the one they use.
//
// Applied to Postgres only. The other engines do not use /dev/shm for
// anything, and a gigabyte of tmpfs per container on a box this size is
// not free.
const PostgresShmSize = 256 << 20

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
		// **Redis is not backed up here, and that is a decision.** It
		// could be — `redis-cli --rdb -` streams one — but putting it
		// back is the half that does not work: an RDB is read once, at
		// startup, so a restore means stopping the server and replacing
		// the file. A backup whose restore takes the database away is
		// worth having for the engine holding somebody's tables and not
		// for the one in front of them, which is what this is on a box
		// this size.
		//
		// Somebody running Redis as a queue rather than a cache is the
		// case this leaves out, and `--appendonly yes` above is what
		// already carries that across a restart.
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
