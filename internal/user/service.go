package user

import (
	"context"

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
func (s *Service) CreateWithAPIKey(ctx context.Context, q database.Queryer, username string, isSuperAdmin bool) (*User, string, error) {
	repo := NewRepository(q)
	u, err := repo.Create(ctx, username, isSuperAdmin)
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
