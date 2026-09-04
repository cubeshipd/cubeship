package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cubeship/internal/org"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/user"
)

// Garbage collection: why it is a button rather than a schedule, and why
// it takes the registry down.
//
// Deleting a tag makes an image unreachable and frees nothing. The
// layers stay on disk, referenced by nothing, until something walks the
// storage and removes what no manifest names. In the distribution
// registry that something is a subcommand of the registry binary itself
// — `registry garbage-collect <config>` — so it runs inside the
// container, over the same bind mount, with no API in front of it.
//
// It is unsafe to run while the registry accepts pushes, and not
// subtly: the pass marks which blobs are referenced, then deletes the
// rest. A push that uploads a blob after the marking and links it before
// the sweep has its blob deleted underneath it, and the image it just
// wrote is broken with nothing reporting so. The distribution project's
// own answer is to put the registry in read-only mode or stop it.
//
// Cubeship stops it. Read-only mode is a different config.yml, and
// changing one here means bootstrap replaces the container — twice,
// once each way — which is the same outage with more moving parts. A
// stopped registry refuses a push outright, which is the honest failure:
// `docker push` retries, where a push that half-succeeded does not.
//
// Nothing schedules this. The pass is the only operation in Cubeship
// that interrupts pushes, and something that takes the registry down
// while nobody is looking is worse than disk that has to be reclaimed on
// purpose.

// GCResult is what a pass did.
type GCResult struct {
	// Freed is what the registry reported removing, where it said. The
	// registry prints what it deletes rather than a total, so this is
	// counted from that output.
	BlobsDeleted int `json:"blobs_deleted"`
	// Output is the pass's transcript. It is the only record of what was
	// removed, so it is returned rather than only logged.
	Output string `json:"output"`
	// Duration is how long the registry was down for it.
	Duration string `json:"duration"`
}

// ErrNoMaintenance is a daemon with no way to reach the registry
// container — no Engine, or a registry someone else is running.
var ErrNoMaintenance = errors.New("this daemon cannot run maintenance on the registry")

// GarbageCollect reclaims the disk that deleted images left behind.
//
// The registry is stopped for the pass and started again afterwards,
// including when the pass fails: leaving it down would turn a
// maintenance action into an outage.
//
// It is an instance-wide operation over storage that has no notion of
// tenants, so it is not scoped to an organization the way the catalogue
// is. The caller must be an admin of the organization it is asked
// through, which is the same bar as deleting an image.
func (h *Handler) GarbageCollect(ctx context.Context, caller *user.User, orgSlug string) (*GCResult, error) {
	if _, err := h.orgs.Resolve(ctx, caller, orgSlug, org.RoleAdmin); err != nil {
		return nil, err
	}
	if h.maintenance == nil {
		return nil, ErrNoMaintenance
	}

	info, err := h.maintenance.InspectContainerByName(ctx, bootstrap.RegistryContainerName)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoMaintenance, err)
	}

	started := time.Now()
	if err := h.maintenance.StopContainer(ctx, info.ID); err != nil {
		return nil, fmt.Errorf("stop the registry: %w", err)
	}
	// Whatever happens, it comes back. A pass that fails with the
	// registry left down is a far worse outcome than one that fails.
	defer func() {
		if err := h.maintenance.StartContainer(context.WithoutCancel(ctx), info.ID); err != nil {
			// Nothing above can act on this, and the pass's own result
			// is the more useful thing to return.
			_ = err
		}
	}()

	// --delete-untagged also removes manifests no tag points at, which
	// is what an overwritten tag leaves behind — the common case here,
	// since a redeploy pushes the same tag again.
	output, code, err := h.maintenance.Exec(ctx, info.ID, []string{
		"registry", "garbage-collect", "--delete-untagged", "/etc/docker/registry/config.yml",
	})
	if err != nil {
		return nil, fmt.Errorf("run the collection: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("the collection exited %d: %s", code, strings.TrimSpace(output))
	}

	return &GCResult{
		BlobsDeleted: strings.Count(output, "blob eligible for deletion"),
		Output:       strings.TrimSpace(output),
		Duration:     time.Since(started).Round(time.Millisecond).String(),
	}, nil
}
