// Package client talks to a Cubeship daemon's HTTP API. It is what the
// CLI is built on.
//
// The wire types here are declared rather than imported from the server
// modules on purpose: importing internal/app or internal/org would drag
// Postgres and the Docker SDK into the CLI binary. What keeps them from
// drifting is client_test.go, which runs these calls against a real
// server rather than a mock — a renamed field or a changed status code
// fails there.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Timeouts. Most calls are a database round-trip and answer immediately;
// a deploy blocks for the pull, the container start and several seconds
// of health checks, so it gets its own budget rather than the default.
const (
	DefaultTimeout = 30 * time.Second
	DeployTimeout  = 10 * time.Minute
	LogsTimeout    = 2 * time.Minute
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		// No Timeout on the client itself: it would apply to every call
		// equally, and a deploy legitimately takes minutes. Each call
		// sets its own deadline on the context instead.
		http: &http.Client{},
	}
}

// Error is what every call returns when the daemon refuses. It carries
// the server's own message, which is the part that tells a user what to
// do — "app already exists" rather than "unexpected status 409".
type Error struct {
	Op      string
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%s: the daemon answered %d %s", e.Op, e.Status, http.StatusText(e.Status))
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// Status reports the HTTP status behind err, or 0 if err is not an API
// refusal. It lets a caller tell "already exists" from "not found"
// without matching on message text.
func Status(err error) int {
	var apiErr *Error
	if errorsAs(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// request performs one API call and decodes its body into Out. Pass
// `struct{}` as Out for an endpoint that answers with no content.
//
// It is a function rather than a method because Go does not allow type
// parameters on methods.
func request[Out any](ctx context.Context, c *Client, op, method, path string, body any, want int, timeout time.Duration) (Out, error) {
	var out Out

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := c.send(ctx, method, path, body)
	if err != nil {
		return out, fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != want {
		return out, apiError(op, resp)
	}
	if _, noBody := any(out).(struct{}); noBody {
		return out, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("%s: could not read the daemon's reply: %w", op, err)
	}
	return out, nil
}

func (c *Client) send(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// apiError reads the refusal the daemon sent. Its errors are plain text
// (see http.Error), except a failed deploy, which answers JSON so it can
// carry the underlying reason.
func apiError(op string, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	message := strings.TrimSpace(string(raw))

	var asJSON struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &asJSON) == nil && asJSON.Error != "" {
		message = asJSON.Error
	}
	return &Error{Op: op, Status: resp.StatusCode, Message: message}
}

// segment escapes one path component, so a name with a slash or a space
// in it produces a 404 from the daemon rather than a request against
// some other route.
func segment(s string) string {
	return url.PathEscape(s)
}
