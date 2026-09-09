package node

import (
	"context"
	"fmt"

	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Service is the cluster: which machines are in it, and what the
// workers are told when they call.
//
// It sits at the bottom with `user` and knows about no other module.
// What runs *on* a node is the placing module's business, and it will
// reach this one through the Desired the agent already asks for.
type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service { return &Service{db: db} }

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Add puts a machine in the cluster and mints the credential its agent
// will authenticate with.
//
// **The credential is returned once and cannot be read again**, like an
// API key: only its hash is stored. Losing it means removing the node
// and adding it back, which is the same cost as rotating it and one
// fewer thing to build.
//
// Nothing is contacted. The row is a place for a machine that does not
// exist yet — somebody now goes and runs the installer on it — and a
// node that has never called is `pending` rather than an error.
func (s *Service) Add(ctx context.Context, caller *user.User, name, description string) (*Node, string, error) {
	if err := user.Require(caller, RoleToManage); err != nil {
		return nil, "", err
	}
	if err := checkSlug(name); err != nil {
		return nil, "", err
	}
	token, err := authkey.Generate()
	if err != nil {
		return nil, "", fmt.Errorf("generate the server's credential: %w", err)
	}
	created, err := s.Repo().Create(ctx, name, description, authkey.Hash(token))
	if err != nil {
		return nil, "", err
	}
	return created, token, nil
}

// List is the cluster, this machine included.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Node, error) {
	if err := user.Require(caller, RoleToRead); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

// Get is one machine by name.
func (s *Service) Get(ctx context.Context, caller *user.User, name string) (*Node, error) {
	if err := user.Require(caller, RoleToRead); err != nil {
		return nil, err
	}
	return s.Repo().BySlug(ctx, name)
}

// Remove takes a machine out of the cluster.
//
// **It is a local act.** The row goes and the credential with it, so
// the agent's next call is refused and the worker stops being told
// anything — but nothing reaches out to that box, and whatever is
// running on it goes on running until somebody stops it. That is the
// honest shape for a machine that may be unreachable, off, or gone:
// the alternative is a delete that hangs on a host nobody can dial.
//
// The control plane cannot be removed. It is not a machine this
// instance joined; it is the instance.
func (s *Service) Remove(ctx context.Context, caller *user.User, name string) (*Node, error) {
	if err := user.Require(caller, RoleToManage); err != nil {
		return nil, err
	}
	n, err := s.Repo().BySlug(ctx, name)
	if err != nil {
		return nil, err
	}
	if n.ControlPlane {
		return nil, ErrControlPlane
	}
	if err := s.Repo().Delete(ctx, n.ID); err != nil {
		return nil, err
	}
	return n, nil
}

// Authenticate turns an agent's credential into the node it belongs to.
//
// It takes no caller and answers no question about a person: a node is
// not one. This is the whole of the agent surface's authorization —
// holding the credential *is* being that machine, and every reconcile
// is scoped to the node it resolved to.
func (s *Service) Authenticate(ctx context.Context, token string) (*Node, error) {
	if token == "" {
		return nil, ErrUnknownToken
	}
	n, err := s.Repo().ByTokenHash(ctx, authkey.Hash(token))
	if err != nil {
		return nil, err
	}
	// A credential that resolves to the control plane would mean this
	// machine dialling itself. Nothing mints one — the row's token is
	// NULL — so this is a belt-and-braces refusal rather than a case
	// anybody can reach.
	if n.ControlPlane {
		return nil, ErrUnknownToken
	}
	return n, nil
}

// Reconcile is one pass of the agent loop: the machine says what it is,
// and is told what it should be running.
//
// The report is recorded before the answer is built, so a node that
// calls is marked seen even if working out its desired state fails.
// Being seen is a fact about the network; what it should run is a
// question about this instance, and one must not be lost with the
// other.
//
// **The answer is empty in this release.** See Desired: the loop exists
// now so that placing an app on a node is filling it in rather than
// inventing a way to reach the machine.
func (s *Service) Reconcile(ctx context.Context, n *Node, rep Report) (Desired, error) {
	if err := s.Repo().Record(ctx, n.ID, rep); err != nil {
		return Desired{}, err
	}
	return Desired{Apps: []Placement{}}, nil
}

func checkSlug(name string) error {
	if slug.Reserved(name) {
		return slug.ErrReserved
	}
	if reservedSlugs[name] {
		return ErrReservedSlug
	}
	if name == ControlPlaneSlug {
		return ErrControlPlane
	}
	if !slug.Valid(name) {
		return slug.ErrInvalid
	}
	return nil
}
