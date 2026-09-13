package user

import (
	"context"
	"strings"
	"time"

	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database"
)

// Service holds the use cases for identities and their credentials. Both
// the HTTP handlers and the MCP tools call exactly these — neither
// implements any of this logic itself, so the two surfaces cannot drift
// apart.
type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// Add creates an account and the password it signs in with, and is the
// only way one is made after setup. Admin only: an account is a way into
// this instance, so handing out the ability to mint them would hand out
// the instance.
//
// **A password, not an API key.** It used to be the other way round, and
// that account could not sign in at all: a key is what a CLI or an MCP
// client carries, the dashboard wants a session, and nothing anywhere
// let a new person set a first password — there is no invite mail on a
// box like this and no reset flow. So an admin created somebody an
// account, handed them a credential, and the credential opened nothing
// they had been given the address of.
//
// It is the same answer setup already gives the first account, for the
// same reason: the way in is the password, and a key nobody is ever
// shown would be a live credential lying around for nothing. Keys are
// self-service, made from the account screen by whoever wants one.
//
// The password is generated when the caller names none, so an account
// with a weak one is not something anybody gets by leaving a box empty,
// and it is returned exactly once — this instance keeps only its hash,
// like every other credential here. Whoever it belongs to changes it
// from their own account screen, which ends every session but the one
// they are changing it from.
//
// One transaction for both halves. A user created without a password
// would hold their username forever with no way to finish or undo it
// through the API.
func (s *Service) Add(ctx context.Context, caller *User, username, password string, role Role) (*User, string, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, "", err
	}
	if !role.Valid() {
		return nil, "", ErrInvalidRole
	}
	if err := ValidUsername(username); err != nil {
		return nil, "", err
	}
	if password == "" {
		generated, err := authkey.Password()
		if err != nil {
			return nil, "", err
		}
		password = generated
	}

	var created *User
	err := s.db.WithTx(ctx, func(tx database.Queryer) error {
		u, err := s.CreateWithPassword(ctx, tx, username, password, role)
		if database.IsUniqueViolation(err) {
			// Another request took this username between the caller
			// typing it and here. Both cannot own it; the loser is told
			// so rather than shown a driver error.
			return ErrUsernameTaken
		}
		if err != nil {
			return err
		}
		created = u
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return created, password, nil
}

// List returns every account on the instance. An admin's, because it is
// the roster of who can reach this instance at all.
func (s *Service) List(ctx context.Context, caller *User) ([]*User, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

// Delete removes an account and everything it authenticates with.
//
// This is what a person leaving looks like: the account goes, and with
// it every API key and every session it holds — in one transaction, so
// there is no window where the rows that authenticate somebody outlive
// the account they belong to.
func (s *Service) Delete(ctx context.Context, caller *User, username string) error {
	if err := Require(caller, RoleAdmin); err != nil {
		return err
	}
	target, err := s.Repo().ByUsername(ctx, username)
	if err != nil {
		return ErrNoSuchUser
	}
	if caller.ID == target.ID {
		return ErrCannotRemoveYourself
	}

	return s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		if err := repo.refuseIfLastAdmin(ctx, target, true); err != nil {
			return err
		}
		if _, err := repo.DeleteSessions(ctx, target.ID); err != nil {
			return err
		}
		if _, err := repo.DeleteAPIKeys(ctx, target.ID); err != nil {
			return err
		}
		return repo.Delete(ctx, target.ID)
	})
}

// Revoked counts what RevokeCredentials ended.
type Revoked struct {
	APIKeys  int64 `json:"api_keys"`
	Sessions int64 `json:"sessions"`
}

// RevokeCredentials ends every session and revokes every API key an
// account holds, leaving the account itself.
//
// It is the answer to a laptop that walked off: what was on it stops
// working everywhere, at once, without the account having to be deleted
// and made again. The password is not touched — it is a secret in
// somebody's head, not a credential lying on the machine that was lost —
// so signing in again is how the account comes back.
func (s *Service) RevokeCredentials(ctx context.Context, caller *User, username string) (Revoked, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return Revoked{}, err
	}
	target, err := s.Repo().ByUsername(ctx, username)
	if err != nil {
		return Revoked{}, ErrNoSuchUser
	}

	var out Revoked
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		sessions, err := repo.DeleteSessions(ctx, target.ID)
		if err != nil {
			return err
		}
		keys, err := repo.DeleteAPIKeys(ctx, target.ID)
		if err != nil {
			return err
		}
		out = Revoked{APIKeys: keys, Sessions: sessions}
		return nil
	})
	return out, err
}

