package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"cubeship/internal/node"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/dirarchive"
)

// s3Opener opens the bucket a volume job names. A test supplies its own.
type s3Opener func(ctx context.Context, o node.S3Object) (objectstore.Client, error)

func openS3(ctx context.Context, o node.S3Object) (objectstore.Client, error) {
	return objectstore.S3Connector{}.Connect(ctx, &objectstore.Store{
		Kind: objectstore.KindExternal, Endpoint: o.Endpoint, Region: o.Region,
		Secure: o.Secure, PathStyle: o.PathStyle, AccessKey: o.AccessKey, SecretKey: o.SecretKey,
	})
}

// volumeJob backs up or restores a volume on this machine and posts how it
// went. It runs on its own goroutine: a copy takes minutes, and the loop
// that calls home has to keep calling meanwhile.
func (a *Agent) volumeJob(cmd node.Command) {
	ctx, cancel := context.WithTimeout(context.Background(), node.VolumeJobTimeout)
	defer cancel()

	size, err := a.runVolumeJob(ctx, cmd)
	var out []byte
	if err != nil {
		log.Printf("agent: %s: %v", cmd.Kind, err)
	} else {
		out, _ = json.Marshal(node.VolumeJobResult{Size: size})
	}
	if err := a.post(context.WithoutCancel(ctx), cmd.ID, out, err); err != nil {
		log.Printf("agent: answering %s: %v", cmd.Kind, err)
	}
}

func (a *Agent) runVolumeJob(ctx context.Context, cmd node.Command) (int64, error) {
	job := cmd.Volume
	if job == nil || job.ID == 0 {
		return 0, fmt.Errorf("the command names no volume")
	}
	if a.engine == nil {
		return 0, fmt.Errorf("this server has no Docker")
	}
	c, err := a.s3(ctx, job.S3)
	if err != nil {
		return 0, err
	}
	dir := filepath.Join(a.dataDir, "volumes", strconv.FormatInt(job.ID, 10))

	var size int64
	err = a.withAppStopped(ctx, job.App, func() error {
		if cmd.Kind == node.CommandVolumeRestore {
			return download(ctx, c, dir, job.S3)
		}
		n, err := upload(ctx, c, dir, job.S3)
		size = n
		return err
	})
	return size, err
}

// withAppStopped stops an app's containers on this machine, runs fn, and
// starts them again whatever fn returned. The app is paused meanwhile, so
// apply does not take a stopped container for a missing one.
func (a *Agent) withAppStopped(ctx context.Context, app string, fn func() error) error {
	a.pause(app)
	defer a.resume(app)

	running, err := a.engine.RunningContainers(ctx)
	if err != nil {
		return fmt.Errorf("list what is running: %w", err)
	}
	var stopped []string
	defer func() {
		for _, id := range stopped {
			if err := a.engine.StartContainer(context.WithoutCancel(ctx), id); err != nil {
				log.Printf("agent: starting %s again: %v", id, err)
			}
		}
	}()
	for _, c := range running {
		if c.Labels[node.LabelApp] != app {
			continue
		}
		if err := a.engine.StopContainer(ctx, c.ID); err != nil {
			return fmt.Errorf("stop the app: %w", err)
		}
		stopped = append(stopped, c.ID)
	}
	return fn()
}

func (a *Agent) pause(app string) {
	a.jobs.Lock()
	defer a.jobs.Unlock()
	if a.paused == nil {
		a.paused = map[string]int{}
	}
	a.paused[app]++
}

func (a *Agent) resume(app string) {
	a.jobs.Lock()
	defer a.jobs.Unlock()
	if a.paused[app]--; a.paused[app] <= 0 {
		delete(a.paused, app)
	}
}

func (a *Agent) isPaused(app string) bool {
	a.jobs.Lock()
	defer a.jobs.Unlock()
	return a.paused[app] > 0
}

// upload streams a tar.gz of dir into the bucket, with nothing staged.
func upload(ctx context.Context, c objectstore.Client, dir string, o node.S3Object) (int64, error) {
	if _, err := os.Stat(dir); err != nil {
		return 0, fmt.Errorf("read the volume: %w", err)
	}
	pr, pw := io.Pipe()
	counted := &countingWriter{w: pw}
	written := make(chan error, 1)
	go func() {
		entries, err := dirarchive.Write(dir, counted)
		if err == nil && entries == 0 {
			err = dirarchive.ErrEmpty
		}
		// An error here breaks the upload rather than finishing it: the
		// client abandons an object whose reader failed.
		pw.CloseWithError(err)
		written <- err
	}()
	putErr := c.Put(ctx, o.Bucket, o.Key, pr, -1, "application/gzip")
	if putErr != nil {
		// Unblocks the archive if the upload gave up first.
		pr.CloseWithError(putErr)
	}
	if err := <-written; err != nil {
		if putErr == nil {
			_ = c.Remove(ctx, o.Bucket, o.Key)
		}
		return 0, err
	}
	if putErr != nil {
		return 0, fmt.Errorf("upload the archive: %w", putErr)
	}
	return counted.n, nil
}

// download replaces dir with the archive in the bucket.
func download(ctx context.Context, c objectstore.Client, dir string, o node.S3Object) error {
	r, _, err := c.Get(ctx, o.Bucket, o.Key)
	if err != nil {
		return fmt.Errorf("read the backup: %w", err)
	}
	defer r.Close()
	return dirarchive.Replace(r, dir)
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
