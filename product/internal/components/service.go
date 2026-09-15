package components

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/user"
	"github.com/docker/docker/pkg/stdcopy"
)

const CommandInventory = "components"
const CommandLogs = "component-logs"

var ErrUnknown = errors.New("unknown Cubeship component")
var ErrUnavailable = errors.New("this machine's components are unavailable")
var ErrTail = errors.New("tail must be between 1 and 5000 lines")

type Engine interface {
	InspectContainerByName(context.Context, string) (dockerx.ContainerInfo, error)
	Logs(context.Context, string, string) (io.ReadCloser, error)
}

type Service struct {
	engine Engine
	nodes  *node.Service
}

func NewService(engine Engine, nodes *node.Service) *Service {
	return &Service{engine: engine, nodes: nodes}
}

func definition(id string) (Component, error) {
	for _, c := range definitions {
		if c.ID == id {
			return c, nil
		}
	}
	return Component{}, ErrUnknown
}

// Inspect reads only fixed Cubeship container names, never arbitrary containers
// or environment variables. Stopped containers retain their logs.
func Inspect(ctx context.Context, engine Engine, worker bool) ([]Component, error) {
	if engine == nil {
		return nil, ErrUnavailable
	}
	out := make([]Component, 0, len(definitions))
	for _, c := range definitions {
		info, err := engine.InspectContainerByName(ctx, c.Container)
		if errors.Is(err, dockerx.ErrContainerNotFound) {
			if worker && c.ID != "daemon" {
				continue
			}
			c.Status = "not-installed"
			c.Detail = "No container on this machine. It may run externally or be started on demand."
		} else if err != nil {
			c.Status = "unavailable"
			c.Detail = "Docker could not inspect this component."
		} else {
			c.Status = info.Status
			if c.Status == "" {
				if info.Running {
					c.Status = "running"
				} else {
					c.Status = "stopped"
				}
			}
			c.Image, c.Health, c.StartedAt, c.Restarts = info.Image, info.Health, info.StartedAt, info.Restarts
			c.LogsAvailable = true
		}
		if worker && c.ID == "daemon" {
			c.Name = "Cubeship worker"
			c.Description = "Worker agent, application runtime and host telemetry."
		}
		out = append(out, c)
	}
	return out, nil
}

func ReadLogs(ctx context.Context, engine Engine, id, tail string) ([]byte, error) {
	c, err := definition(id)
	if err != nil {
		return nil, err
	}
	if engine == nil {
		return nil, ErrUnavailable
	}
	if tail == "" {
		tail = "200"
	}
	n, err := strconv.Atoi(tail)
	if err != nil || n < 1 || n > 5000 {
		return nil, ErrTail
	}
	info, err := engine.InspectContainerByName(ctx, c.Container)
	if err != nil {
		return nil, err
	}
	stream, err := engine.Logs(ctx, c.Container, tail)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	raw, err := io.ReadAll(io.LimitReader(stream, node.MaxAnswerBytes))
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if info.TTY {
		output.Write(raw)
	} else {
		_, err = stdcopy.StdCopy(&output, &output, bytes.NewReader(raw))
		if err != nil && len(raw) < node.MaxAnswerBytes {
			return nil, err
		}
	}
	if len(raw) == node.MaxAnswerBytes {
		const notice = "\n[Log output truncated at 2 MiB. Select a smaller tail.]\n"
		if output.Len() > node.MaxAnswerBytes-len(notice) {
			output.Truncate(node.MaxAnswerBytes - len(notice))
		}
		output.WriteString(notice)
	}
	return output.Bytes(), nil
}

func (s *Service) target(ctx context.Context, caller *user.User, server string) (*node.Node, error) {
	// Infrastructure logs can contain operational secrets; application log grants
	// must not also grant access to the daemon, registry or instance database.
	if err := user.Require(caller, user.RoleAdmin); err != nil {
		return nil, err
	}
	if server == "" {
		server = node.ControlPlaneSlug
	}
	n, err := s.nodes.Get(ctx, caller, server)
	if err != nil {
		return nil, err
	}
	if !n.ControlPlane && n.Status() != "ready" {
		return nil, fmt.Errorf("%w: %s is %s", ErrUnavailable, n.Slug, n.Status())
	}
	return n, nil
}

func (s *Service) List(ctx context.Context, caller *user.User, server string) (Inventory, error) {
	n, err := s.target(ctx, caller, server)
	if err != nil {
		return Inventory{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, node.CommandTimeout)
	defer cancel()
	out := Inventory{Server: n.Slug}
	if n.ControlPlane {
		out.Components, err = Inspect(ctx, s.engine, false)
	} else {
		var data []byte
		data, err = s.nodes.Request(ctx, n.ID, node.Command{Kind: CommandInventory})
		if err == nil {
			err = json.Unmarshal(data, &out.Components)
		}
	}
	return out, err
}

func (s *Service) Logs(ctx context.Context, caller *user.User, server, id, tail string) ([]byte, error) {
	n, err := s.target(ctx, caller, server)
	if err != nil {
		return nil, err
	}
	if _, err = definition(id); err != nil {
		return nil, err
	}
	if tail == "" {
		tail = "200"
	}
	amount, err := strconv.Atoi(tail)
	if err != nil || amount < 1 || amount > 5000 {
		return nil, ErrTail
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if n.ControlPlane {
		return ReadLogs(ctx, s.engine, id, tail)
	}
	return s.nodes.Request(ctx, n.ID, node.Command{Kind: CommandLogs, Container: id, Tail: tail})
}
