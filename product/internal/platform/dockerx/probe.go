package dockerx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
)

// ConfigureProbes pins helpers to the running binary's image, never a mutable
// tag. Containerized daemons must not fall back to their isolated network.
func (c *Client) ConfigureProbes(ctx context.Context, daemon string) error {
	c.probeInContainer = true
	info, err := c.api.ContainerInspect(ctx, daemon)
	if err != nil {
		return fmt.Errorf("resolve health probe image: %w", err)
	}
	if info.Image == "" {
		return errors.New("running daemon has no health probe image")
	}
	c.probeImage = info.Image
	return nil
}

func (c *Client) ProbeHTTP(ctx context.Context, id, path string, port int) error {
	return c.probe(ctx, id, "http", path, port)
}

func (c *Client) ProbeTCP(ctx context.Context, id string, port int) error {
	return c.probe(ctx, id, "tcp", "", port)
}

func (c *Client) probe(ctx context.Context, id, protocol, path string, port int) (err error) {
	if c.probeInContainer && c.probeImage == "" {
		return errors.New("health probe helper image is unavailable")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid health probe port %d", port)
	}
	info, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return err
	}
	if info.NetworkSettings == nil {
		return errors.New("container has no network")
	}
	endpoint := info.NetworkSettings.Networks[ApplicationNetwork]
	if endpoint == nil || net.ParseIP(endpoint.IPAddress) == nil {
		return errors.New("container has no application-network address")
	}
	address := net.JoinHostPort(endpoint.IPAddress, strconv.Itoa(port))
	if !c.probeInContainer {
		return RunProbe(ctx, protocol, address, path)
	}

	// Only the helper shares the app's network namespace. No host namespaces,
	// mounts, credentials or Docker socket enter it, and it opens no listener.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	helper, err := c.api.ContainerCreate(ctx, &container.Config{
		Image: c.probeImage, User: "65534:65534",
		Entrypoint: []string{"/usr/local/bin/cubeshipd"},
		Cmd:        []string{"-probe", protocol, "-probe-address", address, "-probe-path", path},
		Labels:     map[string]string{"cubeship.health-probe": "true"},
	}, &container.HostConfig{
		NetworkMode:    container.NetworkMode("container:" + id),
		ReadonlyRootfs: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges:true"},
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
		Resources:     container.Resources{Memory: 128 * 1024 * 1024, MemorySwap: 128 * 1024 * 1024, PidsLimit: probePIDLimit()},
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("create health probe: %w", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if cleanupErr := c.api.ContainerRemove(cleanup, helper.ID, container.RemoveOptions{Force: true}); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove health probe %s: %w", helper.ID, cleanupErr))
		}
	}()
	if err = c.api.ContainerStart(ctx, helper.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start health probe: %w", err)
	}
	done, failed := c.api.ContainerWait(ctx, helper.ID, container.WaitConditionNotRunning)
	var status container.WaitResponse
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err = <-failed:
		return fmt.Errorf("wait for health probe: %w", err)
	case status = <-done:
	}
	if status.Error != nil {
		return fmt.Errorf("health probe: %s", status.Error.Message)
	}
	if status.StatusCode == 0 {
		return nil
	}
	if info, inspectErr := c.api.ContainerInspect(ctx, helper.ID); inspectErr == nil && info.State != nil && info.State.OOMKilled {
		return fmt.Errorf("health probe exceeded its memory limit (exit %d)", status.StatusCode)
	}
	logs, err := c.api.ContainerLogs(ctx, helper.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: "10"})
	if err != nil {
		return fmt.Errorf("health probe exited %d; read diagnostic: %w", status.StatusCode, err)
	}
	defer logs.Close()
	body, err := io.ReadAll(io.LimitReader(logs, 4096))
	if err != nil {
		return fmt.Errorf("read health probe diagnostic: %w", err)
	}
	return fmt.Errorf("health probe exited %d: %s", status.StatusCode, strings.TrimSpace(string(demux(body))))
}

func probePIDLimit() *int64 { n := int64(32); return &n }

// RunProbe is the helper's entire workload. It never initializes a daemon or
// connects to Docker. The same operation supports host-process development.
func RunProbe(ctx context.Context, protocol, address, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	switch protocol {
	case "tcp":
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return err
		}
		return conn.Close()
	case "http":
		if !strings.HasPrefix(path, "/") {
			return errors.New("health probe path must start with /")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+path, nil)
		if err != nil {
			return err
		}
		transport := &http.Transport{MaxResponseHeaderBytes: 32 * 1024, DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("health probe returned HTTP %d", resp.StatusCode)
		}
		return nil
	default:
		return fmt.Errorf("unknown health probe protocol %q", protocol)
	}
}
