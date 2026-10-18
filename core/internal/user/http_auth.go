package user

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/slug"
)

// UserResponse is one account as the API returns it. Never a hash, and
// never a key: those are shown once, at creation.
type UserResponse struct {
	Username string `json:"username"`
	Role     Role   `json:"role"`
	// Theme is the chosen base and accent. Absent for dark-cyan.
	Theme string `json:"theme,omitempty"`
	// What the account says about its holder. Absent when unset, which
	// is the normal state — a username is what identifies somebody
	// here, and these are what a username cannot carry.
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Avatar      string `json:"avatar"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	// BlockedAt is when this account was shut out, absent while it is
	// not. **One field rather than a flag and a date**: the two would
	// be one fact with two places to disagree about it, and when it
	// happened is the first thing anybody asks. Its credentials are all
	// still there — see Service.SetBlocked.
	BlockedAt *time.Time `json:"blocked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	// AccessRoleID is a member's role, absent for the member default.
	AccessRoleID int64 `json:"access_role_id,omitempty"`
}

// userResponseFor is the whole of an account, and it is one function
// because there is one shape.
//
// The listing used to build its own with three fields in it, so
// `/users` answered without the face, the name or whether the account
// was blocked — every one of which is a column on the screen that
// listing is for. A second literal of the same struct is a second
// answer to what an account is.
func userResponseFor(u *User) UserResponse {
	return UserResponse{
		Username: u.Username, Role: u.Role, Theme: u.Theme,
		DisplayName: u.DisplayName, Email: u.Email, Avatar: u.Avatar, AvatarURL: u.AvatarURL(),
		BlockedAt: u.BlockedAt, CreatedAt: u.CreatedAt, AccessRoleID: u.AccessRoleID,
	}
}

// setPreferences changes what the caller has chosen about their own
// view of the instance.
//
// **The caller's own, and no path parameter to say otherwise.** A
// preference somebody else can change is not a preference, and an
// admin has no business deciding what colour another person's screen
// is.
func (h *Handler) setPreferences(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// Theme is one of the combinations in the themes list on GET
		// /users/me. Empty selects the default dark-cyan combination.
		Theme *string `json:"theme"`
		// The rest is what the account says about its holder. Every one
		// of them is a pointer: an absent field is left alone, and an
		// empty one clears it — which is the only way to say "no
		// display name" that a form can send.
		Username    *string `json:"username"`
		DisplayName *string `json:"display_name"`
		Email       *string `json:"email"`
		Avatar      *string `json:"avatar"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	profile := req.Username != nil || req.DisplayName != nil || req.Email != nil ||
		req.Avatar != nil
	if req.Theme == nil && !profile {
		http.Error(w, "nothing to change", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	updated := FromContext(ctx)
	var err error
	// The theme first, because it is the one that cannot fail on
	// anything the rest might: a body carrying both should not leave a
	// palette applied and a name refused, and this order at least
	// leaves the two writes in the order the caller reads them back.
	if req.Theme != nil {
		updated, err = h.svc.SetTheme(ctx, updated, *req.Theme)
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	if profile {
		updated, err = h.svc.UpdateProfile(ctx, updated, Profile{
			Username: req.Username, DisplayName: req.DisplayName,
			Email: req.Email, Avatar: req.Avatar,
		})
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, UserResponse{
		Username: updated.Username, Role: updated.Role,
		Theme: updated.Theme, CreatedAt: updated.CreatedAt,
		DisplayName: updated.DisplayName, Email: updated.Email, Avatar: updated.Avatar, AvatarURL: updated.AvatarURL(),
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	users, err := h.svc.List(ctx, FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]UserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, userResponseFor(u))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": out})
}

// remove deletes an account and everything it authenticates with.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.svc.Delete(ctx, FromContext(ctx), r.PathValue("username")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateUser changes what an admin decides about somebody else's
// account: what they may do, and whether they may get in at all.
//
// **One endpoint for both, because they are the same kind of act** —
// an admin deciding about an account that is not theirs — and both are
// a field on the same row. Neither is a preference: what is the
// account holder's own is on PATCH /users/me, and there is no path
// parameter there for exactly this reason.
//
// Each field is a pointer, so leaving one out is "as it is" rather than
// a zero: `blocked: false` has to be able to mean unblock, which it
// could not if absent meant the same thing.
func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role         *Role  `json:"role"`
		Blocked      *bool  `json:"blocked"`
		AccessRoleID *int64 `json:"access_role_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	caller := FromContext(ctx)
	username := r.PathValue("username")

	// Nothing asked for is not an error and not a write. It is what a
	// form submitted with nothing changed sends.
	updated, err := h.svc.Repo().ByUsername(ctx, username)
	if err != nil {
		WriteError(w, ErrNoSuchUser)
		return
	}
	if err := Require(caller, RoleAdmin); err != nil {
		WriteError(w, err)
		return
	}

	// The role first: an account being blocked in the same request is
	// still the account whose role this is about, and the two refusals
	// are independent.
	if req.Role != nil {
		updated, err = h.svc.SetRole(ctx, caller, username, *req.Role)
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	if req.Blocked != nil {
		updated, err = h.svc.SetBlocked(ctx, caller, username, *req.Blocked)
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	if req.AccessRoleID != nil {
		updated, err = h.svc.SetAccessRole(ctx, caller, username, *req.AccessRoleID)
		if err != nil {
			WriteError(w, err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, userResponseFor(updated))
}

// resetPassword issues a new password for somebody else's account and
// returns it once, for an admin to hand over.
//
// **The API keys are untouched**, which is what separates this from
// DELETE /users/{username}/credentials: a forgotten password is not a
// lost laptop. Every session ends, because the password changed.
func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := r.PathValue("username")
	password, err := h.svc.ResetPassword(ctx, FromContext(ctx), username)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"username": username,
		"password": password,
	})
}

// revokeCredentials ends every session and key an account holds without
// deleting the account — the answer to a laptop that walked off.
func (h *Handler) revokeCredentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	revoked, err := h.svc.RevokeCredentials(ctx, FromContext(ctx), r.PathValue("username"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, revoked)
}

// rotateAPIKey replaces exactly the key this request authenticated with,
// keeping its name. A user can hold several independent keys precisely so
// that rotating one — routine hygiene on the key your terminal uses, for
// instance — can't silently invalidate an unrelated integration's key.
func (h *Handler) rotateAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key, err := h.svc.RotateAPIKey(ctx, FromContext(ctx), KeyHashFromContext(ctx))
	if errors.Is(err, ErrUnauthenticated) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"api_key": key})
}

