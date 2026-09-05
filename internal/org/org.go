// Package org owns organizations, who belongs to them, and with what
// role — which makes it the module every other one asks for
// authorization. An app, project or environment is reachable exactly when
// the caller has a role in the organization that owns it.
package org

import (
	"context"
	"errors"
	"time"
)

// Organization is a tenant on this instance. Its slug becomes a path
// component of every registry image its apps push to.
// An organization has no display name. Its slug is its name — the same
// rule an app has always followed, and for the same reason: the slug is
// a path component of every registry reference underneath it, so it is
// the one identifier that cannot change and the one everybody reads.
// A second, editable name was a second idea for one thing, asked for at
// creation and drifting apart afterwards.
type Organization struct {
	ID        int64
	Slug      string
	CreatedAt time.Time
}

// Role is what a user may do inside one organization.
type Role string

const (
	// RoleAdmin can add users to the organization, create projects and
	// environments, and everything a member can.
	RoleAdmin Role = "admin"
	// RoleMember can create, deploy, configure and read the logs of the
	// organization's apps.
	RoleMember Role = "member"
)

// Valid reports whether r is a role this instance recognizes.
func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleMember
}

// Membership is one user's place in one organization.
type Membership struct {
	OrgID   int64
	OrgSlug string
	Role    Role
}

var (
	// ErrNotFound is returned for an organization that doesn't exist —
	// or that the caller may not see. The two are deliberately the same
	// error, so a response never confirms that another tenant's
	// organization exists.
	ErrNotFound = errors.New("organization not found")

	// ErrForbidden is returned when the caller exists in the
	// organization but lacks the role an action requires.
	ErrForbidden = errors.New("forbidden: you do not have the required role in this organization")

	// ErrSuperAdminOnly guards the actions only the VPS operator may take.
	ErrSuperAdminOnly = errors.New("forbidden: only a super-admin can create organizations")

	// ErrAlreadyExists reports a slug already taken on this instance.
	ErrAlreadyExists = errors.New("organization already exists")

	// ErrAlreadyMember reports that the named user already belongs to the
	// target organization, so there is nothing to add.
	ErrAlreadyMember = errors.New("user is already a member of this organization")

	// ErrInvalidRole reports a role string that is neither admin nor member.
	ErrInvalidRole = errors.New(`role must be "admin" or "member"`)

	// ErrUsernameTaken reports that the username was claimed by a
	// concurrent request while this one was working.
	ErrUsernameTaken = errors.New("that username was just taken; try again")

	// ErrNoTeardown reports that the app module was never wired in, so a
	// delete cannot know what it would leave running. Refusing is the
	// only safe answer: rows would go and containers would stay.
	ErrNoTeardown = errors.New("cannot delete: the app module is not wired in")
)

// AppTeardown removes the apps under something being deleted — their
// containers first, then their rows.
//
// Deleting an organization or a project takes everything under it with
// it, and stopping a container is the app module's job. Dependencies run
// one way, so org knows nothing of app: the app service is handed in at
// wiring time, and internal/project reaches this same interface through
// the org package it already depends on.
//
// Authorization has already happened by the time one of these is called
// — the caller had to be able to delete the thing above — so none of
// them takes a caller.
type AppTeardown interface {
	DeleteAppsInOrg(ctx context.Context, orgID int64) error
	DeleteAppsInProject(ctx context.Context, projectID int64) error
	DeleteAppsInEnvironment(ctx context.Context, environmentID int64) error
}
