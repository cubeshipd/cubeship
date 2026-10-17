package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (c *Client) ListAppVolumes(ctx context.Context, ref string) ([]Volume, error) {
	return request[[]Volume](ctx, c, "list app volumes", http.MethodGet, appPath(ref)+"/volumes",
		nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) AddAppVolume(ctx context.Context, ref, containerPath string) (Volume, error) {
	return request[Volume](ctx, c, "add app volume", http.MethodPost, appPath(ref)+"/volumes",
		map[string]any{"path": containerPath}, http.StatusCreated, DefaultTimeout)
}

// RemoveAppVolume takes a volume off an app, keeping its data unless
// deleteData.
func (c *Client) RemoveAppVolume(ctx context.Context, ref string, id int64, deleteData bool) error {
	path := fmt.Sprintf("%s/volumes/%d", appPath(ref), id)
	if deleteData {
		path += "?delete_data=true"
	}
	_, err := request[noContent](ctx, c, "remove app volume", http.MethodDelete, path, nil,
		http.StatusNoContent, DefaultTimeout)
	return err
}

// TCPPort is one of an app's published TCP ports.
type TCPPort struct {
	ID            int64     `json:"id"`
	ContainerPort int       `json:"container_port"`
	HostPort      int       `json:"host_port"`
	CreatedAt     time.Time `json:"created_at"`
}

func (c *Client) ListAppTCPPorts(ctx context.Context, ref string) ([]TCPPort, error) {
	return request[[]TCPPort](ctx, c, "list app tcp ports", http.MethodGet, appPath(ref)+"/tcp-ports",
		nil, http.StatusOK, DefaultTimeout)
}

// AddAppTCPPort publishes a container port; hostPort 0 lets the instance
// pick one.
func (c *Client) AddAppTCPPort(ctx context.Context, ref string, containerPort, hostPort int) (TCPPort, error) {
	body := map[string]any{"container_port": containerPort}
	if hostPort != 0 {
		body["host_port"] = hostPort
	}
	return request[TCPPort](ctx, c, "add app tcp port", http.MethodPost, appPath(ref)+"/tcp-ports",
		body, http.StatusCreated, DefaultTimeout)
}

func (c *Client) RemoveAppTCPPort(ctx context.Context, ref string, id int64) error {
	_, err := request[noContent](ctx, c, "remove app tcp port", http.MethodDelete,
		fmt.Sprintf("%s/tcp-ports/%d", appPath(ref), id), nil, http.StatusNoContent, DefaultTimeout)
	return err
}

// PlaceApp says which machines run an app and how many copies run in
// total.
//
// Each is left alone when it is not given: no machines keeps the ones
// it has, no scale keeps the count. Where its traffic arrives is not a
// choice — every name this instance serves arrives at the control
// plane, which routes it to whichever machine runs the app.
func (c *Client) PlaceApp(ctx context.Context, ref string, nodes []string, scale int, spread *bool) (App, error) {
	body := map[string]any{}
	if len(nodes) > 0 {
		body["nodes"] = nodes
	}
	if scale > 0 {
		body["scale"] = scale
	}
	if spread != nil {
		body["spread"] = *spread
	}
	return request[App](ctx, c, "place app", http.MethodPatch,
		"/apps/"+ref, body, http.StatusOK, DefaultTimeout)
}

// SetAppAutoscale hands an app's replica count to the instance.
//
// The whole rule travels: zero is how it is turned off, so a field left
// out would be indistinguishable from one being cleared.
func (c *Client) SetAppAutoscale(ctx context.Context, ref string, a Autoscale) (App, error) {
	return request[App](ctx, c, "set app autoscaling", http.MethodPatch,
		"/apps/"+ref, map[string]any{"autoscale": a}, http.StatusOK, DefaultTimeout)
}

// SetAppLimits caps what one copy of an app may take.
//
// The whole ceiling travels, both halves, because zero is a value here
// rather than a gap: it is the only way to say "no limit", so a field
// left out would be indistinguishable from one being removed.
func (c *Client) SetAppLimits(ctx context.Context, ref string, limits Limits) (App, error) {
	return request[App](ctx, c, "set app limits", http.MethodPatch,
		"/apps/"+ref, map[string]any{"limits": limits}, http.StatusOK, DefaultTimeout)
}

// SetAppPredeploy configures the command run from the new image before a
// deploy swaps containers. command may be a shell string or []string argv.
func (c *Client) SetAppPredeploy(ctx context.Context, ref string, command any, timeoutSeconds int64) (App, error) {
	return request[App](ctx, c, "set app pre-deploy command", http.MethodPatch,
		"/apps/"+ref, map[string]any{"predeploy_command": command, "predeploy_timeout": timeoutSeconds}, http.StatusOK, DefaultTimeout)
}

func (c *Client) CancelDeployment(ctx context.Context, ref string, id int64) error {
	_, err := request[struct{}](ctx, c, "cancel deployment", http.MethodPost,
		"/apps/"+ref+"/deployments/"+strconv.FormatInt(id, 10)+"/cancel", nil, http.StatusNoContent, DefaultTimeout)
	return err
}
