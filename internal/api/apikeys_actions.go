package api

import (
	"context"
	"errors"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

// errLastAPIKey reports that a revoke was refused because it would have
// left the caller with no way to authenticate at all.
var errLastAPIKey = errors.New("cannot revoke your only remaining API key")

// Core API-key actions, shared between the HTTP handlers (user_handlers.go)
// and the MCP tools (mcp_handlers.go) — every path that lets a caller
// manage their own credentials goes through exactly these.

// rotateAPIKey replaces the key identified by keyHash with a freshly
// generated one, keeping its name, and returns the new plaintext key.
// Every OTHER key the same user holds is left untouched.
func (s *Server) rotateAPIKey(ctx context.Context, user *store.User, keyHash string) (string, error) {
	oldKey, err := s.store.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		return "", err
	}

	var key string
	// Revoke and reissue in one transaction. Revoking first and failing
	// to issue the replacement locks the user out permanently — and if
	// that user is the super-admin, the instance has nobody left who can
	// fix it (bootstrap only runs while there are no users at all).
	err = s.store.WithTx(ctx, func(tx *store.Tx) error {
		if err := tx.RevokeAPIKeyByHash(ctx, keyHash); err != nil {
			return err
		}
		generated, err := authkey.Generate()
		if err != nil {
			return err
		}
		if _, err := tx.CreateAPIKey(ctx, user.ID, authkey.Hash(generated), oldKey.Name); err != nil {
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

// createAdditionalAPIKey issues a new, independent key for user under
// name, alongside any key(s) they already hold.
func (s *Server) createAdditionalAPIKey(ctx context.Context, user *store.User, name string) (*store.APIKey, string, error) {
	generated, err := authkey.Generate()
	if err != nil {
		return nil, "", err
	}
	created, err := s.store.CreateAPIKey(ctx, user.ID, authkey.Hash(generated), name)
	if err != nil {
		return nil, "", err
	}
	return created, generated, nil
}

// revokeAPIKey deletes one of user's own keys by id. Refuses when id is
// their last remaining key: deleting it would lock them out with no way
// back in short of a super-admin re-adding them (or, if they are the
// super-admin, nothing at all).
func (s *Server) revokeAPIKey(ctx context.Context, user *store.User, id int64) error {
	keys, err := s.store.ListAPIKeysForUser(ctx, user.ID)
	if err != nil {
		return err
	}
	// Confirm id actually belongs to user before anything else: checking
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
		return store.ErrNotFound
	}
	if len(keys) <= 1 {
		return errLastAPIKey
	}
	return s.store.RevokeAPIKeyByID(ctx, id, user.ID)
}
