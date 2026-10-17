package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"cubeship/internal/platform/config"
	"cubeship/internal/platform/dockerx"

	"github.com/docker/docker/errdefs"
)

// WriteAPIRouterConfig writes APIRouterConfigYAML to the path Traefik's
// file provider watches (see the Binds entry above). Call it before
// starting Traefik, and again any time cfg changes.
func WriteAPIRouterConfig(cfg *config.Config, apiHost string, daemonPort int) error {
	daemonAddress := DaemonAddress(cfg, daemonPort)
	dir := cfg.DataDir + "/traefik-dynamic"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create traefik dynamic config dir: %w", err)
	}
	path := dir + "/api.yml"
	if err := os.WriteFile(path, []byte(APIRouterConfigYAML(apiHost, daemonAddress)), 0o600); err != nil {
		return fmt.Errorf("write traefik dynamic config: %w", err)
	}
	return nil
}

// dockerAPI is the subset of dockerx.Client this package needs.
type dockerAPI interface {
	PullImage(ctx context.Context, ref string, creds *dockerx.RegistryAuth) error
	ImageID(ctx context.Context, ref string) (string, error)
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	InspectContainerByName(ctx context.Context, name string) (dockerx.ContainerInfo, error)
}

// TokenCertLabel records which trust root the registry container was
// started with, so a changed one replaces it rather than being written
// to a file the running registry has already read.
const TokenCertLabel = "cubeship.token-cert"

