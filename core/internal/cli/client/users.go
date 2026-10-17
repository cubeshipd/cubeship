package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// APIKey is one of the caller's keys. The key value itself is only ever
// returned once, at creation.
type APIKey struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// AccessRole narrows the key; empty for one carrying all its owner's
	// access.
	AccessRole string     `json:"access_role,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CurrentKey bool       `json:"current_key"`
}

// WhoAmI returns the username the client's key belongs to. `registry
// login` uses it to learn the username to log Docker in as — the saved
// credentials file only ever stores the key itself.
func (c *Client) WhoAmI(ctx context.Context) (string, error) {
	out, err := request[struct {
		Username string `json:"username"`
	}](ctx, c, "look up your username", http.MethodGet, "/users/me", nil, http.StatusOK, DefaultTimeout)
	return out.Username, err
}

// AddUser creates an account and returns the password it signs in with,
// shown exactly once. A password is named rather than generated when it
// is not empty.
func (c *Client) AddUser(ctx context.Context, username, role, password string) (string, error) {
	body := map[string]string{"username": username, "role": role}
	if password != "" {
		body["password"] = password
	}
	out, err := request[struct {
		Password string `json:"password"`
	}](ctx, c, "add user", http.MethodPost, "/users", body, http.StatusCreated, DefaultTimeout)
	if err != nil {
		return "", err
	}
	return out.Password, nil
}

// User is one account on the instance.
type User struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// ListUsers returns every account. Admin only.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	out, err := request[struct {
		Users []User `json:"users"`
	}](ctx, c, "list users", http.MethodGet, "/users", nil, http.StatusOK, DefaultTimeout)
	return out.Users, err
}

// DeleteUser removes an account and every key and session it holds.
func (c *Client) DeleteUser(ctx context.Context, username string) error {
	_, err := request[struct{}](ctx, c, "delete user", http.MethodDelete,
		"/users/"+segment(username), nil, http.StatusNoContent, DefaultTimeout)
	return err
}

// Revoked is what revoking an account's credentials ended.
type Revoked struct {
	APIKeys  int64 `json:"api_keys"`
	Sessions int64 `json:"sessions"`
}

// RevokeUserCredentials ends every session and revokes every API key an
// account holds, leaving the account itself.
func (c *Client) RevokeUserCredentials(ctx context.Context, username string) (Revoked, error) {
	return request[Revoked](ctx, c, "revoke credentials", http.MethodDelete,
		"/users/"+segment(username)+"/credentials", nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) RotateAPIKey(ctx context.Context) (string, error) {
	out, err := request[struct {
		APIKey string `json:"api_key"`
	}](ctx, c, "rotate api key", http.MethodPost, "/users/me/api-key/rotate", nil, http.StatusOK, DefaultTimeout)
	return out.APIKey, err
}

// CreateAPIKey issues an additional key under name, independent of any
// the caller already holds.
func (c *Client) CreateAPIKey(ctx context.Context, name string) (id int64, apiKey string, err error) {
	return c.CreateRoleAPIKey(ctx, name, 0)
}

// CreateRoleAPIKey issues a key narrowed to an access role; 0 is none,
// which for a restricted key making the call is its own role.
func (c *Client) CreateRoleAPIKey(ctx context.Context, name string, roleID int64) (id int64, apiKey string, err error) {
	body := map[string]any{"name": name}
	if roleID != 0 {
		body["access_role_id"] = roleID
	}
	out, err := request[struct {
		ID     int64  `json:"id"`
		APIKey string `json:"api_key"`
	}](ctx, c, "create api key", http.MethodPost, "/users/me/api-keys",
		body, http.StatusCreated, DefaultTimeout)
	return out.ID, out.APIKey, err
}

// AccessRole is a named set of grants for members and keys.
type AccessRole struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Grants      []struct {
		Resource string   `json:"resource"`
		Level    string   `json:"level"`
		Secrets  bool     `json:"secrets,omitempty"`
		Items    []string `json:"items"`
	} `json:"grants"`
	Members int `json:"members"`
	Keys    int `json:"keys"`
}

func (c *Client) ListRoles(ctx context.Context) ([]AccessRole, error) {
	out, err := request[struct {
		Roles []AccessRole `json:"roles"`
	}](ctx, c, "list roles", http.MethodGet, "/roles", nil, http.StatusOK, DefaultTimeout)
	return out.Roles, err
}

func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return request[[]APIKey](ctx, c, "list api keys", http.MethodGet, "/users/me/api-keys",
		nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) RevokeAPIKey(ctx context.Context, id int64) error {
	_, err := request[noContent](ctx, c, "revoke api key", http.MethodDelete,
		fmt.Sprintf("/users/me/api-keys/%d", id), nil, http.StatusOK, DefaultTimeout)
	return err
}
