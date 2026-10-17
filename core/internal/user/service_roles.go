package user

import (
	"context"
	"strings"

	"cubeship/internal/platform/database"
)

// UpdateProfile changes the caller's own account, the username
// included.
//
// **The caller's own, and nobody else's**, for the reason SetTheme is:
// a name and a face are the person's, and an admin renaming somebody
// else is not administration, it is impersonation with extra steps.
//
// **A username is editable here and nowhere else in this product**, and
// the difference from a slug is worth saying. A project's slug is a
// path component of every app's registry reference underneath it, so
// renaming one silently moves things other people have configured
// against it. A username is written against *this account's own* rows —
// sessions and keys are by id, so both survive — and the one thing it
// breaks belongs to the person doing it: `docker login` sends the
// username with the key, so a push keeps being refused until they log
// in again. That is a consequence to state, which the screen does, not
// a reason to refuse.
func (s *Service) UpdateProfile(ctx context.Context, caller *User, in Profile) (*User, error) {
	if caller == nil {
		return nil, ErrUnauthenticated
	}

	next := *caller
	if in.Username != nil {
		name := strings.TrimSpace(*in.Username)
		if err := ValidUsername(name); err != nil {
			return nil, err
		}
		next.Username = name
	}
	if in.DisplayName != nil {
		name := strings.TrimSpace(*in.DisplayName)
		if len([]rune(name)) > 60 {
			return nil, ErrBadDisplayName
		}
		next.DisplayName = name
	}
	if in.Email != nil {
		address := strings.TrimSpace(*in.Email)
		if !ValidEmail(address) {
			return nil, ErrBadEmail
		}
		next.Email = address
	}
	if in.Avatar != nil {
		if !ValidAvatar(*in.Avatar) {
			return nil, ErrUnknownAvatar
		}
		next.Avatar = *in.Avatar
	}

	updated, err := s.Repo().UpdateProfile(ctx, caller.ID, &next, in.Avatar != nil)
	if database.IsUniqueViolation(err) {
		// The unique index decides, not a lookup before it: two people
		// renaming to one name in the same second would both pass a
		// check and one would still have to lose.
		return nil, ErrUsernameTaken
	}
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// SetTheme records which palette the caller sees the dashboard in.
//
// **The caller's own, and nobody else's.** There is no username here on
// purpose: a preference somebody else can change is not a preference,
// and an admin has no business deciding what colour another person's
// screen is.
func (s *Service) SetTheme(ctx context.Context, caller *User, theme string) (*User, error) {
	if caller == nil {
		return nil, ErrUnauthenticated
	}
	if !ValidTheme(theme) {
		return nil, ErrUnknownTheme
	}
	if err := s.Repo().SetTheme(ctx, caller.ID, theme); err != nil {
		return nil, err
	}
	caller.Theme = theme
	return caller, nil
}

// --- access roles ---

// RoleRequest is a role as an editor sends it.
type RoleRequest struct {
	Name        string
	Description string
	Grants      []Grant
}

func (req *RoleRequest) check() error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 60 {
		return ErrRoleName
	}
	if req.Grants == nil {
		req.Grants = []Grant{}
	}
	return ValidateGrants(req.Grants)
}

// ListRoles is every role. Anybody signed in may read them: a member
// choosing a role for their own key has to see what each one reaches.
func (s *Service) ListRoles(ctx context.Context, caller *User) ([]*AccessRole, error) {
	if err := Require(caller, RoleMember); err != nil {
		return nil, err
	}
	return s.Repo().ListRoles(ctx)
}

// CreateRole, like every change to roles, is an admin's: whoever can
// shape access can give themselves all of it.
func (s *Service) CreateRole(ctx context.Context, caller *User, req RoleRequest) (*AccessRole, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	if err := req.check(); err != nil {
		return nil, err
	}
	id, err := s.Repo().CreateRole(ctx, req.Name, req.Description, req.Grants)
	if database.IsUniqueViolation(err) {
		return nil, ErrRoleNameTaken
	}
	if err != nil {
		return nil, err
	}
	return s.Repo().RoleByID(ctx, id)
}