// SetRole moves an account between admin and member.
//
// **The last admin cannot be demoted, and nobody may demote
// themselves.** The first is the rule ErrLastAdmin has always named:
// an instance with no admin can never configure itself again, and
// setup closed the moment the first account existed. The second is the
// same shape as not being able to delete yourself — whether there is
// another admin to put it back is not knowable from inside the
// request, and the mistake is one the person making it cannot undo.
//
// Nothing about the account's credentials is touched. A role is what
// somebody may do, not who they are: their sessions and keys go on
// being theirs and start being refused for what the new role does not
// reach, on the next request.
func (s *Service) SetRole(ctx context.Context, caller *User, username string, role Role) (*User, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, ErrInvalidRole
	}
	id, err := s.Repo().SystemRoleID(ctx, systemFor(role))
	if err != nil {
		return nil, err
	}
	return s.SetAccessRole(ctx, caller, username, id)
}

// systemFor is the role an admin or a member is given: Admin, or Deploy,
// which is what a member could always do.
func systemFor(role Role) string {
	if role == RoleAdmin {
		return SystemAdmin
	}
	return SystemDeploy
}

// SetBlocked shuts an account out of the instance, or lets it back in.
//
// **It is the reversible half of deleting somebody.** Deleting takes
// the account, its keys and its sessions in one transaction and cannot
// be undone; this closes the door and leaves everything behind it
// exactly as it was, so letting somebody back in is one click and they
// sign in with what they already had.
//
// So **nothing is revoked**. A blocked account's sessions and keys
// stay, and every one of them is refused at the door instead — see
// ErrBlocked, which Login, Authenticate and AuthenticateSession all
// answer. Revoking here would make unblocking a half-undo: the account
// would come back with nothing to come back with, and an admin who
// blocked the wrong person for a minute would have cost them every key
// on every machine they own.
//
// The two refusals are the ones that would shut the instance itself:
// the account making the request, and the only admin.
func (s *Service) SetBlocked(ctx context.Context, caller *User, username string, blocked bool) (*User, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	target, err := s.Repo().ByUsername(ctx, username)
	if err != nil {
		return nil, ErrNoSuchUser
	}
	if caller.ID == target.ID {
		return nil, ErrCannotBlockYourself
	}
	if target.Blocked() == blocked {
		return target, nil
	}

	var updated *User
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		if err := repo.refuseIfLastAdmin(ctx, target, blocked); err != nil {
			return err
		}
		if err := repo.SetBlocked(ctx, target.ID, blocked); err != nil {
			return err
		}
		updated, err = repo.ByID(ctx, target.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ResetPassword issues a new password for somebody else's account and
// returns it once, for the admin to hand over.
//
// **There is no other way back in.** This box sends no mail, so there
// is no reset link, and an account that has forgotten its password has
// nothing else to try — which used to mean the account was deleted and
// made again, losing its keys and its history to recover a secret.
//
// **The API keys are not touched**, and that is the difference between
// this and RevokeCredentials. A forgotten password is not a compromised
// machine: the keys on somebody's laptop are still theirs, and taking
// them as well would make the fix for a forgotten password a morning of
// logging back into everything. Whoever wants both asks for both.
//
// Every session ends, because the password changed — the same rule
// SetPassword follows for an account changing its own. Whoever knew the
// old one should not still be signed in, and this is the case where
// somebody else might.
func (s *Service) ResetPassword(ctx context.Context, caller *User, username string) (string, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return "", err
	}
	target, err := s.Repo().ByUsername(ctx, username)
	if err != nil {
		return "", ErrNoSuchUser
	}

	password, err := authkey.Password()
	if err != nil {
		return "", err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}

	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		if err := repo.SetPassword(ctx, target.ID, hash); err != nil {
			return err
		}
		_, err := repo.DeleteSessions(ctx, target.ID)
		return err
	})
	if err != nil {
		return "", err
	}
	return password, nil
}

