// Package user owns identities and the credentials they authenticate
// with: the User and APIKey entities, their persistence, the use cases
// that manage them, and the HTTP and MCP surfaces those are reached
// through.
//
// It also owns the one authorization question on the instance. There is
// no tenant boundary above it — Cubeship runs one instance on one VPS —
// so what a caller may do is a property of the account, and Require is
// where every other module asks.
package user

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Role is what an account may do on this instance.
//
// The two are not a hierarchy of seniority but a line between two kinds
// of act: running an image someone already published, and turning source
// into an image on this host. See app.RoleToDeploy.
type Role string

const (
	// RoleAdmin can add accounts, create projects and environments,
	// configure the instance, build source into images, and everything
	// a member can.
	RoleAdmin Role = "admin"
	// RoleMember can create, deploy, configure and read the logs of
	// apps that run published images.
	RoleMember Role = "member"
)

// Valid reports whether r is a role this instance recognizes.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleMember }

// User is one identity on this Cubeship instance.
type User struct {
	ID       int64
	Username string
	Role     Role
	// Theme is which palette this person sees the dashboard in, empty
	// for the default. A fact about the person rather than about the
	// machine they opened it on — see Themes.
	Theme string
	// DisplayName is what the person is called, which a username often
	// is not: `lgs` is an address, "Lucas" is a name. Empty is normal —
	// the username stands in wherever it is shown.
	DisplayName string
	// Email is somewhere to reach whoever holds this account. **Nothing
	// on this instance sends mail**, and it is stored anyway for the
	// same reason a description was once kept: an operator handing a
	// box to somebody else should be able to find out whose account is
	// whose. It is not a second way to sign in and never becomes one.
	Email string
	// Avatar is a name from Avatars, never a path or a URL, and never
	// empty — see DefaultAvatar.
	Avatar string
	// BlockedAt is when somebody shut this account out, nil while it is
	// not. Every door checks it — see ErrBlocked — and nothing about the
	// account is destroyed, which is the whole difference between this
	// and deleting one.
	BlockedAt *time.Time
	CreatedAt time.Time
}

// Blocked reports whether this account may authenticate at all.
func (u *User) Blocked() bool { return u != nil && u.BlockedAt != nil }

// Avatars are the faces the dashboard ships, by name.
//
// The list is here for the reason Themes is: the daemon is what refuses
// a name, and a second list in the browser would be one to disagree
// with it. The files are `web/public/profiles/<name>.png`.
//
// **Adding one is two edits — the file, and this line — and they cannot
// be derived from each other**: the images are in the dashboard's
// image and this runs in the daemon's, which are two containers. What
// makes two edits safe is that forgetting either fails a test rather
// than shipping: a name here with no file there is a broken image on
// somebody's account, and a file there with no name here is a face
// nobody can choose and nobody knows is missing. See avatars_test.go.
// `scripts/profiles.sh` prepares the files and prints this line.
//
// **They are the palettes' own names**, because they are drawn in the
// same colours: somebody on the red theme picking the red face is the
// whole of why there is more than one. A second vocabulary for one set
// of colours would be one to keep in step by hand, and there is nothing
// to gain by calling the same red something else here.
//
// **Sharing the vocabulary is not the same as being the same list**, and
// this is where that shows: `blue` is a palette with no face, because a
// face is an image somebody has to draw and a palette is twenty lines of
// CSS. Nothing here requires the two to match — `Themes` and this are
// read for different questions, and a face missing for a palette costs
// somebody on that palette a choice, not a broken screen. Which is why
// there is no test tying them together: it would fail for a reason that
// is not a fault.
var Avatars = []string{"blue", "cyan", "hacker", "mono", "orange", "pink", "purple", "red"}

// ErrUnknownAvatar is a face this instance does not ship.
var ErrUnknownAvatar = errors.New("no avatar by that name")

// ErrBadEmail refuses something that is not an address. The check is
// deliberately shallow — one @ with something either side — because
// the only thing that proves an address is sending to it, and nothing
// here sends.
var ErrBadEmail = errors.New("that is not an email address")

// ErrBadDisplayName refuses one long enough to break a layout. It is a
// name rather than an identifier, so nothing else about it is this
// instance's business.
var ErrBadDisplayName = errors.New("a display name is at most 60 characters")

// ErrBadUsername refuses a name that cannot be one.
var ErrBadUsername = errors.New(
	"a username is 1-32 characters of lowercase letters, digits, dot, dash or underscore, starting with a letter or a digit")

// usernamePattern is what a username may be.
//
// **There was no rule at all until a username became editable**, and
// the unique index was the only thing refusing anything — so a name
// with a slash in it was accepted and then addressed nothing:
// `/users/{username}` would never match it, and neither would the
// registry's basic auth. Applied to creating an account as well as to
// renaming one, because the two produce the same column.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// ValidUsername reports whether a name may be used, or says why not.
func ValidUsername(s string) error {
	if !usernamePattern.MatchString(s) {
		return ErrBadUsername
	}
	return nil
}

// DefaultAvatar is the face an account arrives with.
//
// **There is no "no face".** It was an answer for one release — the
// picker offered it first and the sidebar drew two letters of the
// username instead — and it was the answer almost every account had,
// because it was what an account was made with. So the ordinary state
// of the feature was its own fallback, and the fallback was a second
// thing the same row could be, drawn by a second branch on every
// screen.
//
// Cyan because it is the interface's own accent and the palette an
// account already starts on: a new account looks like this instance
// rather than like one nobody has finished setting up. See migration
// 00048, which is what makes it true of the accounts that already
// exist.
const DefaultAvatar = "cyan"

