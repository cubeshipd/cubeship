package dockerx

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	dockerclient "github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// apiClient is the subset of the Docker Engine API this package uses.
// *dockerclient.Client satisfies it structurally.
//
// NOTE: these method signatures match github.com/docker/docker v25.0.6 as
// actually installed. ImagePull's options type is types.ImagePullOptions
// in this version (not image.PullOptions from a separate api/types/image
// package, which is what later SDK versions use) — if `go build` fails
// after `go get` pulls a different version, adjust the option struct
// types to whatever the compiler error names; the compiler is the source
// of truth for the installed SDK version.
type apiClient interface {
	ImagePull(ctx context.Context, ref string, options types.ImagePullOptions) (io.ReadCloser, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	// NOTE: NetworkCreate's option/response types are types.NetworkCreate /
	// types.NetworkCreateResponse in this installed SDK version (v25.0.6),
	// not network.CreateOptions / network.CreateResponse from a later SDK
	// version's api/types/network package — see the note above.
	NetworkCreate(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error)
}

type Client struct {
	api apiClient

	// registryAuths holds basic-auth credentials keyed by registry host
	// (e.g. "127.0.0.1:5000"). PullImage attaches the matching entry to
	// the pull so the daemon can read from its own authenticated
	// registry. Set once at startup, read-only afterwards.
	registryAuths map[string]registry.AuthConfig
}

func New() (*Client, error) {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{api: cli}, nil
}

// newWithAPI is used by tests to inject a fake apiClient.
func newWithAPI(api apiClient) *Client {
	return &Client{api: api}
}

// SetRegistryAuth registers basic-auth credentials for a registry host.
// Any later PullImage whose reference starts with that host sends them.
// Call this before serving requests; it is not safe to call concurrently
// with PullImage.
func (c *Client) SetRegistryAuth(host, username, password string) {
	if c.registryAuths == nil {
		c.registryAuths = make(map[string]registry.AuthConfig)
	}
	c.registryAuths[host] = registry.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: host,
	}
}
