package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"cubeship/internal/mcpx"
	"cubeship/internal/user"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxMCPLogBytes bounds how much of an app's log get_app_logs returns — a
// large log pasted whole into an LLM's context is mostly waste. Paired
// with a "tail" input so the bytes that do fit are the most recent ones.
const maxMCPLogBytes = 100_000

// mcpDefaultLogTail is deliberately shorter than the HTTP API's: an agent
// reading logs wants the relevant tail, not a transcript.
const mcpDefaultLogTail = "200"

type Tools struct {
	svc    *Service
	caller *user.User
}

func NewTools(svc *Service, caller *user.User) *Tools {
	return &Tools{svc: svc, caller: caller}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:         "create_app",
		InputSchema:  appToolSchema[createInput](),
		OutputSchema: appToolSchema[Response](),
		Description:  `Register a new app in a project and get its registry push path. environment defaults to "production" when omitted. Requires the member role.`,
	}, t.create)
	mcp.AddTool(srv, &mcp.Tool{
		Name:         "list_apps",
		OutputSchema: appToolSchema[mcpx.List[Response]](),
		Description:  "List every app you can see, on this instance.",
	}, t.list)
	mcp.AddTool(srv, &mcp.Tool{
		Name:         "get_app",
		OutputSchema: appToolSchema[Response](),
		Description:  "Get one app by reference: its domain, registry push path, status, and which project and environment it lives in.",
	}, t.get)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "deploy_app",
		Description: `Manually redeploy an app from an image tag already pushed to its registry path (no tag redeploys the tag it is already running, or the stored ref for a source that builds). Waits for the deploy to finish and reports the outcome; if this call times out first, the deploy carries on regardless — check it with get_app_deployments. Requires the member role.`,
	}, t.deploy)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app_env",
		Description: "Read an app's environment variables: the ones set on the app itself, and the effective set its container runs with (project, then environment, then app — each overriding the last), with the source of every value. Read this before changing anything.",
	}, t.getEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_app_env",
		Description: "Add, change or remove an app's own environment variables. Only the keys you name are touched — variables you don't mention are left alone. These are layered on top of (and override) the app's environment's and project's variables. Requires the member role.",
	}, t.setEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app_deployments",
		Description: "List an app's recent deploys, newest first: whether each succeeded, and why it failed if it did. A deploy runs detached from the call that started it, so this is how you find out how one ended.",
	}, t.deployments)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cancel_app_deployment",
		Description: "Cancel an unfinished deployment before its swap, preserving the currently serving version.",
	}, t.cancelDeployment)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "rollback_app",
		Description: "Roll an app back to an earlier deployment: deploys that deployment's image tag again, or its Git commit for an app that builds (reusing the image built for it when it is still there). The app's current environment applies. Only a deployment that succeeded and is not the one running can be rolled back to — get_app_deployments lists them. Waits for the deploy to finish and reports the outcome. Requires the member role.",
	}, t.rollback)
	mcp.AddTool(srv, &mcp.Tool{
		Name:         "update_app",
		InputSchema:  appToolSchema[updateInput](),
		OutputSchema: appToolSchema[Response](),
		Description:  "Reconfigure an app: where its image comes from. Its domains are added and removed one at a time — an app can answer at several, each with its own port — so they are not here. A field you leave out is left as it was. The source and its settings travel together — naming a source without what it needs, or settings the source would ignore, is refused. Moving an app to a source that builds requires the admin role. The app's name cannot be changed.",
	}, t.update)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_app",
		Description: "Delete an app and stop the container serving it. Images already pushed stay in the registry. This cannot be undone. Requires the member role.",
	}, t.delete)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app_logs",
		Description: "Get an app's recent container log output (stdout and stderr combined).",
	}, t.logs)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_app_volumes",
		Description: "List an app's volumes: directories mounted into its container that outlive it, each with the path the container sees and the machine its data is on.",
	}, t.listVolumes)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_app_volume",
		Description: "Give an app a volume: a directory at `path` inside its container whose contents survive deploys and restarts. It is mounted from the app's next deploy, so deploy it afterwards. An app with a volume runs as one copy on one machine — adding one is refused while the app is spread, scaled past one copy or autoscaled — and each deploy stops the old container before the new one starts, so the app is briefly unavailable. Removing a volume is not a tool: deleting data stays with a person. Requires the member role.",
	}, t.addVolume)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_app_domains",
		Description: "List the names an app answers at over HTTP, each with the port inside the container it reaches. Read this before changing one: a name pointing at a port nothing listens on is what a 502 from a healthy container looks like.",
	}, t.listDomains)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_app_domain",
		Description: "Give an app another name to answer at over HTTP, and the port inside its container that name reaches. Point the name's DNS at this instance yourself; the certificate is issued on the first request that arrives. The name starts being served on the app's next deploy. Requires the admin role.",
	}, t.addDomain)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_app_domain_port",
		Description: "Change which port inside the container one of an app's names reaches — the fix when the proxy answers 502 but the container is healthy, because the app listens somewhere other than where the name points. The name keeps its certificate. It takes effect on the app's next deploy. Requires the admin role.",
	}, t.setDomainPort)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "remove_app_domain",
		Description: "Take a name off an app. The app goes on answering there until it is next deployed, which is when the proxy is told. Requires the admin role.",
	}, t.removeDomain)
	// Listing only. Publishing a port puts whatever the app speaks on the
	// open internet, and that stays with a person — the line the
	// datastore tools draw at exposing a database.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_app_tcp_ports",
		Description: "List an app's published TCP ports: each port its container listens on and the host port of the control plane it is published on, for protocols that are not HTTP, such as SSH. Publishing one is not a tool.",
	}, t.listTCPPorts)
}

