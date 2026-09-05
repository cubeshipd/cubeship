package user

import (
	"context"
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

// Add creates an account and its first API key, and is the only way one
// is made after setup. Admin only: an account is a way into this
// instance, so handing out the ability to mint them would hand out the
// instance.
//
// The key is shown exactly once, here. There is no second endpoint that
// reveals it, and no password: an account gets one when it sets one.
//
// One transaction for both halves. A user created without a key would
// hold their username forever with no way to finish or undo it through
// the API.
func (s *Service) Add(ctx context.Context, caller *User, username string, role Role) (*User, string, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, "", err
	}
	if !role.Valid() {
		return nil, "", ErrInvalidRole
	}

	var created *User
	var key string
	err := s.db.WithTx(ctx, func(tx database.Queryer) error {
		u, k, err := s.CreateWithAPIKey(ctx, tx, username, role)
		if database.IsUniqueViolation(err) {
			// Another request took this username between the caller
			// typing it and here. Both cannot own it; the loser is told
			// so rather than shown a driver error.
			return ErrUsernameTaken
		}
		if err != nil {
			return err
		}
		created, key = u, k
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return created, key, nil
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
		if target.Role == RoleAdmin {
			// Counted inside the transaction: two requests each
			// deleting one of the last two admins would otherwise both
			// pass a check taken outside it and leave none.
			admins, err := repo.CountByRole(ctx, RoleAdmin)
			if err != nil {
				return err
			}
			if admins <= 1 {
				return ErrLastAdmin
			}
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
		if _, err := repo.CreateAPIKey(ctx, u.ID, authkey.Hash(generated), old.Name); err != nil {
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
func (s *Service) CreateAPIKey(ctx context.Context, u *User, name string) (*APIKey, string, error) {
	if u == nil {
		return nil, "", ErrUnauthenticated
	}
	generated, err := authkey.Generate()
	if err != nil {
		return nil, "", err
	}
	created, err := s.Repo().CreateAPIKey(ctx, u.ID, authkey.Hash(generated), name)
	if err != nil {
		return nil, "", err
	}
	return created, generated, nil
}

// ListAPIKeys returns metadata for every key u holds. The key values
// themselves are never shown again after creation.
func (s *Service) ListAPIKeys(ctx context.Context, u *User) ([]*APIKey, error) {
	if u == nil {
		return nil, ErrUnauthenticated
	}
	return s.Repo().ListAPIKeys(ctx, u.ID)
}

// RevokeAPIKey deletes one of u's own keys by id. It refuses when id is
// their last remaining key: deleting it would lock them out with no way
// back short of a super-admin re-adding them (or, if they are the
// super-admin, nothing at all).
func (s *Service) RevokeAPIKey(ctx context.Context, u *User, id int64) error {
	if u == nil {
		return ErrUnauthenticated
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
	if len(keys) <= 1 {
		return ErrLastAPIKey
	}
	return s.Repo().RevokeAPIKeyByID(ctx, id, u.ID)
}

// CreateWithAPIKey creates a user and issues their first key in one
// transaction, returning the plaintext key. A user that exists with no
// key would hold their username forever with no way to finish or undo it
// through the API.
//
// It takes a Queryer so a caller already inside a transaction — adding a
// user to an organization, which must also write a membership — can make
// the whole thing atomic. Pass s.db to run it standalone.
func (s *Service) CreateWithAPIKey(ctx context.Context, q database.Queryer, username string, role Role) (*User, string, error) {
	repo := NewRepository(q)
	u, err := repo.Create(ctx, username, role)
	if err != nil {
		return nil, "", err
	}
	key, err := authkey.Generate()
	if err != nil {
		return nil, "", err
	}
	if _, err := repo.CreateAPIKey(ctx, u.ID, authkey.Hash(key), DefaultAPIKeyName); err != nil {
		return nil, "", err
	}
	return u, key, nil
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
	return u, nil
}

// PurgeExpiredSessions deletes rows nobody can use. Expiry already takes
// effect at lookup; this only stops the table growing forever.
func (s *Service) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	return s.Repo().DeleteExpiredSessions(ctx)
}
