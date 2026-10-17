package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type App struct {
	// Reference identifies the app — project/environment/name — and
	// is also its registry repository path.
	Reference string `json:"reference"`
	Name      string `json:"name"`
	// Source is where the app's image comes from.
	Source string `json:"source"`
	// Domains are every name the app answers at, each with the port
	// behind it. An app can have several — one image can expose more
	// than one port, and each name says which it reaches.
	Domains     []Domain `json:"domains"`
	Image       string   `json:"image"`
	Status      string   `json:"status"`
	Project     string   `json:"project"`
	Environment string   `json:"environment"`
	// Nodes are the machines this app runs on. On an instance of one
	// box that is the control plane.
	Nodes []string `json:"nodes"`
	// Scale is how many copies run in total, across those machines.
	Scale int `json:"scale"`
	// Spread says it follows the cluster: every machine there is.
	Spread bool `json:"spread,omitempty"`
	// Address is where a DNS record for this app has to point, which is
	// this instance's own whatever machine the app runs on.
	Address string `json:"address,omitempty"`
	// InternalHost is where another app on this instance reaches this
	// one, on the app's own port. A public name cannot do that job from
	// inside the box — the request leaves it for a record pointing back
	// at it.
	InternalHost string `json:"internal_host,omitempty"`
	// Split says the machines serving it are not all serving the same
	// deployment.
	Split bool `json:"split,omitempty"`
	// Limits is what one copy of it may take from the machine it runs
	// on. Zero in either half is no limit.
	Limits Limits `json:"limits"`
	// Autoscale is when the instance decides the count itself. Off
	// when Max is zero, which is every app until somebody says
	// otherwise.
	Autoscale        Autoscale `json:"autoscale"`
	PredeployCommand any       `json:"predeploy_command,omitempty"`
	PredeployTimeout int64     `json:"predeploy_timeout,omitempty"`
	StopGracePeriod  int64     `json:"stop_grace_period,omitempty"`
	StopSignal       string    `json:"stop_signal,omitempty"`
	WatchPaths       []string  `json:"watch_paths,omitempty"`
	IgnorePaths      []string  `json:"ignore_paths,omitempty"`
}

// Autoscale is the rule the instance scales an app by.
type Autoscale struct {
	Min int     `json:"min"`
	Max int     `json:"max"`
	CPU float64 `json:"cpu"`
	// At is when it last changed the count, and what the cooldown is
	// measured from.
	At *time.Time `json:"at,omitempty"`
}

// Limits is a container's ceiling: cores and bytes, zero meaning none.
type Limits struct {
	CPU    float64 `json:"cpu"`
	Memory int64   `json:"memory_bytes"`
}

// Domain is one name an app is served at.
type Domain struct {
	ID   int64  `json:"id"`
	Host string `json:"host"`
	// Port is what this name reaches, or 0 for "read it from the
	// image".
	Port int `json:"port"`
}

// CreateApp registers an app and returns it, including the registry path
// to push to. environment may be empty, which means "production".
// CreateApp registers an app. image is what an `external` app pulls,
// without a tag, and is empty for every other source — the daemon
// refuses an image on a source that derives its own, rather than
// ignoring it.
func (c *Client) CreateApp(ctx context.Context, name, projectSlug, environment, source, image, tag string) (App, error) {
	body := map[string]string{
		"name": name, "project": projectSlug,
		"environment": environment, "source": source,
	}
	if image != "" {
		body["image"] = image
	}
	if tag != "" {
		body["tag"] = tag
	}
	return request[App](ctx, c, "create app", http.MethodPost, "/apps", body,
		http.StatusCreated, DefaultTimeout)
}

// AddAppDomain gives an app a name to answer at.
//
// port is what it reaches inside the container, or 0 to read it from the
// image — which is the normal answer, and the only one available before
// an app that builds has been built.
func (c *Client) AddAppDomain(ctx context.Context, ref, host string, port int) (App, error) {
	return request[App](ctx, c, "add app domain", http.MethodPost, appPath(ref)+"/domains",
		map[string]any{"host": host, "port": port}, http.StatusCreated, DefaultTimeout)
}

// RemoveAppDomain takes a name off an app.
func (c *Client) RemoveAppDomain(ctx context.Context, ref string, domainID int64) (App, error) {
	return request[App](ctx, c, "remove app domain", http.MethodDelete,
		fmt.Sprintf("%s/domains/%d", appPath(ref), domainID), nil, http.StatusOK, DefaultTimeout)
}

// Volume is one of an app's volumes.
type Volume struct {
	ID        int64     `json:"id"`
	Path      string    `json:"path"`
	Node      string    `json:"node"`
	CreatedAt time.Time `json:"created_at"`
}

func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	return request[[]App](ctx, c, "list apps", http.MethodGet, "/apps", nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) GetApp(ctx context.Context, ref string) (App, error) {
	return request[App](ctx, c, "get app", http.MethodGet, appPath(ref), nil, http.StatusOK, DefaultTimeout)
}

// DeleteApp removes an app and stops the container serving it. Images
// already pushed stay in the registry.
func (c *Client) DeleteApp(ctx context.Context, ref string) error {
	_, err := request[noContent](ctx, c, "delete app", http.MethodDelete,
		appPath(ref), nil, http.StatusOK, DeployTimeout)
	return err
}

// appSegments renders an app reference as the three path segments the
// detach route takes, escaping each separately. A two-part reference is
// production, the same shorthand the daemon accepts.
func appSegments(ref string) string {
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) == 2 {
		parts = []string{parts[0], "production", parts[1]}
	}
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = segment(p)
	}
	return strings.Join(escaped, "/")
}
