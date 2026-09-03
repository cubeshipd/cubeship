package dockerx

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
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
}

type Client struct {
	api apiClient
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
