package app

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/mcpx"
	"cubeship/internal/user"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (t *Tools) deploy(ctx context.Context, _ *mcp.CallToolRequest, in deployInput) (*mcp.CallToolResult, user.ActionResult, error) {
	tag := in.Tag
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	a, deployment, err := t.svc.Deploy(ctx, t.caller, ref, tag)
	if err != nil {
		return nil, user.ActionResult{}, err
	}

	// The deploy runs detached, so waiting here is a convenience, not a
	// dependency: if this call times out the deploy carries on, and
	// get_app_deployments reports how it ended.
	finished, waitErr := t.svc.WaitForDeployment(ctx, t.caller, ref, deployment.ID)
	if waitErr != nil {
		return nil, user.ActionResult{Message: fmt.Sprintf(
			"deploy %d of %s from tag %s is still running; check it with get_app_deployments",
			deployment.ID, a.Name, tag)}, nil
	}
	if finished.Status == DeploymentFailed {
		return nil, user.ActionResult{}, fmt.Errorf("deploy of %s from tag %s failed: %s", a.Name, tag, finished.Error)
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deployed %s from tag %s", a.Name, tag)}, nil
}

type deploymentOutput struct {
	ID         int64  `json:"id"`
	Status     string `json:"status" jsonschema:"pending, succeeded or failed"`
	Phase      string `json:"phase"`
	Duration   int64  `json:"duration_ms"`
	Cancelled  bool   `json:"cancelled"`
	Image      string `json:"image"`
	GitSHA     string `json:"git_sha,omitempty"`
	GitBranch  string `json:"git_branch,omitempty"`
	Error      string `json:"error,omitempty" jsonschema:"why it failed, when it did"`
	Restorable bool   `json:"restorable" jsonschema:"whether rollback_app accepts this deployment"`
	CreatedAt  string `json:"created_at"`
}

func (t *Tools) deployments(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, mcpx.List[deploymentOutput], error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, mcpx.List[deploymentOutput]{}, err
	}
	history, err := t.svc.Deployments(ctx, t.caller, ref)
	if err != nil {
		return nil, mcpx.List[deploymentOutput]{}, err
	}
	out := make([]deploymentOutput, 0, len(history))
	for _, d := range history {
		out = append(out, deploymentOutput{
			ID: d.ID, Status: d.Status, Phase: d.Phase, Duration: d.Duration().Milliseconds(), Cancelled: d.Cancelled, Image: d.ImageRef, Error: d.Error, GitSHA: d.GitSHA, GitBranch: d.GitBranch,
			Restorable: d.Restorable, CreatedAt: d.CreatedAt.Format(time.RFC3339),
		})
	}
	return nil, mcpx.Of(out), nil
}

func (t *Tools) rollback(ctx context.Context, _ *mcp.CallToolRequest, in rollbackInput) (*mcp.CallToolResult, user.ActionResult, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	a, deployment, err := t.svc.Rollback(ctx, t.caller, ref, in.ID)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	finished, waitErr := t.svc.WaitForDeployment(ctx, t.caller, ref, deployment.ID)
	if waitErr != nil {
		return nil, user.ActionResult{Message: fmt.Sprintf(
			"rollback of %s to deployment %d is still running as deploy %d; check it with get_app_deployments",
			a.Name, in.ID, deployment.ID)}, nil
	}
	if finished.Status == DeploymentFailed {
		return nil, user.ActionResult{}, fmt.Errorf("rollback of %s to deployment %d failed: %s", a.Name, in.ID, finished.Error)
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("rolled %s back to deployment %d", a.Name, in.ID)}, nil
}

func (t *Tools) cancelDeployment(ctx context.Context, _ *mcp.CallToolRequest, in cancelDeploymentInput) (*mcp.CallToolResult, user.ActionResult, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	if err := t.svc.CancelDeployment(ctx, t.caller, ref, in.ID); err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("cancelled deployment %d of %s", in.ID, ref)}, nil
}

func (t *Tools) delete(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, user.ActionResult, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	if _, err := t.svc.Delete(ctx, t.caller, ref); err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deleted app %s", ref)}, nil
}

type volumeOutput struct {
	ID   int64  `json:"id"`
	Path string `json:"path" jsonschema:"where the container sees the volume"`
	Node string `json:"node" jsonschema:"the machine its data is on"`
}