// UpdateRole replaces a role. Everybody holding it reaches the new grants
// from their next request.
func (s *Service) UpdateRole(ctx context.Context, caller *User, id int64, req RoleRequest) (*AccessRole, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	if err := req.check(); err != nil {
		return nil, err
	}
	current, err := s.Repo().RoleByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.System != "" {
		return nil, ErrSystemRole
	}
	err = s.Repo().UpdateRole(ctx, id, req.Name, req.Description, req.Grants)
	if database.IsUniqueViolation(err) {
		return nil, ErrRoleNameTaken
	}
	if err != nil {
		return nil, err
	}
	return s.Repo().RoleByID(ctx, id)
}

func (s *Service) DeleteRole(ctx context.Context, caller *User, id int64) error {
	if err := Require(caller, RoleAdmin); err != nil {
		return err
	}
	role, err := s.Repo().RoleByID(ctx, id)
	if err != nil {
		return err
	}
	if role.System != "" {
		return ErrSystemRole
	}
	if role.Members > 0 || role.Keys > 0 {
		return ErrRoleInUse
	}
	return s.Repo().DeleteRole(ctx, id)
}

// SetAccessRole gives a member a role, or the member default with 0.
func (s *Service) SetAccessRole(ctx context.Context, caller *User, username string, roleID int64) (*User, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, err
	}
	target, err := s.Repo().ByUsername(ctx, username)
	if err != nil {
		return nil, ErrNoSuchUser
	}
	if roleID == 0 {
		if roleID, err = s.Repo().SystemRoleID(ctx, SystemDeploy); err != nil {
			return nil, err
		}
	}
	role, err := s.Repo().RoleByID(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if target.AccessRoleID == role.ID {
		return target, nil
	}
	// Your own is refused for the reason demoting yourself always was:
	// whether anybody is left to put it back is not knowable from here.
	if caller.ID == target.ID {
		return nil, ErrCannotChangeYourOwnRole
	}
	next := RoleMember
	if role.System == SystemAdmin {
		next = RoleAdmin
	}

	var updated *User
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		if err := repo.refuseIfLastAdmin(ctx, target, next != RoleAdmin); err != nil {
			return err
		}
		// users.role follows the role, so everything that asks "is this
		// an admin" goes on getting one answer.
		if err := repo.SetRole(ctx, target.ID, next); err != nil {
			return err
		}
		if err := repo.SetAccessRole(ctx, target.ID, role.ID); err != nil {
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

// AddWithRole is Add giving the account a role from the list: Admin makes
// an admin, any other a member holding it.
func (s *Service) AddWithRole(ctx context.Context, caller *User, username, password string, roleID int64) (*User, string, error) {
	if err := Require(caller, RoleAdmin); err != nil {
		return nil, "", err
	}
	role, err := s.Repo().RoleByID(ctx, roleID)
	if err != nil {
		return nil, "", err
	}
	kind := RoleMember
	if role.System == SystemAdmin {
		kind = RoleAdmin
	}
	created, generated, err := s.Add(ctx, caller, username, password, kind)
	if err != nil {
		return nil, "", err
	}
	if created.AccessRoleID != role.ID {
		if err := s.Repo().SetAccessRole(ctx, created.ID, role.ID); err != nil {
			return nil, "", err
		}
		created.AccessRoleID = role.ID
	}
	return created, generated, nil
}

// RoleNames is every role's name by id, for listings that show one.
func (s *Service) RoleNames(ctx context.Context) map[int64]string {
	out := map[int64]string{}
	roles, err := s.Repo().ListRoles(ctx)
	if err != nil {
		return out
	}
	for _, r := range roles {
		out[r.ID] = r.Name
	}
	return out
}

// RoleName is one role's name, empty for 0 or a role that is gone.
func (s *Service) RoleName(ctx context.Context, id int64) string {
	if id == 0 {
		return ""
	}
	role, err := s.Repo().RoleByID(ctx, id)
	if err != nil {
		return ""
	}
	return role.Name
}

// KeyResponse describes the key a request carried, nil for a session.
func (s *Service) KeyResponse(ctx context.Context, u *User) *KeyResponse {
	if u == nil || u.Key == nil {
		return nil
	}
	return &KeyResponse{Name: u.Key.Name, AccessRole: s.RoleName(ctx, u.Key.AccessRoleID)}
}

// RoleByName finds a role for a caller that names one, as an agent does.
func (s *Service) RoleByName(ctx context.Context, caller *User, name string) (*AccessRole, error) {
	roles, err := s.ListRoles(ctx, caller)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if strings.EqualFold(r.Name, name) {
			return r, nil
		}
	}
	return nil, ErrNoSuchRole
}