// Command inputs keep raw JSON to distinguish omission from an explicit clear.
// Predeploy responses marshal as that same union, not as the Go struct. Both
// need the wire schema instead of the underlying []byte or struct schema.
func appToolSchema[T any]() *jsonschema.Schema {
	command := &jsonschema.Schema{
		Types: []string{"string", "array", "null"},
		Items: &jsonschema.Schema{Type: "string"},
	}
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[json.RawMessage](): command,
			reflect.TypeFor[Predeploy]():       command,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("app MCP schema: %v", err))
	}
	return schema
}

type createInput struct {
	Project          string            `json:"project" jsonschema:"project slug"`
	Environment      string            `json:"environment,omitempty" jsonschema:"environment slug (default \"production\")"`
	Name             string            `json:"name" jsonschema:"app name: lowercase letters, digits and dashes — becomes part of its registry image path. Permanent"`
	Source           string            `json:"source,omitempty" jsonschema:"where the image comes from: \"registry\" (the default) for an image you push to Cubeship, \"external\" for one in a registry Cubeship does not run, \"dockerfile\" to build a Dockerfile from a Git repository, or \"railpack\" to build from a Git repository with no Dockerfile. Building requires the admin role."`
	Image            string            `json:"image,omitempty" jsonschema:"for an external app, the image it pulls, without a tag — e.g. \"registry.digitalocean.com/acme/api\". Leave empty otherwise."`
	Tag              string            `json:"tag,omitempty" jsonschema:"the tag to run. Leave empty to follow the registry: on this instance's own that means a push deploys the app, and on any other it means latest. A building app has no tag \u2014 it names a ref."`
	Repo             string            `json:"repo,omitempty" jsonschema:"for a building app, the https:// Git repository to build from. Leave empty otherwise."`
	Ref              string            `json:"ref,omitempty" jsonschema:"for a building app, the branch, tag or commit to build. Defaults to the repository's default branch."`
	Dockerfile       string            `json:"dockerfile,omitempty" jsonschema:"for a dockerfile app only, the recipe's path within the repository. Defaults to \"Dockerfile\" at the root."`
	PredeployCommand json.RawMessage   `json:"predeploy_command,omitempty" jsonschema:"shell string or argv array to run from the new image before swapping; empty string, empty array or null disables it"`
	PredeployTimeout *int64            `json:"predeploy_timeout,omitempty" jsonschema:"pre-deploy timeout in seconds, up to 3600"`
	StopGracePeriod  *int64            `json:"stop_grace_period,omitempty" jsonschema:"container stop grace period in seconds, 0 to 3600"`
	StopSignal       *string           `json:"stop_signal,omitempty" jsonschema:"container stop signal, for example SIGTERM"`
	WatchPaths       *[]string         `json:"watch_paths,omitempty" jsonschema:"include globs; empty preserves deploy-on-every-push"`
	IgnorePaths      *[]string         `json:"ignore_paths,omitempty" jsonschema:"exclude globs applied after watch paths"`
	BuildArgs        map[string]string `json:"build_args,omitempty" jsonschema:"Dockerfile build arguments; values are write-only and may persist in image history"`
}

