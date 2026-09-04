package api

import (
	"context"
	"errors"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

// errAlreadyMember reports that the named user already belongs to the
// target organization, so there is nothing to add.
var errAlreadyMember = errors.New("user is already a member of this organization")

// addOrgUser adds username to org with role, creating the user (and
// their first API key) if this is the first organization they've been
// added to. An existing username gains a membership instead of
// colliding on the unique index — users belong to as many organizations
// as they are added to, each with its own role.
//
// The returned API key is empty when an existing user was added to a
// further organization — that user keeps the key they already have.
// Shared between handleCreateOrgUser (HTTP) and the MCP create_org_user
// tool.
func (s *Server) addOrgUser(ctx context.Context, org *store.Organization, username string, role store.Role) (apiKey string, err error) {
	// One transaction for the whole thing: a user created without a
	// membership or a key would hold their username forever with no way
	// to finish or undo it through the API.
	err = s.store.WithTx(ctx, func(tx *store.Tx) error {
		existing, err := tx.GetUserByUsername(ctx, username)
		switch {
		case err == nil:
			if _, err := tx.GetMembership(ctx, existing.ID, org.ID); err == nil {
				return errAlreadyMember
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			return tx.AddMembership(ctx, existing.ID, org.ID, role)
		case !errors.Is(err, store.ErrNotFound):
			return err
		}

		user, err := tx.CreateUser(ctx, username, false)
		if err != nil {
			return err
		}
		if err := tx.AddMembership(ctx, user.ID, org.ID, role); err != nil {
			return err
		}
		key, err := authkey.Generate()
		if err != nil {
			return err
		}
		if _, err := tx.CreateAPIKey(ctx, user.ID, authkey.Hash(key), store.DefaultAPIKeyName); err != nil {
			return err
		}
		apiKey = key
		return nil
	})
	if err != nil {
		return "", err
	}
	return apiKey, nil
}