// Repo returns a repository over the shared pool, for callers that only
// need to read.
func (s *Service) Repo() *Repository {
	return NewRepository(s.db)
}

// Authenticate resolves a plaintext API key to the identity holding it,
// and records the key as used. The returned hash identifies the specific
// key — not just its owner — which is what lets a caller rotate exactly
// the credential they are calling with.
func (s *Service) Authenticate(ctx context.Context, key string) (*User, string, error) {
	keyHash := authkey.Hash(key)
	u, err := s.Repo().ByAPIKeyHash(ctx, keyHash)
	if err != nil {
		return nil, "", err
	}
	if u.Blocked() {
		return nil, "", ErrBlocked
	}
	k, err := s.Repo().APIKeyByHash(ctx, keyHash)
	if err != nil {
		return nil, "", err
	}
	u.Key = &KeyScope{ID: k.ID, Name: k.Name, AccessRoleID: k.AccessRoleID}
	if u.Policy, err = s.PolicyFor(ctx, u); err != nil {
		return nil, "", err
	}
	// Best effort: a caller whose last_used_at could not be written is
	// still authenticated. Failing the request over a bookkeeping write
	// would take the whole API down with the column.
	_ = s.Repo().TouchAPIKeyLastUsed(ctx, keyHash)
	return u, keyHash, nil
}