func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		AccessRoleID int64  `json:"access_role_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	created, generated, err := h.svc.CreateAPIKey(r.Context(), FromContext(r.Context()),
		KeyRequest{Name: req.Name, AccessRoleID: req.AccessRoleID})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id": created.ID, "name": created.Name, "api_key": generated,
		"access_role_id": created.AccessRoleID,
	})
}

func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	keys, err := h.svc.ListAPIKeys(ctx, FromContext(ctx))
	if errors.Is(err, ErrUnauthenticated) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAPIKeyResponses(keys, KeyHashFromContext(ctx), h.svc.RoleNames(ctx)))
}

func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}
	switch err := h.svc.RevokeAPIKey(r.Context(), FromContext(r.Context()), id); {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, database.ErrNotFound):
		http.Error(w, "api key not found", http.StatusNotFound)
	default:
		WriteError(w, err)
	}
}

// WriteError maps this module's errors onto status codes. Every other
// module falls through to it, since authorization now lives here and its
// two refusals are the ones they all raise.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoAvatar):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrAvatarTooLarge):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, ErrAvatarType):
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
	case errors.Is(err, ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrKeyScopeWider), errors.Is(err, ErrKeyRestricted):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrHidden), errors.Is(err, ErrNoSuchRole):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrRoleName), errors.Is(err, ErrBadGrant):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrRoleNameTaken), errors.Is(err, ErrRoleInUse), errors.Is(err, ErrSystemRole):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrBadUsername), errors.Is(err, ErrBadEmail),
		errors.Is(err, ErrBadDisplayName), errors.Is(err, ErrUnknownAvatar):
		http.Error(w, err.Error(), http.StatusBadRequest)

	case errors.Is(err, ErrUsernameTaken):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoSuchUser):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrCannotRemoveYourself), errors.Is(err, ErrLastAdmin),
		errors.Is(err, ErrCannotBlockYourself), errors.Is(err, ErrCannotChangeYourOwnRole):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrInvalidRole), errors.Is(err, ErrPasswordTooShort), errors.Is(err, ErrPasswordTooLong),
		errors.Is(err, ErrUnknownTheme), errors.Is(err, slug.ErrReserved):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, slug.ErrInvalid):
		http.Error(w, "username "+slug.ErrInvalid.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// AddResponse is a newly created account and the password it signs in
// with, shown exactly once.
//
// Once, because this instance keeps only the hash: there is no endpoint
// that reads it back, and an account whose password is lost before it is
// handed over is deleted and made again.
type AddResponse struct {
	Username string `json:"username"`
	Role     Role   `json:"role"`
	Password string `json:"password"`
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		// Password is optional, and generated when it is not given —
		// the same bargain a datastore's makes, for the same reason: a
		// field somebody has to fill in is a field somebody fills in
		// badly.
		Password string `json:"password"`
		// AccessRoleID gives the account a role from the list, and when
		// set decides admin or member itself.
		AccessRoleID int64 `json:"access_role_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = string(RoleMember)
	}
	if !slug.Valid(req.Username) {
		WriteError(w, slug.ErrInvalid)
		return
	}

	var created *User
	var password string
	var err error
	if req.AccessRoleID != 0 {
		created, password, err = h.svc.AddWithRole(r.Context(), FromContext(r.Context()),
			req.Username, req.Password, req.AccessRoleID)
	} else {
		created, password, err = h.svc.Add(r.Context(), FromContext(r.Context()),
			req.Username, req.Password, Role(req.Role))
	}
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, AddResponse{
		Username: created.Username, Role: created.Role, Password: password,
	})
}

// --- access roles ---

type roleBody struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Grants      []Grant `json:"grants"`
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roles, err := h.svc.ListRoles(ctx, FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := RolesResponse{Roles: make([]RoleResponse, 0, len(roles)), Resources: Resources}
	for _, role := range roles {
		out.Roles = append(out.Roles, toRoleResponse(role))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	var body roleBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	role, err := h.svc.CreateRole(ctx, FromContext(ctx), RoleRequest(body))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toRoleResponse(role))
}

func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid role id", http.StatusBadRequest)
		return
	}
	var body roleBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	role, err := h.svc.UpdateRole(ctx, FromContext(ctx), id, RoleRequest(body))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRoleResponse(role))
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid role id", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if err := h.svc.DeleteRole(ctx, FromContext(ctx), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
