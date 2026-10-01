package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// Storage is the reclaimable part of Docker's disk usage report.
type Storage struct {
	ImagesBytes, BuildCacheBytes, StoppedContainersBytes, VolumesBytes int64
	Images, StoppedContainers, Volumes                                 int
}

type storageAPI interface {
	DiskUsage(context.Context, types.DiskUsageOptions) (types.DiskUsage, error)
	ImagePrune(context.Context, filters.Args) (image.PruneReport, error)
	ContainerPrune(context.Context, filters.Args) (container.PruneReport, error)
	BuildCachePrune(context.Context, build.CachePruneOptions) (*build.CachePruneReport, error)
}

func (c *Client) storageAPI() (storageAPI, error) {
	a, ok := c.api.(storageAPI)
	if !ok {
		return nil, fmt.Errorf("docker engine does not support storage management")
	}
	return a, nil
}

func (c *Client) Storage(ctx context.Context) (Storage, error) {
	a, err := c.storageAPI()
	if err != nil {
		return Storage{}, err
	}
	d, err := a.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return Storage{}, err
	}
	var s Storage
	for _, i := range d.Images {
		if i.Containers == 0 {
			s.Images++
			s.ImagesBytes += i.Size
		}
	}
	for _, x := range d.Containers {
		if x.State != "running" {
			s.StoppedContainers++
			s.StoppedContainersBytes += x.SizeRw + x.SizeRootFs
		}
	}
	for _, v := range d.Volumes {
		if v.UsageData != nil && v.UsageData.RefCount == 0 {
			s.Volumes++
			s.VolumesBytes += v.UsageData.Size
		}
	}
	for _, b := range d.BuildCache {
		if !b.InUse {
			s.BuildCacheBytes += b.Size
		}
	}
	return s, nil
}

// Prune removes only Docker artifacts that are not referenced by a running
// container. Volumes are deliberately excluded; their deletion is a separate
// data-loss decision.
func (c *Client) Prune(ctx context.Context) (int64, error) {
	a, err := c.storageAPI()
	if err != nil {
		return 0, err
	}
	var reclaimed int64
	if r, err := a.ImagePrune(ctx, filters.NewArgs(filters.Arg("dangling", "false"))); err != nil {
		return 0, err
	} else {
		reclaimed += int64(r.SpaceReclaimed)
	}
	if r, err := a.ContainerPrune(ctx, filters.NewArgs()); err != nil {
		return 0, err
	} else {
		reclaimed += int64(r.SpaceReclaimed)
	}
	if r, err := a.BuildCachePrune(ctx, build.CachePruneOptions{}); err != nil {
		return 0, err
	} else {
		reclaimed += int64(r.SpaceReclaimed)
	}
	return reclaimed, nil
}