// RotateAPIKey replaces the key identified by keyHash with a freshly
// generated one, keeping its name, and returns the new plaintext key.
// Every OTHER key the same user holds is left untouched.
func (s *Service) RotateAPIKey(ctx context.Context, u *User, keyHash string) (string, error) {
	if u == nil || keyHash == "" {
		return "", ErrUnauthenticated
	}
	old, err := s.Repo().APIKeyByHash(ctx, keyHash)
	if err != nil {
		return "", err
	}
	var key string
	// Revoke and reissue in one transaction. Revoking first and failing
	// to issue the replacement locks the user out permanently — and if
	// that user is the super-admin, the instance has nobody left who can
	// fix it (bootstrap only runs while there are no users at all).
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		if err := repo.RevokeAPIKeyByHash(ctx, keyHash); err != nil {
			return err
		}
		generated, err := authkey.Generate()
		if err != nil {
			return err
		}
		if _, err := repo.CreateRoleAPIKey(ctx, u.ID, authkey.Hash(generated), old.Name, old.AccessRoleID); err != nil {
			return err
		}
		key = generated
		return nil
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// CreateAPIKey issues a new, independent key for u under name, alongside
// any key(s) they already hold. This is how an MCP client gets its own
// credential, separate from the one a terminal uses, so revoking or
// rotating one never touches the other.
func (s *Service) CreateAPIKey(ctx context.Context, u *User, req KeyRequest) (*APIKey, string, error) {
	if u == nil {
		return nil, "", ErrUnauthenticated
	}
	// Left out means the caller's own: a restricted key asking for a key
	// by name alone gets one no wider than itself, not a refusal.
	if req.AccessRoleID == 0 && u.Key.Restricted() {
		req.AccessRoleID = u.Key.AccessRoleID
	}
	if req.AccessRoleID != 0 {
		role, err := s.Repo().RoleByID(ctx, req.AccessRoleID)
		if err != nil {
			return nil, "", err
		}
		// What the new key would reach is its role narrowed by the owner;
		// a restricted key may only hand that out when it reaches it too.
		if u.Key.Restricted() {
			owner := *u
			owner.Key = nil
			base, err := s.PolicyFor(ctx, &owner)
			if err != nil {
				return nil, "", err
			}
			if !Intersect(base, role.Policy()).Within(u.Policy) {
				return nil, "", ErrKeyScopeWider
			}
		}
	}

	generated, err := authkey.Generate()
	if err != nil {
		return nil, "", err
	}
	created, err := s.Repo().CreateRoleAPIKey(ctx, u.ID, authkey.Hash(generated), req.Name, req.AccessRoleID)
	if err != nil {
		return nil, "", err
	}
	return created, generated, nil
}

// KeyRequest is what a new key is called and which role narrows it. An
// AccessRoleID of 0 is as much as the caller has.
type KeyRequest struct {
	Name         string
	AccessRoleID int64
}

// PolicyFor is what u may reach: an admin everything, a member their
// role or the member default, and either narrowed by the key's role.
func (s *Service) PolicyFor(ctx context.Context, u *User) (Policy, error) {
	var base Policy
	if u.Role != RoleAdmin {
		base = DefaultMemberPolicy()
		if u.AccessRoleID != 0 {
			role, err := s.Repo().RoleByID(ctx, u.AccessRoleID)
			if err != nil {
				return nil, err
			}
			base = role.Policy()
		}
	}
	if !u.Key.Restricted() {
		return base, nil
	}
	role, err := s.Repo().RoleByID(ctx, u.Key.AccessRoleID)
	if err != nil {
		return nil, err
	}
	return Intersect(base, role.Policy()), nil
}

// ListAPIKeys returns metadata for every key u holds. The key values
// themselves are never shown again after creation.
func (s *Service) ListAPIKeys(ctx context.Context, u *User) ([]*APIKey, error) {
	if u == nil {
		return nil, ErrUnauthenticated
	}
	return s.Repo().ListAPIKeys(ctx, u.ID)
}

// RevokeAPIKey deletes one of u's own keys by id.
//
// **Including the last one.** It used to refuse that, to stop somebody
// locking themselves out — and that was the wrong trade the moment you
// name the case revocation exists for: a key that has leaked. Under the
// old rule the answer to "this key is in someone else's hands" was to
// mint a second one first, which leaves the leaked key live for as long
// as that takes and is a strange thing to be made to do while you are
// already in a hurry.
//
// The lockout it guarded against is also smaller than it was. An
// account has a password and a session now, so an API key is not the
// only way in; the account screen says plainly when revoking this one
// leaves no way to authenticate, and asks. That is the same shape as
// every other irreversible act here — a confirmation in front of it,
// not a refusal you have to work around.
func (s *Service) RevokeAPIKey(ctx context.Context, u *User, id int64) error {
	if u == nil {
		return ErrUnauthenticated
	}
	// A restricted key revoking its owner's other keys would be a way
	// to lock the owner out from inside a scope meant to contain it.
	if u.Key.Restricted() {
		return ErrKeyRestricted
	}
	keys, err := s.Repo().ListAPIKeys(ctx, u.ID)
	if err != nil {
		return err
	}
	// Confirm id actually belongs to u before anything else: checking
	// "is this the caller's last key" against the caller's OWN key count
	// would be meaningless (and wrongly block or allow) for an id that
	// isn't theirs to begin with — ownership must be settled first.
	owned := false
	for _, k := range keys {
		if k.ID == id {
			owned = true
			break
		}
	}
	if !owned {
		return database.ErrNotFound
	}
	return s.Repo().RevokeAPIKeyByID(ctx, id, u.ID)
}

// DB exposes the connection pool for a module that has to open a
// transaction spanning users and its own tables.
func (s *Service) DB() *database.DB { return s.db }

// --- signing in ---

// Login verifies a username and password and starts a session, returning
// the token the browser carries and the session it belongs to.
//
// Every failure is ErrInvalidCredentials, and an unknown username still
// pays for a hash verification: the response must not say — in its text
// or in its timing — whether an account exists.
func (s *Service) Login(ctx context.Context, username, password string) (string, *Session, error) {
	u, hash, err := s.Repo().PasswordHash(ctx, username)
	if err != nil {
		// No such account. Verify against a fixed hash anyway so this
		// costs what a real attempt costs.
		VerifyPassword(dummyHash, password)
		return "", nil, ErrInvalidCredentials
	}
	if hash == "" {
		// The account exists but has no password — it was created with
		// an API key and has never set one. Same answer.
		VerifyPassword(dummyHash, password)
		return "", nil, ErrInvalidCredentials
	}
	if !VerifyPassword(hash, password) {
		return "", nil, ErrInvalidCredentials
	}
	// Checked after the password, deliberately. Answering "blocked" to
	// a wrong password would tell whoever is guessing that the account
	// exists, which is the one thing every other failure here is
	// shaped to avoid — and answering "wrong password" to the person
	// whose password is right sends them to reset the one thing that
	// was never the problem.
	if u.Blocked() {
		return "", nil, ErrBlocked
	}

	return s.StartSession(ctx, u)
}

// StartSession issues a session for an account whose identity is already
// established. Login calls it after verifying a password; setup calls it
// for the account it just created from one.
func (s *Service) StartSession(ctx context.Context, u *User) (string, *Session, error) {
	token, err := authkey.Generate()
	if err != nil {
		return "", nil, err
	}
	session, err := s.Repo().CreateSession(ctx, authkey.Hash(token), u.ID, time.Now().Add(SessionLifetime))
	if err != nil {
		return "", nil, err
	}
	return token, session, nil
}

// Logout ends one session. A token that matches nothing is not an error:
// the caller wanted to be signed out, and they are.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.Repo().DeleteSession(ctx, authkey.Hash(token))
}

