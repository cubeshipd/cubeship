package user

import (
	"context"
	"time"

	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database"
)

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

// ConfirmPassword asks a signed-in person for their password again, for
// the one thing that deserves it: a root shell on a machine. A session
// cookie proves somebody signed in once; this proves they are still the
// one at the keyboard. Throttled like a sign-in, because it is one.
func (s *Service) ConfirmPassword(ctx context.Context, u *User, password string) error {
	if u == nil {
		return ErrUnauthenticated
	}
	if len(password) > MaxPasswordBytes {
		return ErrInvalidCredentials
	}
	release, err := s.logins.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	_, hash, err := s.Repo().PasswordHash(ctx, u.Username)
	if err != nil || hash == "" {
		VerifyPassword(dummyHash, password)
		return ErrInvalidCredentials
	}
	if !VerifyPassword(hash, password) {
		return ErrInvalidCredentials
	}
	return nil
}

// Login verifies a username and password and starts a session, returning
// the token the browser carries and the session it belongs to.
//
// Credential failures are ErrInvalidCredentials, and an admitted unknown
// username still pays for a hash verification: the response must not say
// whether an account exists. Admission rejects excess attempts before any
// database lookup or hash allocation.
func (s *Service) Login(ctx context.Context, username, password string) (string, *Session, error) {
	// Keep historical names usable even though new usernames are limited to 32.
	if len(username) > 255 || len(password) > MaxPasswordBytes {
		return "", nil, ErrInvalidCredentials
	}
	release, err := s.logins.acquire(ctx)
	if err != nil {
		return "", nil, err
	}
	defer release()

	u, hash, err := s.Repo().PasswordHash(ctx, username)
	if ctx.Err() != nil {
		return "", nil, ctx.Err()
	}
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
