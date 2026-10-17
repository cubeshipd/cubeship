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

// Image is the reference the container runs for a datastore with no
// extensions — the engine's own image at that version.
func (e Engine) Image(version string) string {
	return specs[e].image + ":" + version
}

// Image is the reference this datastore's container runs.
//
// Without extensions it is the engine's own image, unchanged: a database
// created before extensions existed, or created without any, keeps
// running exactly what it ran before. That is not a nicety — it is the
// same image over the same data directory, which is the only thing that
// makes this migration free.
//
// With them it is the build from the support matrix, pinned by digest,
// so which bytes a Cubeship release runs is decided here rather than by
// whoever last pushed the tag.
func (d *Datastore) Image() string {
	if b, ok := buildFor(d.Version, d.Extensions); ok {
		return b.image + ":" + b.tag + "@" + b.digest
	}
	return d.Engine.Image(d.Version)
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
	// An extension build may need libraries mapped before the first
	// backend starts, which no statement can do afterwards — VectorChord
	// is one. Said here rather than relying on the image's own CMD: the
	// image happens to set the same thing today, and a setting that
	// lives in somebody else's default changes without us.
	if libs := preloadFor(d.Version, d.Extensions); len(libs) > 0 {
		return []string{"postgres", "-c", "shared_preload_libraries=" + strings.Join(libs, ",")}
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

// Consistency says what a dump of this engine actually promises, in a
// sentence meant for the screen.
//
// It is served rather than left in a comment because it is the one
// thing about a backup somebody has to know before they rely on it, and
// it differs per engine in a way no general wording can cover: one of
// these cannot give a snapshot across collections at all.
func (e Engine) Consistency() string { return specs[e].consistency }

// CanBackUp reports whether this instance backs this engine up.
//
// False for Redis, and that is a decision rather than a gap: see its
// entry in `specs`. A screen offering a tab that could never hold
// anything is worse than one that does not offer it.
func (e Engine) CanBackUp() bool { return specs[e].dump != nil }