// AuthenticateSession resolves a session token to whoever holds it, and
// records the session as used.
func (s *Service) AuthenticateSession(ctx context.Context, token string) (*User, string, error) {
	tokenHash := authkey.Hash(token)
	u, err := s.Repo().UserBySession(ctx, tokenHash)
	if err != nil {
		return nil, "", ErrNoSession
	}
	// A session started before the block is refused from the next
	// request onwards. Nothing had to be deleted for that — which is
	// what makes unblocking put somebody back exactly where they were.
	if u.Blocked() {
		return nil, "", ErrBlocked
	}
	if u.Policy, err = s.PolicyFor(ctx, u); err != nil {
		return nil, "", err
	}
	// Best effort, like the API key's: a caller whose last_used_at could
	// not be written is still signed in.
	_ = s.Repo().TouchSession(ctx, tokenHash)
	return u, tokenHash, nil
}

// SetPassword sets or changes the caller's own password.
//
// An account that already has one must prove it knows it — otherwise a
// stolen session, or a borrowed terminal, would be enough to lock the
// owner out. An account that has none is setting its first, and the API
// key it authenticated with is the proof.
//
// Every other session the account holds ends: whoever knew the old
// password should not stay signed in.
func (s *Service) SetPassword(ctx context.Context, u *User, currentSessionHash, current, next string) error {
	if u == nil {
		return ErrUnauthenticated
	}
	if u.Key.Restricted() {
		return ErrKeyRestricted
	}

	_, existing, err := s.Repo().PasswordHash(ctx, u.Username)
	if err != nil {
		return err
	}
	if existing != "" && !VerifyPassword(existing, current) {
		return ErrInvalidCredentials
	}

	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		if err := repo.SetPassword(ctx, u.ID, hash); err != nil {
			return err
		}
		return repo.DeleteOtherSessions(ctx, u.ID, currentSessionHash)
	})
}

// CreateWithPassword creates a user who can sign in immediately.
//
// No API key is issued: this account's way in is its password, and a key
// nobody is ever shown would be a live credential lying around for
// nothing. Keys are self-service, created when someone actually wants
// one.
func (s *Service) CreateWithPassword(ctx context.Context, q database.Queryer, username, password string, role Role) (*User, error) {
	// Hash before inserting: a password too short to accept should not
	// leave a user row behind.
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	repo := NewRepository(q)
	u, err := repo.Create(ctx, username, role)
	if err != nil {
		return nil, err
	}
	if err := repo.SetPassword(ctx, u.ID, hash); err != nil {
		return nil, err
	}
	id, err := repo.SystemRoleID(ctx, systemFor(role))
	if err != nil {
		return nil, err
	}
	if err := repo.SetAccessRole(ctx, u.ID, id); err != nil {
		return nil, err
	}
	u.AccessRoleID = id
	return u, nil
}

// PurgeExpiredSessions deletes rows nobody can use. Expiry already takes
// effect at lookup; this only stops the table growing forever.
func (s *Service) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	return s.Repo().DeleteExpiredSessions(ctx)
}

// HasPassword reports whether the caller's account can sign in without
// an API key.
//
// It is the account screen's, and it is there because revoking a key may
// leave none: what that costs depends on whether there is another way
// in, and only the daemon knows.
func (s *Service) HasPassword(ctx context.Context, u *User) (bool, error) {
	if u == nil {
		return false, ErrUnauthenticated
	}
	return s.Repo().HasPassword(ctx, u.ID)
}

// Profile is what an account says about its holder. A nil field is one
// the caller did not send, which is left as it was.
type Profile struct {
	Username    *string
	DisplayName *string
	Email       *string
	Avatar      *string
}

