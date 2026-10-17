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
	db     *database.DB
	logins *loginAdmission
}

func NewService(db *database.DB) *Service {
	return &Service{db: db, logins: newLoginAdmission(time.Now)}
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
	// that user is the last admin, the instance has nobody left who can
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