type volumesOutput struct {
	Volumes []volumeOutput `json:"volumes"`
}

func (t *Tools) listVolumes(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, volumesOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, volumesOutput{}, err
	}
	volumes, err := t.svc.Volumes(ctx, t.caller, ref)
	if err != nil {
		return nil, volumesOutput{}, err
	}
	out := volumesOutput{Volumes: []volumeOutput{}}
	for _, v := range volumes {
		out.Volumes = append(out.Volumes, volumeOutput{ID: v.ID, Path: v.Path, Node: v.NodeSlug})
	}
	return nil, out, nil
}

type addVolumeInput struct {
	App  string `json:"app" jsonschema:"app reference: project/environment/app, or project/app for production"`
	Path string `json:"path" jsonschema:"absolute path inside the container, e.g. /var/lib/rabbitmq. Not /, and not under /proc, /sys or /dev"`
}

func (t *Tools) addVolume(ctx context.Context, _ *mcp.CallToolRequest, in addVolumeInput) (*mcp.CallToolResult, volumeOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, volumeOutput{}, err
	}
	v, err := t.svc.AddVolume(ctx, t.caller, ref, in.Path)
	if err != nil {
		return nil, volumeOutput{}, err
	}
	return nil, volumeOutput{ID: v.ID, Path: v.Path, Node: v.NodeSlug}, nil
}

type tcpPortOutput struct {
	ID            int64 `json:"id"`
	ContainerPort int   `json:"container_port" jsonschema:"what the app listens on inside its container"`
	HostPort      int   `json:"host_port" jsonschema:"where the control plane publishes it"`
}

type tcpPortsOutput struct {
	Ports []tcpPortOutput `json:"ports"`
}

func (t *Tools) listTCPPorts(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, tcpPortsOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, tcpPortsOutput{}, err
	}
	ports, err := t.svc.TCPPorts(ctx, t.caller, ref)
	if err != nil {
		return nil, tcpPortsOutput{}, err
	}
	out := tcpPortsOutput{Ports: []tcpPortOutput{}}
	for _, p := range ports {
		out.Ports = append(out.Ports, tcpPortOutput{ID: p.ID, ContainerPort: p.ContainerPort, HostPort: p.HostPort})
	}
	return nil, out, nil
}

type envOutput struct {
	Vars      envvar.Map        `json:"vars" jsonschema:"the variables set on the app itself"`
	Effective []envvar.Resolved `json:"effective" jsonschema:"every variable the container actually runs with, and the level that set it"`
}

func (t *Tools) getEnv(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, envOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, envOutput{}, err
	}
	own, effective, err := t.svc.Env(ctx, t.caller, ref)
	if err != nil {
		return nil, envOutput{}, err
	}
	return nil, envOutput{Vars: own, Effective: effective}, nil
}

type setEnvInput struct {
	App   string     `json:"app" jsonschema:"app reference: project/environment/app, or project/app for production"`
	Set   envvar.Map `json:"set,omitempty" jsonschema:"variables to add or overwrite"`
	Unset []string   `json:"unset,omitempty" jsonschema:"names of variables to remove"`
}

// setEnv merges rather than replaces. An agent that means to change one
// variable must not be able to erase the rest by omitting them, which is
// exactly what the old replace-everything tool made easy.
func (t *Tools) setEnv(ctx context.Context, _ *mcp.CallToolRequest, in setEnvInput) (*mcp.CallToolResult, user.ActionResult, error) {
	if len(in.Set) == 0 && len(in.Unset) == 0 {
		return nil, user.ActionResult{}, fmt.Errorf("give set, unset, or both")
	}
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	a, err := t.svc.MergeEnv(ctx, t.caller, ref, in.Set, in.Unset)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("updated env for app %s", a.Name)}, nil
}

type logsInput struct {
	App  string `json:"app" jsonschema:"app reference: project/environment/app, or project/app for production"`
	Tail string `json:"tail,omitempty" jsonschema:"number of trailing lines to return, e.g. \"500\", or \"all\" for the full log (default \"200\")"`
	// An app spread over several machines has one log per machine and
	// no combined one. Naming none gets the machine its traffic
	// arrives at.
	Server string `json:"server,omitempty" jsonschema:"which server's copy to read, for an app that runs on more than one; defaults to the one serving its names"`
}