// UpdateProfile changes the caller's own account, the username
// included.
//
// **The caller's own, and nobody else's**, for the reason SetTheme is:
// a name and a face are the person's, and an admin renaming somebody
// else is not administration, it is impersonation with extra steps.
//
// **A username is editable here and nowhere else in this product**, and
// the difference from a slug is worth saying. A project's slug is a
// path component of every app's registry reference underneath it, so
// renaming one silently moves things other people have configured
// against it. A username is written against *this account's own* rows —
// sessions and keys are by id, so both survive — and the one thing it
// breaks belongs to the person doing it: `docker login` sends the
// username with the key, so a push keeps being refused until they log
// in again. That is a consequence to state, which the screen does, not
// a reason to refuse.
func (s *Service) UpdateProfile(ctx context.Context, caller *User, in Profile) (*User, error) {
	if caller == nil {
		return nil, ErrUnauthenticated
	}

	next := *caller
	if in.Username != nil {
		name := strings.TrimSpace(*in.Username)
		if err := ValidUsername(name); err != nil {
			return nil, err
		}
		next.Username = name
	}
	if in.DisplayName != nil {
		name := strings.TrimSpace(*in.DisplayName)
		if len([]rune(name)) > 60 {
			return nil, ErrBadDisplayName
		}
		next.DisplayName = name
	}
	if in.Email != nil {
		address := strings.TrimSpace(*in.Email)
		if !ValidEmail(address) {
			return nil, ErrBadEmail
		}
		next.Email = address
	}
	if in.Avatar != nil {
		if !ValidAvatar(*in.Avatar) {
			return nil, ErrUnknownAvatar
		}
		next.Avatar = *in.Avatar
	}

	updated, err := s.Repo().UpdateProfile(ctx, caller.ID, &next)
	if database.IsUniqueViolation(err) {
		// The unique index decides, not a lookup before it: two people
		// renaming to one name in the same second would both pass a
		// check and one would still have to lose.
		return nil, ErrUsernameTaken
	}
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// SetTheme records which palette the caller sees the dashboard in.
//
// **The caller's own, and nobody else's.** There is no username here on
// purpose: a preference somebody else can change is not a preference,
// and an admin has no business deciding what colour another person's
// screen is.
func (s *Service) SetTheme(ctx context.Context, caller *User, theme string) (*User, error) {
	if caller == nil {
		return nil, ErrUnauthenticated
	}
	if !ValidTheme(theme) {
		return nil, ErrUnknownTheme
	}
	if err := s.Repo().SetTheme(ctx, caller.ID, theme); err != nil {
		return nil, err
	}
	caller.Theme = theme
	return caller, nil
}

// --- access roles ---

// RoleRequest is a role as an editor sends it.
type RoleRequest struct {
	Name        string
	Description string
	Grants      []Grant
}

func (req *RoleRequest) check() error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 60 {
		return ErrRoleName
	}
	if req.Grants == nil {
		req.Grants = []Grant{}
	}
	return ValidateGrants(req.Grants)
}

// ListRoles is every role. Anybody signed in may read them: a member
// choosing a role for their own key has to see what each one reaches.
func (s *Service) ListRoles(ctx context.Context, caller *User) ([]*AccessRole, error) {
	if err := Require(caller, RoleMember); err != nil {
		return nil, err
	}
	return s.Repo().ListRoles(ctx)
}

// CreateRole, like every change to roles, is an admin's: whoever can
// shape access can give themselves all of it.
func (s *Service) CreateRole(ctx context.Context, caller *User, req RoleRequest) (*AccessRole, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	if err := req.check(); err != nil {
		return nil, err
	}
	id, err := s.Repo().CreateRole(ctx, req.Name, req.Description, req.Grants)
	if database.IsUniqueViolation(err) {
		return nil, ErrRoleNameTaken
	}
	if err != nil {
		return nil, err
	}
	return s.Repo().RoleByID(ctx, id)
}

// UpdateRole replaces a role. Everybody holding it reaches the new grants
// from their next request.
func (s *Service) UpdateRole(ctx context.Context, caller *User, id int64, req RoleRequest) (*AccessRole, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	if err := req.check(); err != nil {
		return nil, err
	}
	current, err := s.Repo().RoleByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.System != "" {
		return nil, ErrSystemRole
	}
	err = s.Repo().UpdateRole(ctx, id, req.Name, req.Description, req.Grants)
	if database.IsUniqueViolation(err) {
		return nil, ErrRoleNameTaken
	}
	if err != nil {
		return nil, err
	}
	return s.Repo().RoleByID(ctx, id)
}