// ValidAvatar reports whether s is one of them. Empty is not one: see
// DefaultAvatar.
func ValidAvatar(s string) bool {
	return slices.Contains(Avatars, s)
}

// ValidEmail is one @ with something either side and no spaces.
func ValidEmail(s string) bool {
	if s == "" {
		return true
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	local, domain, found := strings.Cut(s, "@")
	return found && local != "" && domain != "" && strings.Contains(domain, ".")
}

// Themes are the palettes the dashboard offers.
//
// **All of them are dark but `helix`**, and every one of them changes
// only colour: the layout, the type and the square corners are the
// product, and a theme that moved those would be a second interface to
// keep working. Empty is the first.
//
// The list is here rather than in the dashboard because it is what the
// daemon will accept, and two lists would be one to disagree with: a
// browser sending a name this refuses is a preference that saves and
// then is not there.
var Themes = []string{
	"cyan", "mono", "hacker", "red", "orange", "pink", "purple", "blue", "helix",
}

// DefaultTheme is the palette an account sees until it picks one, and
// it is the one name in Themes with no block of its own in the
// stylesheet: `:root` **is** cyan, and the other palettes are what
// override it. So `""` and `"cyan"` paint the same interface, and the
// picker offers the first — two entries somebody cannot tell apart is
// worse than one.
//
// It is named here rather than only in the CSS because the list above
// has to be able to say which of its entries is the one that needs no
// block. See themes_test.go, which is what would otherwise read a
// working default as a broken palette.
const DefaultTheme = "cyan"

// ErrUnknownTheme is a palette this instance does not have.
var ErrUnknownTheme = errors.New("no theme by that name")

// ValidTheme reports whether s is one of them, or empty for the default.
func ValidTheme(s string) bool {
	if s == "" {
		return true
	}
	return slices.Contains(Themes, s)
}

// Is reports whether u holds at least min. An admin satisfies both
// checks; a member only satisfies RoleMember.
func (u *User) Is(min Role) bool {
	if u == nil {
		return false
	}
	return min == RoleMember || u.Role == RoleAdmin
}

// Require is the authorization every module calls. It answers with the
// error the caller should see rather than a bool, so the two refusals
// stay distinct: nobody signed in at all, and somebody signed in who
// lacks the role.
func Require(caller *User, min Role) error {
	if caller == nil {
		return ErrUnauthenticated
	}
	if !caller.Is(min) {
		return ErrForbidden
	}
	return nil
}

// APIKey is one credential belonging to a User. Only its hash is ever
// stored; the key itself is shown once, at creation, and never again.
type APIKey struct {
	ID         int64
	UserID     int64
	KeyHash    string
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// DefaultAPIKeyName is the name given to a key created without one
// explicitly chosen: a new account's first key. A key created through
// the "additional key" endpoint always carries a caller-chosen name
// instead — "mcp", "laptop", whatever distinguishes it.
const DefaultAPIKeyName = "default"

var (
	// ErrUnauthenticated is what the service returns when it is handed
	// no caller at all — a bug in the transport wiring rather than a
	// user error, but never a panic.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrForbidden is returned when the caller is signed in but lacks
	// the role an action requires. Unlike a missing resource, this is
	// said plainly: they already know the thing exists, and hiding it
	// would only confuse them.
	ErrForbidden = errors.New("forbidden: this requires the admin role")

	// ErrInvalidRole reports a role string that is neither admin nor
	// member.
	ErrInvalidRole = errors.New(`role must be "admin" or "member"`)

	// ErrUsernameTaken reports that the username was claimed by a
	// concurrent request while this one was working.
	ErrUsernameTaken = errors.New("that username was just taken; try again")

	// ErrNoSuchUser is a username that is not an account here.
	ErrNoSuchUser = errors.New("no such account")

	// ErrCannotRemoveYourself refuses deleting the account making the
	// request. Whoever meant to leave has to be removed by somebody
	// else, and an admin who deletes themselves mid-session is the one
	// mistake nothing on the instance can undo.
	ErrCannotRemoveYourself = errors.New("you cannot delete the account you are signed in as")

	// ErrLastAdmin refuses removing, demoting or blocking the only
	// admin. An instance with no admin can never configure itself
	// again, and nothing in the API could put one back — setup is
	// closed the moment the first account exists.
	ErrLastAdmin = errors.New("this is the only admin on the instance")

	// ErrCannotBlockYourself refuses shutting out the account making
	// the request, for the reason deleting it is refused: an admin who
	// blocks themselves is refused by the very door they would have to
	// come back through.
	ErrCannotBlockYourself = errors.New("you cannot block the account you are signed in as")

	// ErrCannotChangeYourOwnRole refuses an admin demoting themselves.
	// There may be another admin to put it back and there may not, and
	// which it is cannot be known from inside the request — an instance
	// whose last admin made themselves a member is one nothing can
	// configure again. Somebody else does it, the same as leaving.
	ErrCannotChangeYourOwnRole = errors.New("you cannot change your own role")

	// ErrBlocked is an account that has been shut out. It is answered
	// to every way in — a password, an API key, a session cookie — and
	// it is deliberately **not** ErrInvalidCredentials: the credential
	// is fine and the account is not, and telling somebody their
	// password is wrong when it is right sends them to reset a password
	// that was never the problem.
	//
	// It says nothing about who blocked them or when. That is the
	// instance's business, and this is the one message an account that
	// should not be here still gets to read.
	ErrBlocked = errors.New("this account has been blocked on this instance")
)