func (t *Tools) logs(ctx context.Context, _ *mcp.CallToolRequest, in logsInput) (*mcp.CallToolResult, any, error) {
	tail := in.Tail
	if tail == "" {
		tail = mcpDefaultLogTail
	}
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, nil, err
	}
	rc, err := t.svc.Logs(ctx, t.caller, ref, in.Server, tail)
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()

	// Containers run without a TTY, so the Engine multiplexes
	// stdout/stderr behind an 8-byte binary frame header per chunk —
	// demultiplex it before this is readable at all.
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, rc); err != nil {
		return nil, nil, fmt.Errorf("read logs: %w", err)
	}

	text := buf.String()
	if len(text) > maxMCPLogBytes {
		text = "...[truncated; showing the most recent output]...\n" + text[len(text)-maxMCPLogBytes:]
	}
	if text == "" {
		text = "(no log output)"
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

type domainOutput struct {
	ID   int64  `json:"id"`
	Host string `json:"host" jsonschema:"the name a browser asks for"`
	Port int    `json:"port" jsonschema:"the port inside the container that name reaches"`
}

type domainsOutput struct {
	Domains []domainOutput `json:"domains"`
}

// Named by host rather than by id, in every one of these. An agent has
// the name — it is what it was told to fix — and making it list the
// domains to turn that name into a number is a round trip that can only
// go wrong.
type domainInput struct {
	App  string `json:"app" jsonschema:"app reference: project/environment/app, or project/app for production"`
	Host string `json:"host" jsonschema:"the name, e.g. api.example.com"`
}

type domainPortInput struct {
	App  string `json:"app" jsonschema:"app reference: project/environment/app, or project/app for production"`
	Host string `json:"host" jsonschema:"the name, e.g. api.example.com"`
	Port int    `json:"port" jsonschema:"the port inside the container this name reaches. Omit for 8080"`
}

func (t *Tools) listDomains(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, domainsOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	a, err := t.svc.Resolve(ctx, t.caller, ref, user.LevelView)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	return nil, toDomainsOutput(a), nil
}

func (t *Tools) addDomain(ctx context.Context, _ *mcp.CallToolRequest, in domainPortInput) (*mcp.CallToolResult, domainsOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	a, err := t.svc.AddDomain(ctx, t.caller, ref, in.Host, in.Port)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	return nil, toDomainsOutput(a), nil
}

func (t *Tools) setDomainPort(ctx context.Context, _ *mcp.CallToolRequest, in domainPortInput) (*mcp.CallToolResult, domainsOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	id, err := t.domainID(ctx, ref, in.Host)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	a, err := t.svc.SetDomainPort(ctx, t.caller, ref, id, in.Port)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	return nil, toDomainsOutput(a), nil
}

func (t *Tools) removeDomain(ctx context.Context, _ *mcp.CallToolRequest, in domainInput) (*mcp.CallToolResult, domainsOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	id, err := t.domainID(ctx, ref, in.Host)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	a, err := t.svc.RemoveDomain(ctx, t.caller, ref, id)
	if err != nil {
		return nil, domainsOutput{}, err
	}
	return nil, toDomainsOutput(a), nil
}

// domainID turns a host into the id the service takes. It reads the app
// as a viewer: the service asks the real question about who may change a
// name, and refusing here on a weaker one would answer "no such domain"
// to somebody who should have been told "not yours".
func (t *Tools) domainID(ctx context.Context, ref Reference, host string) (int64, error) {
	a, err := t.svc.Resolve(ctx, t.caller, ref, user.LevelView)
	if err != nil {
		return 0, err
	}
	want := NormalizeHost(host)
	for _, d := range a.Domains {
		if d.Host == want {
			return d.ID, nil
		}
	}
	return 0, ErrDomainNotFound
}

func toDomainsOutput(a *Scoped) domainsOutput {
	out := domainsOutput{Domains: []domainOutput{}}
	for _, d := range a.Domains {
		port := d.Port
		if port == 0 {
			port = DefaultPort
		}
		out.Domains = append(out.Domains, domainOutput{ID: d.ID, Host: d.Host, Port: port})
	}
	return out
}
