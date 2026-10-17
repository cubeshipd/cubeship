package client

import (
	"context"
	"net/http"
)

// Server is one machine this instance is made of.
//
// The control plane is one of them, and it is a row like the others:
// "the other servers" is a list that cannot answer where something
// runs, and everything placeable on another machine has to be placeable
// on this one.
type Server struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	ControlPlane bool   `json:"control_plane"`
	// Status is derived from when the machine last called in, never
	// stored: ready, unreachable, or pending for one that has been
	// added and never installed.
	Status  string `json:"status"`
	Address string `json:"address,omitempty"`
	Version string `json:"version,omitempty"`

	Cores            int   `json:"cores"`
	MemoryTotalBytes int64 `json:"memory_total_bytes"`
	DiskTotalBytes   int64 `json:"disk_total_bytes"`
	// InMesh is whether this machine is on the cluster's private
	// network. A machine can be calling in and not on it, and then its
	// containers cannot reach the other machines'.
	InMesh bool `json:"in_mesh"`
}

// AddedServer is the one answer that carries a credential, because it
// is the only moment it exists in a form anybody can read.
type AddedServer struct {
	Server
	// Token is what that machine's agent authenticates with. Shown
	// once: only its hash is kept here.
	Token string `json:"token"`
}

// ListServers is every machine in the cluster, the control plane
// included.
func (c *Client) ListServers(ctx context.Context) ([]Server, error) {
	return request[[]Server](ctx, c, "list servers", http.MethodGet,
		"/nodes", nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) GetServer(ctx context.Context, name string) (Server, error) {
	return request[Server](ctx, c, "get server", http.MethodGet,
		"/nodes/"+segment(name), nil, http.StatusOK, DefaultTimeout)
}

// AddServer mints a machine's credential and contacts nothing. The row
// is a place for a box that does not exist yet; installing the agent
// there is the other half, and the two do not touch.
func (c *Client) AddServer(ctx context.Context, name, description string) (AddedServer, error) {
	return request[AddedServer](ctx, c, "add server", http.MethodPost, "/nodes",
		map[string]string{"name": name, "description": description},
		http.StatusCreated, DefaultTimeout)
}

// RemoveServer takes a machine out of the cluster. It is a local act:
// the row and its credential go, the agent is refused on its next call,
// and nothing on that machine is touched — a delete that reached out
// would be one that hangs on a host nobody can dial.
func (c *Client) RemoveServer(ctx context.Context, name string) error {
	_, err := request[struct{}](ctx, c, "remove server", http.MethodDelete,
		"/nodes/"+segment(name), nil, http.StatusOK, DefaultTimeout)
	return err
}

// Mesh is the cluster's private network.
type Mesh struct {
	// Network is the overlay's name, empty on an instance with no
	// cluster.
	Network string `json:"network,omitempty"`
	// Encrypted is whether what crosses between machines is carried
	// over IPsec.
	Encrypted bool `json:"encrypted"`
}

func (c *Client) Mesh(ctx context.Context) (Mesh, error) {
	return request[Mesh](ctx, c, "read the cluster's network", http.MethodGet,
		"/nodes/mesh", nil, http.StatusOK, DefaultTimeout)
}