func (s *Service) DeleteRole(ctx context.Context, caller *User, id int64) error {
	if err := Require(caller, RoleAdmin); err != nil {
		return err
	}
	role, err := s.Repo().RoleByID(ctx, id)
	if err != nil {
		return err
	}
	if role.System != "" {
		return ErrSystemRole
	}
	if role.Members > 0 || role.Keys > 0 {
		return ErrRoleInUse
	}
	return s.Repo().DeleteRole(ctx, id)
}

// SetAccessRole gives a member a role, or the member default with 0.
func (s *Service) SetAccessRole(ctx context.Context, caller *User, username string, roleID int64) (*User, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	target, err := s.Repo().ByUsername(ctx, username)
	if err != nil {
		return nil, ErrNoSuchUser
	}
	if roleID == 0 {
		if roleID, err = s.Repo().SystemRoleID(ctx, SystemDeploy); err != nil {
			return nil, err
		}
	}
	role, err := s.Repo().RoleByID(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if target.AccessRoleID == role.ID {
		return target, nil
	}
	// Your own is refused for the reason demoting yourself always was:
	// whether anybody is left to put it back is not knowable from here.
	if caller.ID == target.ID {
		return nil, ErrCannotChangeYourOwnRole
	}
	next := RoleMember
	if role.System == SystemAdmin {
		next = RoleAdmin
	}

	var updated *User
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		if err := repo.refuseIfLastAdmin(ctx, target, next != RoleAdmin); err != nil {
			return err
		}
		// users.role follows the role, so everything that asks "is this
		// an admin" goes on getting one answer.
		if err := repo.SetRole(ctx, target.ID, next); err != nil {
			return err
		}
		if err := repo.SetAccessRole(ctx, target.ID, role.ID); err != nil {
			return err
		}
		updated, err = repo.ByID(ctx, target.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// AddWithRole is Add giving the account a role from the list: Admin makes
// an admin, any other a member holding it.
func (s *Service) AddWithRole(ctx context.Context, caller *User, username, password string, roleID int64) (*User, string, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, "", err
	}
	role, err := s.Repo().RoleByID(ctx, roleID)
	if err != nil {
		return nil, "", err
	}
	kind := RoleMember
	if role.System == SystemAdmin {
		kind = RoleAdmin
	}
	created, generated, err := s.Add(ctx, caller, username, password, kind)
	if err != nil {
		return nil, "", err
	}
	if created.AccessRoleID != role.ID {
		if err := s.Repo().SetAccessRole(ctx, created.ID, role.ID); err != nil {
			return nil, "", err
		}
		created.AccessRoleID = role.ID
	}
	return created, generated, nil
}

// RoleNames is every role's name by id, for listings that show one.
func (s *Service) RoleNames(ctx context.Context) map[int64]string {
	out := map[int64]string{}
	roles, err := s.Repo().ListRoles(ctx)
	if err != nil {
		return out
	}
	for _, r := range roles {
		out[r.ID] = r.Name
	}
	return out
}

// RoleName is one role's name, empty for 0 or a role that is gone.
func (s *Service) RoleName(ctx context.Context, id int64) string {
	if id == 0 {
		return ""
	}
	role, err := s.Repo().RoleByID(ctx, id)
	if err != nil {
		return ""
	}
	return role.Name
}

// KeyResponse describes the key a request carried, nil for a session.
func (s *Service) KeyResponse(ctx context.Context, u *User) *KeyResponse {
	if u == nil || u.Key == nil {
		return nil
	}
	return &KeyResponse{Name: u.Key.Name, AccessRole: s.RoleName(ctx, u.Key.AccessRoleID)}
}

// RoleByName finds a role for a caller that names one, as an agent does.
func (s *Service) RoleByName(ctx context.Context, caller *User, name string) (*AccessRole, error) {
	roles, err := s.ListRoles(ctx, caller)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if strings.EqualFold(r.Name, name) {
			return r, nil
		}
	}
	return nil, ErrNoSuchRole
}
