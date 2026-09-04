package store

import (
	"context"
	"errors"
	"testing"
)

func TestWithTxCommitsOnSuccess(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")

	err := s.WithTx(ctx, func(tx *Tx) error {
		user, err := tx.CreateUser(ctx, "employee1", false)
		if err != nil {
			return err
		}
		if err := tx.AddMembership(ctx, user.ID, org.ID, RoleMember); err != nil {
			return err
		}
		_, err = tx.CreateAPIKey(ctx, user.ID, "hash-1", "default")
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	user, err := s.GetUserByUsername(ctx, "employee1")
	if err != nil {
		t.Fatalf("expected the committed user to exist: %v", err)
	}
	if role, err := s.GetMembership(ctx, user.ID, org.ID); err != nil || role != RoleMember {
		t.Fatalf("expected the committed membership, got %q (%v)", role, err)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, "hash-1"); err != nil {
		t.Fatalf("expected the committed API key to resolve: %v", err)
	}
}

// A failure partway through user creation used to leave an orphaned user
// behind: no membership, no key, and a username permanently taken with no
// way to recover through the API.
func TestWithTxRollsBackPartialWork(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	boom := errors.New("boom")

	err := s.WithTx(ctx, func(tx *Tx) error {
		user, err := tx.CreateUser(ctx, "employee1", false)
		if err != nil {
			return err
		}
		if err := tx.AddMembership(ctx, user.ID, org.ID, RoleMember); err != nil {
			return err
		}
		// Stands in for CreateAPIKey failing after the user exists.
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected WithTx to return the callback's error, got %v", err)
	}

	if _, err := s.GetUserByUsername(ctx, "employee1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the half-created user to be rolled back, got %v", err)
	}
	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected no users after a rolled-back transaction, got %d", n)
	}
}

// The transaction must see its own uncommitted rows — a create-then-read
// sequence inside one WithTx is exactly what user creation does.
func TestTxSeesItsOwnUncommittedWrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")

	err := s.WithTx(ctx, func(tx *Tx) error {
		user, err := tx.CreateUser(ctx, "employee1", false)
		if err != nil {
			return err
		}
		if _, err := tx.GetUserByUsername(ctx, "employee1"); err != nil {
			return err
		}
		if err := tx.AddMembership(ctx, user.ID, org.ID, RoleAdmin); err != nil {
			return err
		}
		role, err := tx.GetMembership(ctx, user.ID, org.ID)
		if err != nil {
			return err
		}
		if role != RoleAdmin {
			t.Fatalf("expected admin, got %q", role)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}

func TestTxGetUserByUsernameReportsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.GetUserByUsername(ctx, "nobody")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for an unknown username, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}
