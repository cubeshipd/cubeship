package dockerx

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// apiClient is the subset of the Docker Engine API this package uses.
// *dockerclient.Client satisfies it structurally.
//
// NOTE: these method signatures match github.com/docker/docker v27.3.1 as
// actually installed. The SDK moves option types between packages
// between releases — image.PullOptions lived on types.ImagePullOptions
// before v27 — so if `go build` fails after a version bump, adjust these
// to whatever the compiler error names. The compiler is the source of
// truth for the installed SDK version.
type apiClient interface {
	ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
	ImageLoad(ctx context.Context, input io.Reader, quiet bool) (image.LoadResponse, error)
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

	// registryTokenSigners holds a token-minting function per registry
	// host (e.g. "127.0.0.1:5000"). PullImage calls the matching signer
	// fresh for every pull — tokens from the embedded registry's
	// token-auth flow expire in regauth.TokenTTL (5 minutes), so caching
	// one across pulls would go stale. Set once at startup, read-only
	// afterwards.
	registryTokenSigners map[string]func(repository string) (string, error)
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

// SetRegistryTokenSigner registers a token-minting function for a
// registry host. Any later PullImage whose reference starts with that
// host calls signer with the pulled repository (the reference minus
// host and tag) to obtain a fresh identity token, scoped to exactly
// that repository, for the pull. Call this before serving requests; it
// is not safe to call concurrently with PullImage.
func (c *Client) SetRegistryTokenSigner(host string, signer func(repository string) (string, error)) {
	if c.registryTokenSigners == nil {
		c.registryTokenSigners = make(map[string]func(repository string) (string, error))
	}
	c.registryTokenSigners[host] = signer
}
