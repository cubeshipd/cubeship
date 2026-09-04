// Package buildkit turns a directory of source into an image the daemon
// can run.
//
// The result is loaded straight into the Docker Engine rather than
// pushed anywhere: on one VPS the image never has to leave the box, and
// a registry round-trip would mean credentials, a reachable host and a
// certificate — three things that can be missing on a fresh install.
package buildkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/buildkit/client"
	"golang.org/x/sync/errgroup"
)

// Loader is what receives the built image: the Docker Engine, which
// imports the tarball BuildKit writes.
type Loader interface {
	LoadImage(ctx context.Context, r io.Reader) error
}

// Builder builds images through a buildkitd.
type Builder struct {
	addr   string
	loader Loader
}

// New returns a Builder that talks to buildkitd at addr — a
// "unix://…" socket in a real install, "tcp://…" in a test.
//
// It does not dial: buildkitd may still be starting, and a build that
// happens later should not be refused because of where the daemon was in
// its own startup.
func New(addr string, loader Loader) *Builder {
	return &Builder{addr: addr, loader: loader}
}

// ErrUnavailable reports a buildkitd that could not be reached.
var ErrUnavailable = errors.New("the image builder is not available")

// Request is one build.
type Request struct {
	// ContextDir is a directory on the daemon's own filesystem. Its
	// contents are streamed to buildkitd, so the builder container needs
	// no access to it.
	ContextDir string

	// Dockerfile is the build recipe's path, relative to ContextDir.
	// Defaults to "Dockerfile".
	Dockerfile string

	// Image is what the result is named, tag included.
	Image string

	// Args are the Dockerfile's build arguments.
	Args map[string]string

	// Labels go onto the built image.
	Labels map[string]string
}

// Build runs one build, writing BuildKit's progress to logs as it goes,
// and loads the result into the Engine.
//
// logs is written to while the build runs rather than returned at the
// end, because a build is the one part of a deploy long enough that
// watching it is the point.
func (b *Builder) Build(ctx context.Context, req Request, logs io.Writer) error {
	if req.ContextDir == "" {
		return fmt.Errorf("build: no context directory")
	}
	if req.Image == "" {
		return fmt.Errorf("build: the result has no name")
	}
	dockerfile := req.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	// The recipe is streamed as its own local directory, so a Dockerfile
	// in a subdirectory works without sending that subdirectory as the
	// whole context.
	recipeDir := filepath.Join(req.ContextDir, filepath.Dir(dockerfile))
	if _, err := os.Stat(filepath.Join(recipeDir, filepath.Base(dockerfile))); err != nil {
		return fmt.Errorf("build: %s not found in the source", dockerfile)
	}

	c, err := client.New(ctx, b.addr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer c.Close()

	// client.New does not dial, so an unreachable builder would
	// otherwise surface as a solve failure buried in gRPC wording. One
	// round trip buys an answer someone reading a deployment row can act
	// on, and it costs nothing against a build.
	if _, err := c.ListWorkers(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	// The tarball BuildKit writes goes straight into the Engine, so the
	// image is never held in memory or on disk in full.
	pr, pw := io.Pipe()

	frontendAttrs := map[string]string{"filename": filepath.Base(dockerfile)}
	for k, v := range req.Args {
		frontendAttrs["build-arg:"+k] = v
	}
	for k, v := range req.Labels {
		frontendAttrs["label:"+k] = v
	}

	opt := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalDirs: map[string]string{
			"context":    req.ContextDir,
			"dockerfile": recipeDir,
		},
		Exports: []client.ExportEntry{{
			Type:   client.ExporterDocker,
			Attrs:  map[string]string{"name": req.Image},
			Output: func(map[string]string) (io.WriteCloser, error) { return pw, nil },
		}},
	}

	group, ctx := errgroup.WithContext(ctx)
	status := make(chan *client.SolveStatus)

	group.Go(func() error {
		_, err := c.Solve(ctx, nil, opt, status)
		// Closing the pipe is what ends the load below, success or not.
		// Without it a failed build leaves the loader waiting forever.
		pw.CloseWithError(err)
		if err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
		return nil
	})

	group.Go(func() error {
		writeStatus(status, logs)
		return nil
	})

	group.Go(func() error {
		if err := b.loader.LoadImage(ctx, pr); err != nil {
			// Drain, so a loader that gave up does not wedge the solve.
			io.Copy(io.Discard, pr)
			return err
		}
		return nil
	})

	return group.Wait()
}

// writeStatus renders BuildKit's progress as the lines someone reading a
// deploy log would expect. It is deliberately plain: the fancy renderer
// BuildKit ships redraws a terminal, and this is going into a database
// column and a browser.
func writeStatus(status <-chan *client.SolveStatus, logs io.Writer) {
	seen := make(map[string]bool)
	for s := range status {
		if logs == nil {
			continue
		}
		for _, v := range s.Vertexes {
			if v.Completed == nil || seen[v.Digest.String()] {
				continue
			}
			seen[v.Digest.String()] = true
			if v.Error != "" {
				fmt.Fprintf(logs, "ERROR %s: %s\n", v.Name, v.Error)
				continue
			}
			if v.Cached {
				fmt.Fprintf(logs, "CACHED %s\n", v.Name)
				continue
			}
			fmt.Fprintf(logs, "%s\n", v.Name)
		}
		// A build's own output — the compiler, the package manager — is
		// the part someone actually reads when it fails.
		for _, l := range s.Logs {
			line := strings.TrimRight(string(l.Data), "\n")
			if line != "" {
				fmt.Fprintf(logs, "%s\n", line)
			}
		}
	}
}
