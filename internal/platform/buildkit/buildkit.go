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
	"time"

	"github.com/moby/buildkit/client"
	"github.com/tonistiigi/fsutil"
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

	// EnsureRunning starts buildkitd if it is not up, and is called
	// before every build. The daemon sets it; the package does not know
	// about containers. Nil means "assume something else runs it",
	// which is what the tests do.
	EnsureRunning func(ctx context.Context) error
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

	// ContextGit is a repository BuildKit clones itself, as an
	// alternative to ContextDir. Doing the clone there rather than here
	// means no git on the host and a clone BuildKit can cache between
	// builds. Append "#ref" to build something other than the default
	// branch.
	ContextGit string

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
	if req.ContextDir == "" && req.ContextGit == "" {
		return fmt.Errorf("build: no source to build")
	}
	if req.ContextDir != "" && req.ContextGit != "" {
		return fmt.Errorf("build: a build has one source, not two")
	}
	if req.Image == "" {
		return fmt.Errorf("build: the result has no name")
	}
	dockerfile := req.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	frontendAttrs := map[string]string{"filename": dockerfile}
	localDirs := map[string]string{}

	if req.ContextGit != "" {
		// BuildKit resolves this itself, inside the builder, and the
		// dockerfile is read from the same clone.
		frontendAttrs["context"] = req.ContextGit
	} else {
		// The recipe is streamed as its own local directory, so a
		// Dockerfile in a subdirectory works without sending that
		// subdirectory as the whole context.
		recipeDir := filepath.Join(req.ContextDir, filepath.Dir(dockerfile))
		if _, err := os.Stat(filepath.Join(recipeDir, filepath.Base(dockerfile))); err != nil {
			return fmt.Errorf("build: %s not found in the source", dockerfile)
		}
		frontendAttrs["filename"] = filepath.Base(dockerfile)
		localDirs["context"] = req.ContextDir
		localDirs["dockerfile"] = recipeDir
	}

	for k, v := range req.Args {
		frontendAttrs["build-arg:"+k] = v
	}
	for k, v := range req.Labels {
		frontendAttrs["label:"+k] = v
	}

	return b.solve(ctx, solveRequest{
		image:     req.Image,
		frontend:  "dockerfile.v0",
		attrs:     frontendAttrs,
		localDirs: localDirs,
	}, logs)
}

// solveRequest is what the two build paths — a Dockerfile and a Railpack
// plan — both come down to. They differ only in which frontend reads the
// source and what it is told; everything after that is one code path.
type solveRequest struct {
	image     string
	frontend  string
	attrs     map[string]string
	localDirs map[string]string
}

// localMounts turns directories on the daemon's filesystem into what
// BuildKit streams to the builder. LocalDirs was the old spelling and is
// gone from the client.
func localMounts(dirs map[string]string) (map[string]fsutil.FS, error) {
	out := make(map[string]fsutil.FS, len(dirs))
	for name, dir := range dirs {
		fs, err := fsutil.NewFS(dir)
		if err != nil {
			return nil, fmt.Errorf("read %s from %s: %w", name, dir, err)
		}
		out[name] = fs
	}
	return out, nil
}

func (b *Builder) solve(ctx context.Context, req solveRequest, logs io.Writer) error {
	if req.image == "" {
		return fmt.Errorf("build: the result has no name")
	}
	if b.EnsureRunning != nil {
		if err := b.EnsureRunning(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}

	c, err := client.New(ctx, b.addr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer c.Close()

	// client.New does not dial, so an unreachable builder would
	// otherwise surface as a solve failure buried in gRPC wording. This
	// also waits: EnsureRunning may have just started the container, and
	// buildkitd takes a moment to open its socket.
	if err := waitReady(ctx, c); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	// The tarball BuildKit writes goes straight into the Engine, so the
	// image is never held in memory or on disk in full.
	pr, pw := io.Pipe()

	mounts, err := localMounts(req.localDirs)
	if err != nil {
		return err
	}

	opt := client.SolveOpt{
		Frontend:      req.frontend,
		FrontendAttrs: req.attrs,
		LocalMounts:   mounts,
		Exports: []client.ExportEntry{{
			Type:   client.ExporterDocker,
			Attrs:  map[string]string{"name": req.image},
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

// readyTimeout bounds how long a build waits for a builder that is
// starting. It is generous because the very first build on a box is also
// pulling the builder's image.
const readyTimeout = 90 * time.Second

func waitReady(ctx context.Context, c *client.Client) error {
	deadline := time.Now().Add(readyTimeout)
	for {
		workers, err := c.ListWorkers(ctx)
		if err == nil && len(workers) > 0 {
			return nil
		}
		if err == nil {
			err = errors.New("the builder reports no workers")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
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