func (t *Tools) create(ctx context.Context, _ *mcp.CallToolRequest, in createInput) (*mcp.CallToolResult, Response, error) {
	var predeploy *Predeploy
	if len(in.PredeployCommand) > 0 || in.PredeployTimeout != nil {
		p := Predeploy{}
		if len(in.PredeployCommand) > 0 {
			parsed, err := parsePredeployCommand(in.PredeployCommand)
			if err != nil {
				return nil, Response{}, err
			}
			p = *parsed
		}
		if in.PredeployTimeout != nil {
			p.Timeout = time.Duration(*in.PredeployTimeout) * time.Second
		}
		predeploy = &p
	}
	created, err := t.svc.CreateWithPredeploy(ctx, t.caller, in.Project, in.Environment,
		in.Name, Source(in.Source),
		Origin{Image: in.Image, Tag: in.Tag, Repo: in.Repo, Ref: in.Ref, Dockerfile: in.Dockerfile}, predeploy)
	if err != nil {
		return nil, Response{}, err
	}
	if in.WatchPaths != nil || in.IgnorePaths != nil {
		watch, ignore := []string(nil), []string(nil)
		if in.WatchPaths != nil {
			watch = *in.WatchPaths
		}
		if in.IgnorePaths != nil {
			ignore = *in.IgnorePaths
		}
		created, err = t.svc.UpdatePathFilters(ctx, t.caller, Reference{Project: in.Project, Environment: in.Environment, Name: in.Name}, watch, ignore)
		if err != nil {
			return nil, Response{}, err
		}
	}
	if in.BuildArgs != nil {
		created, err = t.svc.UpdateBuildArgs(ctx, t.caller, Reference{Project: in.Project, Environment: in.Environment, Name: in.Name}, in.BuildArgs)
		if err != nil {
			return nil, Response{}, err
		}
	}
	if in.StopGracePeriod != nil || in.StopSignal != nil {
		grace, signal := DefaultStopGracePeriod, DefaultStopSignal
		if in.StopGracePeriod != nil {
			grace = time.Duration(*in.StopGracePeriod) * time.Second
		}
		if in.StopSignal != nil {
			signal = *in.StopSignal
		}
		created, err = t.svc.UpdateStopPolicy(ctx, t.caller, Reference{Project: in.Project, Environment: in.Environment, Name: in.Name}, grace, signal)
		if err != nil {
			return nil, Response{}, err
		}
	}
	return nil, toResponse(created, t.svc.InstanceConfig(ctx)), nil
}