// fingerprint is a short, stable digest — enough to tell two values
// apart in a label, and not the value itself.
func fingerprint(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// ConfigHashLabel records, on the container itself, a fingerprint of the
// options it was created from. Comparing it against what the current
// binary wants is what lets Ensure tell "this container is running" from
// "this container is running the right thing".
const ConfigHashLabel = "cubeship.config-hash"

// configHash fingerprints the options a container should be created
// from. Anything that would need a new container to take effect — the
// image, the binds, the environment, the ports — is part of it.
//
// JSON is the serialization because encoding/json orders map keys, so
// the same options always hash the same way. The hash is not a security
// boundary, just a change detector.
func configHash(opts dockerx.ContainerOpts, imageID string) string {
	// The label itself is excluded: it holds the result.
	opts.Labels = withoutConfigHash(opts.Labels)
	// So is the ceiling, and for a reason rather than for tidiness: it
	// is the one setting the Engine can change on a running container,
	// so a different one is not a reason to replace anything. Nothing
	// here carries one — see TestNoInfrastructureContainerIsCapped,
	// which is what would fail if somebody gave one a ceiling this
	// function would then quietly refuse to notice.
	opts.Resources = dockerx.Resources{}

	encoded, err := json.Marshal(struct {
		Opts dockerx.ContainerOpts
		// The resolved image, not the reference in Opts.
		//
		// A tag is a moving name. `install.sh --local` rebuilds
		// `cubeship:local` on every install, and the
		// options are identical every time — so a fingerprint taken from
		// them alone said "unchanged", the container was left alone, and
		// the box went on running the previous build. It looked exactly
		// like a cache.
		Image string
	}{opts, imageID})
	if err != nil {
		// ContainerOpts is plain data and cannot fail to encode. If that
		// ever changes, a hash nothing matches is the safe answer: it
		// recreates rather than skipping a change.
		return "unhashable"
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func withoutConfigHash(labels map[string]string) map[string]string {
	if _, present := labels[ConfigHashLabel]; !present {
		return labels
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		if k != ConfigHashLabel {
			out[k] = v
		}
	}
	return out
}

// withConfigHash returns opts carrying its own fingerprint as a label.
func withConfigHash(opts dockerx.ContainerOpts, imageID string) dockerx.ContainerOpts {
	hash := configHash(opts, imageID)

	labels := make(map[string]string, len(opts.Labels)+1)
	for k, v := range opts.Labels {
		labels[k] = v
	}
	labels[ConfigHashLabel] = hash
	opts.Labels = labels
	return opts
}

// Ensure makes the described infrastructure container exist, run, and
// match the configuration this binary wants.
//
// A container whose configuration still matches is left alone if it is
// running, and started if it is stopped — a host reboot otherwise leaves
// the daemon "up" with no proxy running at all. One whose configuration
// has changed is replaced, because Docker cannot alter the image, binds,
// ports or environment of an existing container: the only way a new
// setting takes effect is a new container.
//
// Replacing is safe for all three of these because everything they must
// keep lives in a host bind mount — the registry's images, Traefik's
// acme.json, Postgres' data directory. It does mean a few seconds of
// downtime for that container on a release that changes its
// configuration, which is the cost of the alternative being silently
// running stale settings.
//
// Creation failures are returned rather than assumed to mean "already
// exists" — a bad bind path or port conflict is a real failure.
func Ensure(ctx context.Context, docker dockerAPI, opts dockerx.ContainerOpts) error {
	// The image is resolved before anything is compared, because what it
	// resolves to is part of what is being compared.
	imageID, err := docker.ImageID(ctx, opts.Image)
	if err != nil {
		return err
	}
	if imageID == "" {
		// Said before rather than after: on a first install these pulls
		// are most of the wait, and the installer streams these lines
		// so the person watching knows what the wait is.
		log.Printf("bootstrap: pulling %s for %s", opts.Image, opts.Name)
		if err := docker.PullImage(ctx, opts.Image, nil); err != nil {
			return fmt.Errorf("pull %s: %w (if this image was built on this host, it is not there — check the tag)", opts.Image, err)
		}
		if imageID, err = docker.ImageID(ctx, opts.Image); err != nil {
			return err
		}
	}

	opts = withConfigHash(opts, imageID)
	want := opts.Labels[ConfigHashLabel]

	existing, err := docker.InspectContainerByName(ctx, opts.Name)
	switch {
	case err == nil && existing.Labels[ConfigHashLabel] != want:
		// Also covers a container created before this label existed,
		// which is exactly the case operators used to fix by hand with
		// `docker rm -f`.
		log.Printf("bootstrap: %s was created from different settings; replacing it", opts.Name)
		if err := replace(ctx, docker, existing.ID, opts.Name); err != nil {
			return err
		}
	case err == nil && existing.Running:
		log.Printf("bootstrap: %s already running", opts.Name)
		return nil
	case err == nil:
		log.Printf("bootstrap: %s exists but is stopped; starting it", opts.Name)
		if err := docker.StartContainer(ctx, existing.ID); err != nil {
			return fmt.Errorf("start existing %s: %w", opts.Name, err)
		}
		return nil
	case !errors.Is(err, dockerx.ErrContainerNotFound):
		return fmt.Errorf("inspect %s: %w", opts.Name, err)
	}

	id, err := docker.CreateContainer(ctx, opts)
	if err != nil {
		// Only a name conflict is benign here — it means something
		// created the container between the inspect above and now.
		if isNameConflict(err) {
			log.Printf("bootstrap: %s appeared concurrently; leaving it alone", opts.Name)
			return nil
		}
		return fmt.Errorf("create %s: %w", opts.Name, err)
	}

	if err := docker.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start %s: %w", opts.Name, err)
	}
	log.Printf("bootstrap: %s started", opts.Name)
	return nil
}

// replace removes an out-of-date container so a new one can take its
// name. A stop that fails is not fatal — the container may already be
// stopped — but a remove that fails is: creating the replacement would
// then collide on the name and be mistaken for a concurrent create.
func replace(ctx context.Context, docker dockerAPI, id, name string) error {
	if err := docker.StopContainer(ctx, id); err != nil {
		log.Printf("bootstrap: could not stop %s before replacing it: %v", name, err)
	}
	if err := docker.RemoveContainer(ctx, id); err != nil {
		return fmt.Errorf("remove the outdated %s: %w", name, err)
	}
	return nil
}

// isNameConflict reports whether err is Docker's "that container name is
// taken" response. The installed SDK (v25.0.6) wraps the daemon's 409 as
// an errdefs.ErrConflict; the message is checked too because the same
// condition reaches us as a plain error through some code paths.
func isNameConflict(err error) bool {
	if errdefs.IsConflict(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already in use") || strings.Contains(msg, "already exists")
}

// OwnImage is the image this daemon is running from, read from its own
// container.
//
// Asked for rather than configured, and that is the opposite call to
// CUBESHIP_WEB_IMAGE — for a reason. The dashboard's image has to be
// *derived* from the daemon's, and deriving means string surgery on a
// registry path an operator is free to change. This is not derivation:
// it is the Engine being asked what this container was created from,
// which is exactly right whatever anybody named it.
//
// Empty when the daemon is not a container, which is `make dev`. The one
// caller — hostexec — is off there anyway.
func OwnImage(ctx context.Context, docker dockerAPI, cfg *config.Config) string {
	if !cfg.InContainer {
		return ""
	}
	info, err := docker.InspectContainerByName(ctx, DaemonContainerName)
	if err != nil {
		return ""
	}
	return info.Image
}
