package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

// errAlreadyMember reports that the named user already belongs to the
// target organization, so there is nothing to add.
var errAlreadyMember = errors.New("user is already a member of this organization")

type createOrgUserResponse struct {
	Username string `json:"username"`
	Org      string `json:"org"`
	Role     string `json:"role"`
	// APIKey is the new user's key, shown exactly once. It is omitted
	// when an existing user was added to a further organization — that
	// user keeps the key they already have.
	APIKey string `json:"api_key,omitempty"`
}

// handleCreateOrgUser adds a user to an organization, creating them if
// this is their first one. A username that already exists gains a
// membership rather than colliding on the unique index: users belong to
// as many organizations as they are added to, each with its own role.
func (s *Server) handleCreateOrgUser(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	org, err := s.store.GetOrganizationBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}
	if !s.authorizeOrg(r, org.ID, store.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}
	role := store.Role(req.Role)
	if role != store.RoleAdmin && role != store.RoleMember {
		http.Error(w, "role must be \"admin\" or \"member\"", http.StatusBadRequest)
		return
	}

	resp := createOrgUserResponse{Username: req.Username, Org: org.Slug, Role: string(role)}
	// One transaction for the whole thing: a user created without a
	// membership or a key would hold their username forever with no way
	// to finish or undo it through the API.
	err = s.store.WithTx(r.Context(), func(tx *store.Tx) error {
		existing, err := tx.GetUserByUsername(r.Context(), req.Username)
		switch {
		case err == nil:
			if _, err := tx.GetMembership(r.Context(), existing.ID, org.ID); err == nil {
				return errAlreadyMember
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			return tx.AddMembership(r.Context(), existing.ID, org.ID, role)
		case !errors.Is(err, store.ErrNotFound):
			return err
		}

		user, err := tx.CreateUser(r.Context(), req.Username, false)
		if err != nil {
			return err
		}
		if err := tx.AddMembership(r.Context(), user.ID, org.ID, role); err != nil {
			return err
		}
		key, err := authkey.Generate()
		if err != nil {
			return err
		}
		if _, err := tx.CreateAPIKey(r.Context(), user.ID, authkey.Hash(key)); err != nil {
			return err
		}
		resp.APIKey = key
		return nil
	})
	if errors.Is(err, errAlreadyMember) {
		http.Error(w, errAlreadyMember.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var key string
	// Revoke and reissue in one transaction. Revoking first and failing
	// to issue the replacement locks the user out permanently — and if
	// that user is the super-admin, the instance has nobody left who can
	// fix it (bootstrap only runs while there are no users at all).
	err := s.store.WithTx(r.Context(), func(tx *store.Tx) error {
		if err := tx.RevokeAPIKeysForUser(r.Context(), user.ID); err != nil {
			return err
		}
		generated, err := authkey.Generate()
		if err != nil {
			return err
		}
		if _, err := tx.CreateAPIKey(r.Context(), user.ID, authkey.Hash(generated)); err != nil {
			return err
		}
		key = generated
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
}

// handleWhoAmI reports the identity of the caller's own API key. The
// CLI's `registry login` uses this to learn the username to
// authenticate the registry's per-user token auth with — the saved
// credentials file only ever stored the key itself, never the
// username.
func (s *Server) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":       user.Username,
		"is_super_admin": user.IsSuperAdmin,
	})
}
