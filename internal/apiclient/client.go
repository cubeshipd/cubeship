package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (c *Client) CreateApp(ctx context.Context, name, domain, org string) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/apps", map[string]string{"name": name, "domain": domain, "org": org})
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
