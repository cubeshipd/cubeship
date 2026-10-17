package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// appPath turns an app reference into its URL. Each part is escaped
// separately, so the four segments stay four segments whatever they
// contain.
func appPath(ref string) string {
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = segment(p)
	}
	return "/apps/" + strings.Join(escaped, "/")
}

// Deployment is one deploy attempt.
type Deployment struct {
	ID             int64     `json:"id"`
	Status         string    `json:"status"`
	Image          string    `json:"image"`
	GitSHA         string    `json:"git_sha,omitempty"`
	GitBranch      string    `json:"git_branch,omitempty"`
	Error          string    `json:"error,omitempty"`
	Phase          string    `json:"phase"`
	DurationMillis int64     `json:"duration_ms"`
	Cancelled      bool      `json:"cancelled"`
	Skipped        bool      `json:"skipped,omitempty"`
	Logs           string    `json:"logs,omitempty"`
	HasLogs        bool      `json:"has_logs"`
	Deletable      bool      `json:"deletable"`
	CreatedAt      time.Time `json:"created_at"`
}

// Deployment statuses.
const (
	DeploymentPending   = "pending"
	DeploymentSucceeded = "succeeded"
	DeploymentFailed    = "failed"
	DeploymentSkipped   = "skipped"
)

// Done reports whether the deploy has finished, either way.
func (d Deployment) Done() bool {
	return d.Status == DeploymentSucceeded || d.Status == DeploymentFailed
}

// Deploy asks the daemon to redeploy an app and returns as soon as the
// deploy is accepted. The work runs on the daemon, detached from this
// request — use WaitForDeployment to follow it, or don't, and it still
// runs.
func (c *Client) Deploy(ctx context.Context, ref, tag string) (Deployment, error) {
	return request[Deployment](ctx, c, "deploy", http.MethodPost,
		appPath(ref)+"/deploy", map[string]string{"tag": tag}, http.StatusAccepted, DefaultTimeout)
}

// WaitForDeployment blocks until a deploy finishes, or until ctx runs
// out — in which case it returns the deployment as it stands, with no
// error, because giving up on watching is not a failure.
func (c *Client) WaitForDeployment(ctx context.Context, ref string, id int64) (Deployment, error) {
	return request[Deployment](ctx, c, "check deploy", http.MethodGet,
		fmt.Sprintf("%s/deployments/%d?wait=true", appPath(ref), id), nil, http.StatusOK, DeployWaitTimeout)
}

// Deployments returns an app's recent deploy history, newest first.
func (c *Client) Deployments(ctx context.Context, ref string) ([]Deployment, error) {
	return request[[]Deployment](ctx, c, "list deploys", http.MethodGet,
		appPath(ref)+"/deployments", nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) AppEnv(ctx context.Context, ref string) (EnvVars, error) {
	return request[EnvVars](ctx, c, "read app env", http.MethodGet,
		appPath(ref)+"/env", nil, http.StatusOK, DefaultTimeout)
}

// MergeAppEnv adds or overwrites set and removes unset, leaving every
// other variable alone.
func (c *Client) MergeAppEnv(ctx context.Context, ref string, set map[string]string, unset []string) error {
	_, err := request[noContent](ctx, c, "update app env", http.MethodPatch,
		appPath(ref)+"/env", mergeEnv{Set: set, Unset: unset}, http.StatusOK, DefaultTimeout)
	return err
}

// SetAppEnv replaces every app-level variable.
func (c *Client) SetAppEnv(ctx context.Context, ref string, vars map[string]string) error {
	_, err := request[noContent](ctx, c, "set app env", http.MethodPut,
		appPath(ref)+"/env", envVars{Vars: vars}, http.StatusOK, DefaultTimeout)
	return err
}

// Logs streams an app's container output. tail limits it to that many
// trailing lines; empty means the daemon's default. The caller closes
// the reader.
func (c *Client) Logs(ctx context.Context, ref, tail string) (io.ReadCloser, error) {
	const op = "read logs"

	path := appPath(ref) + "/logs"
	if tail != "" {
		path += "?" + url.Values{"tail": {tail}}.Encode()
	}

	// Not through request: the body is the result, so it must outlive
	// this call — which also means no deadline that would cut the stream
	// short. The caller's context still cancels it.
	ctx, cancel := context.WithTimeout(ctx, LogsTimeout)
	resp, err := c.send(ctx, http.MethodGet, path, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		defer cancel()
		return nil, apiError(op, resp)
	}
	return &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}, nil
}

// cancelOnClose releases the request context when the caller is done
// reading, so a stream abandoned early doesn't leak it.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