type updateInput struct {
	Reference  string  `json:"reference" jsonschema:"the app's reference: project/environment/name"`
	Source     *string `json:"source,omitempty" jsonschema:"registry, external, dockerfile or railpack. Send the settings the new source needs alongside it"`
	Image      *string `json:"image,omitempty" jsonschema:"for an external app, the image it pulls, without a tag"`
	Tag        *string `json:"tag,omitempty" jsonschema:"the tag to run; send empty to follow the registry, which on this instance's own means a push deploys the app"`
	Repo       *string `json:"repo,omitempty" jsonschema:"for a building app, the https:// Git repository to build from"`
	Ref        *string `json:"ref,omitempty" jsonschema:"for a building app, the branch, tag or commit to build"`
	Dockerfile *string `json:"dockerfile,omitempty" jsonschema:"for a dockerfile app only, the recipe's path within the repository"`
	// Safe for an agent in a way `node` is not: a wrong path takes an
	// app's names to 503 and is undone by clearing the field, where a
	// wrong placement moves somebody's app to a box they were not
	// looking at.
	HealthPath       *string           `json:"health_path,omitempty" jsonschema:"the path Traefik asks this app for before trusting a container with traffic, e.g. /healthz. Send empty to check nothing, which is the default; a path that the app does not answer 2xx or 3xx on takes every replica out of rotation"`
	PredeployCommand json.RawMessage   `json:"predeploy_command,omitempty" jsonschema:"shell string or argv array to run from the new image before swapping it in; omit to preserve, empty string, empty array or null to clear"`
	PredeployTimeout *int64            `json:"predeploy_timeout,omitempty" jsonschema:"pre-deploy timeout in seconds, up to 3600"`
	StopGracePeriod  *int64            `json:"stop_grace_period,omitempty" jsonschema:"container stop grace period in seconds, 0 to 3600"`
	StopSignal       *string           `json:"stop_signal,omitempty" jsonschema:"container stop signal, for example SIGTERM"`
	WatchPaths       *[]string         `json:"watch_paths,omitempty" jsonschema:"include globs; empty preserves deploy-on-every-push"`
	IgnorePaths      *[]string         `json:"ignore_paths,omitempty" jsonschema:"exclude globs applied after watch paths"`
	BuildArgs        map[string]string `json:"build_args,omitempty" jsonschema:"Dockerfile build arguments; values are write-only"`
}

