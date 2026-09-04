package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{}}
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	return c.http.Do(req)
}

// CreateApp registers a new app in org's project (environment defaults
// to the project's "production" environment when empty) and returns its
// registry push path.
func (c *Client) CreateApp(ctx context.Context, name, domain, org, project, environment string) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/apps", map[string]string{
		"name": name, "domain": domain, "org": org, "project": project, "environment": environment,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create app: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Image, nil
}

func (c *Client) Deploy(ctx context.Context, name, tag string) error {
	resp, err := c.do(ctx, http.MethodPost, "/apps/"+name+"/deploy", map[string]string{"tag": tag})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deploy: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) SetEnv(ctx context.Context, name string, vars map[string]string) error {
	resp, err := c.do(ctx, http.MethodPut, "/apps/"+name+"/env", map[string]map[string]string{"vars": vars})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set env: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Logs(ctx context.Context, name string) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet, "/apps/"+name+"/logs", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("logs: unexpected status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

type Org struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (c *Client) CreateOrg(ctx context.Context, slug, name string) error {
	resp, err := c.do(ctx, http.MethodPost, "/orgs", map[string]string{"slug": slug, "name": name})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create org: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) ListOrgs(ctx context.Context) ([]Org, error) {
	resp, err := c.do(ctx, http.MethodGet, "/orgs", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list orgs: unexpected status %d", resp.StatusCode)
	}
	var out []Org
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

type Project struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Environments []string `json:"environments,omitempty"`
}

func (c *Client) CreateProject(ctx context.Context, org, slug, name string) (Project, error) {
	resp, err := c.do(ctx, http.MethodPost, "/orgs/"+org+"/projects", map[string]string{"slug": slug, "name": name})
	if err != nil {
		return Project{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return Project{}, fmt.Errorf("create project: unexpected status %d", resp.StatusCode)
	}
	var out Project
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Project{}, err
	}
	return out, nil
}

func (c *Client) ListProjects(ctx context.Context, org string) ([]Project, error) {
	resp, err := c.do(ctx, http.MethodGet, "/orgs/"+org+"/projects", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list projects: unexpected status %d", resp.StatusCode)
	}
	var out []Project
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SetProjectEnv(ctx context.Context, org, project string, vars map[string]string) error {
	resp, err := c.do(ctx, http.MethodPut, "/orgs/"+org+"/projects/"+project+"/env", map[string]map[string]string{"vars": vars})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set project env: unexpected status %d", resp.StatusCode)
	}
	return nil
}

type Environment struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (c *Client) CreateEnvironment(ctx context.Context, org, project, slug, name string) (Environment, error) {
	resp, err := c.do(ctx, http.MethodPost, "/orgs/"+org+"/projects/"+project+"/environments", map[string]string{"slug": slug, "name": name})
	if err != nil {
		return Environment{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return Environment{}, fmt.Errorf("create environment: unexpected status %d", resp.StatusCode)
	}
	var out Environment
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Environment{}, err
	}
	return out, nil
}

func (c *Client) ListEnvironments(ctx context.Context, org, project string) ([]Environment, error) {
	resp, err := c.do(ctx, http.MethodGet, "/orgs/"+org+"/projects/"+project+"/environments", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list environments: unexpected status %d", resp.StatusCode)
	}
	var out []Environment
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SetEnvironmentEnv(ctx context.Context, org, project, environment string, vars map[string]string) error {
	resp, err := c.do(ctx, http.MethodPut, "/orgs/"+org+"/projects/"+project+"/environments/"+environment+"/env", map[string]map[string]string{"vars": vars})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set environment env: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) DeleteEnvironment(ctx context.Context, org, project, environment string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/orgs/"+org+"/projects/"+project+"/environments/"+environment, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete environment: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// CreateOrgUser adds a user to an organization. The returned API key is
// empty when the username already existed and only gained a membership —
// that user keeps the key they already have.
func (c *Client) CreateOrgUser(ctx context.Context, orgSlug, username, role string) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/orgs/"+orgSlug+"/users", map[string]string{"username": username, "role": role})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create user: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.APIKey, nil
}

func (c *Client) RotateAPIKey(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/users/me/api-key/rotate", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("rotate api key: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.APIKey, nil
}

// WhoAmI returns the username of the account the client's saved API key
// belongs to. `cubeship registry login` uses this to learn the username
// to log the registry in as — the credentials file only ever stores the
// key itself.
func (c *Client) WhoAmI(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/users/me", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whoami: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Username, nil
}

// APIKey is one of the caller's API keys, as reported by ListAPIKeys.
// The key value itself is only ever returned once, at creation.
type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CurrentKey bool       `json:"current_key"`
}

// CreateAPIKey issues an additional API key for the caller under name,
// independent of any key they already hold.
func (c *Client) CreateAPIKey(ctx context.Context, name string) (id int64, apiKey string, err error) {
	resp, err := c.do(ctx, http.MethodPost, "/users/me/api-keys", map[string]string{"name": name})
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return 0, "", fmt.Errorf("create api key: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		ID     int64  `json:"id"`
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, "", err
	}
	return out.ID, out.APIKey, nil
}

func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	resp, err := c.do(ctx, http.MethodGet, "/users/me/api-keys", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list api keys: unexpected status %d", resp.StatusCode)
	}
	var out []APIKey
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) RevokeAPIKey(ctx context.Context, id int64) error {
	resp, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/users/me/api-keys/%d", id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("revoke api key: unexpected status %d: %s", resp.StatusCode, body)
	}
	return nil
}