func (t *Tools) update(ctx context.Context, _ *mcp.CallToolRequest, in updateInput) (*mcp.CallToolResult, Response, error) {
	ref, err := ParseReference(in.Reference)
	if err != nil {
		return nil, Response{}, err
	}
	var source *Source
	if in.Source != nil {
		s := Source(*in.Source)
		source = &s
	}
	var origin *Origin
	if in.Image != nil || in.Tag != nil || in.Repo != nil || in.Ref != nil || in.Dockerfile != nil {
		origin = &Origin{
			Image:      deref(in.Image),
			Tag:        deref(in.Tag),
			Repo:       deref(in.Repo),
			Ref:        deref(in.Ref),
			Dockerfile: deref(in.Dockerfile),
		}
	}
	var predeploy *Predeploy
	if len(in.PredeployCommand) > 0 || in.PredeployTimeout != nil {
		current, lookupErr := t.svc.Resolve(ctx, t.caller, ref, user.LevelManage)
		if lookupErr != nil {
			return nil, Response{}, lookupErr
		}
		p := current.Predeploy
		if len(in.PredeployCommand) > 0 {
			parsed, parseErr := parsePredeployCommand(in.PredeployCommand)
			if parseErr != nil {
				return nil, Response{}, parseErr
			}
			p = *parsed
			p.Timeout = current.Predeploy.Timeout
		}
		if in.PredeployTimeout != nil {
			p.Timeout = time.Duration(*in.PredeployTimeout) * time.Second
		}
		predeploy = &p
	}
	if source == nil && origin == nil && in.HealthPath == nil && predeploy == nil && in.StopGracePeriod == nil && in.StopSignal == nil && in.WatchPaths == nil && in.IgnorePaths == nil && in.BuildArgs == nil {
		return nil, Response{}, fmt.Errorf("nothing to change")
	}
	// No `node` here, deliberately: moving an app between machines is
	// an act with a container swap on each side of it, and an agent
	// that can do it is one that can move somebody's app to a box they
	// were not looking at. The API and the dashboard are where that is
	// decided.
	//
	// No `limits` or `autoscale` either, and it is the same line drawn
	// one step closer: a memory ceiling below what a container is already
	// holding is an instant kill by the kernel, with no deploy, no
	// confirmation and nothing to roll back to. An agent can read what
	// the ceiling is — it is on every response — and cannot move it.
	updated, err := t.svc.UpdateWithPredeploy(ctx, t.caller, ref, source, origin, in.HealthPath, nil, nil, nil, predeploy)
	if err != nil {
		return nil, Response{}, err
	}
	if in.WatchPaths != nil || in.IgnorePaths != nil {
		current, lookupErr := t.svc.Resolve(ctx, t.caller, ref, user.LevelManage)
		if lookupErr != nil {
			return nil, Response{}, lookupErr
		}
		watch, ignore := current.WatchPaths, current.IgnorePaths
		if in.WatchPaths != nil {
			watch = *in.WatchPaths
		}
		if in.IgnorePaths != nil {
			ignore = *in.IgnorePaths
		}
		updated, err = t.svc.UpdatePathFilters(ctx, t.caller, ref, watch, ignore)
		if err != nil {
			return nil, Response{}, err
		}
	}
	if in.BuildArgs != nil {
		updated, err = t.svc.UpdateBuildArgs(ctx, t.caller, ref, in.BuildArgs)
		if err != nil {
			return nil, Response{}, err
		}
	}
	if in.StopGracePeriod != nil || in.StopSignal != nil {
		current, lookupErr := t.svc.Resolve(ctx, t.caller, ref, user.LevelManage)
		if lookupErr != nil {
			return nil, Response{}, lookupErr
		}
		grace, signal := current.StopGracePeriod, current.StopSignal
		if in.StopGracePeriod != nil {
			grace = time.Duration(*in.StopGracePeriod) * time.Second
		}
		if in.StopSignal != nil {
			signal = *in.StopSignal
		}
		updated, err = t.svc.UpdateStopPolicy(ctx, t.caller, ref, grace, signal)
		if err != nil {
			return nil, Response{}, err
		}
	}
	return nil, toResponse(updated, t.svc.InstanceConfig(ctx)), nil
}

func parsePredeployCommand(raw json.RawMessage) (*Predeploy, error) {
	var p Predeploy
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid predeploy_command: %w", err)
	}
	return &p, nil
}

func (t *Tools) list(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpx.List[Response], error) {
	apps, err := t.svc.List(ctx, t.caller)
	if err != nil {
		return nil, mcpx.List[Response]{}, err
	}
	return nil, mcpx.Of(toResponses(apps, t.svc.InstanceConfig(ctx))), nil
}

type nameInput struct {
	App string `json:"app" jsonschema:"app reference: project/environment/app, or project/app for production"`
}

type rollbackInput struct {
	App string `json:"app" jsonschema:"app reference: project/environment/app, or project/app for production"`
	ID  int64  `json:"deployment_id" jsonschema:"id of the earlier deployment to roll back to"`
}

type cancelDeploymentInput struct {
	App string `json:"app" jsonschema:"app reference"`
	ID  int64  `json:"deployment_id" jsonschema:"deployment id"`
}

func (t *Tools) get(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, Response, error) {
	a, err := t.svc.ResolveString(ctx, t.caller, in.App, user.LevelView)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(a, t.svc.InstanceConfig(ctx)), nil
}

type deployInput struct {
	App string `json:"app" jsonschema:"app reference: project/environment/app, or project/app for production"`
	Tag string `json:"tag,omitempty" jsonschema:"image tag already pushed to the app's registry path (default \"latest\")"`
}
