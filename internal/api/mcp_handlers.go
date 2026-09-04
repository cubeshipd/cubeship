package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"cubeship/internal/store"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpImplementationVersion is reported to MCP clients during their
// initial handshake. It has no relation to the CLI's own version const —
// bump it when a tool's behavior changes in a way a client might care
// about.
const mcpImplementationVersion = "0.1.0"

// maxMCPLogBytes bounds how much of an app's log get_app_logs returns —
// a large log pasted whole into an LLM's context is mostly waste. Pair
// it with a "tail" input (see toolGetAppLogs) so the bytes that do fit
// are the most recent ones, not the oldest.
const maxMCPLogBytes = 100_000

// newMCPHandler returns the http.Handler for the /mcp endpoint: every
// request builds a fresh, request-scoped *mcp.Server (Stateless mode —
// see StreamableHTTPOptions below), so a tool call is authorized as
// whichever user's API key the request actually carried, with no
// server-side session state that could outlive or be reused across
// callers. It is mounted behind authMiddleware (see NewServer), so
// mcpServerForRequest always sees an authenticated user.
func (s *Server) newMCPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(s.mcpServerForRequest, &mcp.StreamableHTTPOptions{Stateless: true})
}

func (s *Server) mcpServerForRequest(r *http.Request) *mcp.Server {
	user := userFromContext(r.Context())
	if user == nil {
		// Unreachable in practice — authMiddleware already rejects an
		// unauthenticated request before this is called — but a nil
		// getServer result is exactly how the SDK is documented to
		// produce a 400 rather than a panic, so handle it explicitly.
		return nil
	}
	return s.buildMCPServer(user, apiKeyHashFromContext(r.Context()))
}

// buildMCPServer registers every tool for one already-authenticated
// user. Building a fresh *mcp.Server per request (rather than one
// shared server reused across everyone) is what lets each tool handler
// simply close over user without any risk of it leaking across callers.
func (s *Server) buildMCPServer(user *store.User, keyHash string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "cubeship", Version: mcpImplementationVersion}, nil)
	t := &mcpToolset{s: s, user: user, keyHash: keyHash}
	t.register(srv)
	return srv
}

// mcpToolset holds every MCP tool handler as a method, so each has the
// authenticated caller (and, for the key-management tools, the exact key
// they're calling with) available without re-deriving it from a request
// — there is no *http.Request inside a tool call.
type mcpToolset struct {
	s       *Server
	user    *store.User
	keyHash string
}

func (t *mcpToolset) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "whoami",
		Description: "Report the identity (username, super-admin status) of the API key this MCP session is using.",
	}, t.whoAmI)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_org",
		Description: "Create a new organization. Super-admin only.",
	}, t.createOrg)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_orgs",
		Description: "List organizations you belong to (or every organization, if you're a super-admin).",
	}, t.listOrgs)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_org_user",
		Description: "Add a user to an organization, creating them if they're new. Requires admin role in the organization.",
	}, t.createOrgUser)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_project",
		Description: `Create a project within an organization. Comes with a "production" environment, which can never be deleted. Requires admin role in the organization.`,
	}, t.createProject)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_projects",
		Description: "List the projects in an organization.",
	}, t.listProjects)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_project_env",
		Description: "Set environment variables shared by every environment (and every app) in a project. Replaces the full set of project-level variables. Requires admin role in the organization.",
	}, t.setProjectEnv)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_environment",
		Description: "Create an additional environment within a project. Requires admin role in the organization.",
	}, t.createEnvironment)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_environments",
		Description: "List the environments in a project.",
	}, t.listEnvironments)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_environment_env",
		Description: "Set environment variables shared by every app in one environment. Replaces the full set of environment-level variables. Requires admin role in the organization.",
	}, t.setEnvironmentEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        `delete_environment`,
		Description: `Delete an environment. Refused for the "production" environment, and refused if the environment still has apps in it. Requires admin role in the organization.`,
	}, t.deleteEnvironment)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_app",
		Description: `Register a new app in a project and get its registry push path. environment defaults to "production" when omitted. Requires member role in the organization.`,
	}, t.createApp)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_apps",
		Description: "List every app you can see, across every organization you belong to.",
	}, t.listApps)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app",
		Description: "Get one app by name: its domain, registry image, status, and which project/environment it lives in.",
	}, t.getApp)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "deploy_app",
		Description: `Manually redeploy an app from an image tag already pushed to its registry path (tag defaults to "latest"). Requires member role in the organization.`,
	}, t.deployApp)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_app_env",
		Description: "Set an app's own environment variables. Replaces the full set of app-level variables. These are layered on top of (and override) the app's environment's and project's variables. Requires member role in the organization.",
	}, t.setAppEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app_logs",
		Description: "Get an app's recent container log output (stdout and stderr combined).",
	}, t.getAppLogs)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_api_key",
		Description: "Issue an additional, independent API key for yourself under a given name (e.g. \"mcp\", \"laptop\") — it coexists with every key you already hold.",
	}, t.createAPIKey)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_api_keys",
		Description: "List metadata for every API key you hold (id, name, timestamps). Key values are never shown again after creation.",
	}, t.listAPIKeys)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "revoke_api_key",
		Description: "Revoke one of your own API keys by id. Refused if it's your only remaining key.",
	}, t.revokeAPIKey)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "rotate_my_api_key",
		Description: "Replace the API key this MCP session is currently using with a freshly generated one. WARNING: the key authenticating this very session stops working immediately — this session's next call will fail. Every other key you hold is unaffected.",
	}, t.rotateMyAPIKey)
}

// --- resolvers: shared lookup+authorization for tool handlers, mirroring
// the equivalent HTTP handlers' logic without an *http.Request to hang
// it off of. ---

var (
	errOrgNotFound         = errors.New("organization not found")
	errForbiddenRole       = errors.New("forbidden: you do not have the required role in this organization")
	errProjectNotFound     = errors.New("project not found")
	errEnvironmentNotFound = errors.New("environment not found")
	errAppNotFound         = errors.New("app not found")
)

func (t *mcpToolset) resolveOrg(ctx context.Context, orgSlug string, minRole store.Role) (*store.Organization, error) {
	org, err := t.s.store.GetOrganizationBySlug(ctx, orgSlug)
	if err != nil {
		return nil, errOrgNotFound
	}
	if !t.s.authorizeOrg(ctx, t.user, org.ID, minRole) {
		return nil, errForbiddenRole
	}
	return org, nil
}

func (t *mcpToolset) resolveProject(ctx context.Context, orgSlug, projectSlug string, minRole store.Role) (*store.Project, error) {
	org, err := t.resolveOrg(ctx, orgSlug, minRole)
	if err != nil {
		return nil, err
	}
	project, err := t.s.store.GetProjectBySlug(ctx, org.ID, projectSlug)
	if err != nil {
		return nil, errProjectNotFound
	}
	return project, nil
}

func (t *mcpToolset) resolveEnvironment(ctx context.Context, orgSlug, projectSlug, envSlug string, minRole store.Role) (*store.Environment, error) {
	project, err := t.resolveProject(ctx, orgSlug, projectSlug, minRole)
	if err != nil {
		return nil, err
	}
	env, err := t.s.store.GetEnvironmentBySlug(ctx, project.ID, envSlug)
	if err != nil {
		return nil, errEnvironmentNotFound
	}
	return env, nil
}

// resolveApp looks up name and requires minRole in its owning
// organization, folding "doesn't exist" and "not authorized" into the
// same error — like handleGetApp, so a tool result never reveals to an
// unauthorized caller that a given app name exists at all.
func (t *mcpToolset) resolveApp(ctx context.Context, name string, minRole store.Role) (*store.App, error) {
	app, err := t.s.store.GetAppByName(ctx, name)
	if err != nil {
		return nil, errAppNotFound
	}
	if !t.s.authorizeApp(ctx, t.user, app, minRole) {
		return nil, errAppNotFound
	}
	return app, nil
}

// --- whoami ---

type whoAmIOutput struct {
	Username     string `json:"username"`
	IsSuperAdmin bool   `json:"is_super_admin"`
}

func (t *mcpToolset) whoAmI(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, whoAmIOutput, error) {
	return nil, whoAmIOutput{Username: t.user.Username, IsSuperAdmin: t.user.IsSuperAdmin}, nil
}

// --- organizations ---

type createOrgInput struct {
	Slug string `json:"slug" jsonschema:"short identifier used in URLs and registry paths: lowercase letters, digits and dashes"`
	Name string `json:"name" jsonschema:"human-readable organization name"`
}

func (t *mcpToolset) createOrg(ctx context.Context, _ *mcp.CallToolRequest, in createOrgInput) (*mcp.CallToolResult, orgResponse, error) {
	if !t.user.IsSuperAdmin {
		return nil, orgResponse{}, errors.New("forbidden: only a super-admin can create organizations")
	}
	if !slugPattern.MatchString(in.Slug) {
		return nil, orgResponse{}, errInvalidSlug
	}
	if _, err := t.s.store.GetOrganizationBySlug(ctx, in.Slug); err == nil {
		return nil, orgResponse{}, fmt.Errorf("organization %q already exists", in.Slug)
	}
	org, err := t.s.store.CreateOrganization(ctx, in.Slug, in.Name)
	if err != nil {
		return nil, orgResponse{}, err
	}
	return nil, orgResponse{Slug: org.Slug, Name: org.Name}, nil
}

func (t *mcpToolset) listOrgs(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []orgResponse, error) {
	if t.user.IsSuperAdmin {
		orgs, err := t.s.store.ListOrganizations(ctx)
		if err != nil {
			return nil, nil, err
		}
		out := make([]orgResponse, 0, len(orgs))
		for _, o := range orgs {
			out = append(out, orgResponse{Slug: o.Slug, Name: o.Name})
		}
		return nil, out, nil
	}

	memberships, err := t.s.store.ListMembershipsForUser(ctx, t.user.ID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]orgResponse, 0, len(memberships))
	for _, m := range memberships {
		out = append(out, orgResponse{Slug: m.OrgSlug, Name: m.OrgName})
	}
	return nil, out, nil
}

type createOrgUserInput struct {
	Org      string `json:"org" jsonschema:"organization slug"`
	Username string `json:"username"`
	Role     string `json:"role,omitempty" jsonschema:"\"admin\" or \"member\" (default \"member\")"`
}

func (t *mcpToolset) createOrgUser(ctx context.Context, _ *mcp.CallToolRequest, in createOrgUserInput) (*mcp.CallToolResult, createOrgUserResponse, error) {
	if in.Role == "" {
		in.Role = string(store.RoleMember)
	}
	role := store.Role(in.Role)
	if role != store.RoleAdmin && role != store.RoleMember {
		return nil, createOrgUserResponse{}, errors.New(`role must be "admin" or "member"`)
	}
	org, err := t.resolveOrg(ctx, in.Org, store.RoleAdmin)
	if err != nil {
		return nil, createOrgUserResponse{}, err
	}
	apiKey, err := t.s.addOrgUser(ctx, org, in.Username, role)
	if err != nil {
		return nil, createOrgUserResponse{}, err
	}
	return nil, createOrgUserResponse{Username: in.Username, Org: org.Slug, Role: in.Role, APIKey: apiKey}, nil
}

// --- projects ---

type createProjectInput struct {
	Org  string `json:"org" jsonschema:"organization slug"`
	Slug string `json:"slug" jsonschema:"short identifier used in URLs: lowercase letters, digits and dashes"`
	Name string `json:"name"`
}

func (t *mcpToolset) createProject(ctx context.Context, _ *mcp.CallToolRequest, in createProjectInput) (*mcp.CallToolResult, projectResponse, error) {
	org, err := t.resolveOrg(ctx, in.Org, store.RoleAdmin)
	if err != nil {
		return nil, projectResponse{}, err
	}
	if !slugPattern.MatchString(in.Slug) {
		return nil, projectResponse{}, errInvalidSlug
	}
	if _, err := t.s.store.GetProjectBySlug(ctx, org.ID, in.Slug); err == nil {
		return nil, projectResponse{}, fmt.Errorf("project %q already exists", in.Slug)
	}
	project, env, err := t.s.store.CreateProjectWithDefaultEnvironment(ctx, org.ID, in.Slug, in.Name)
	if err != nil {
		return nil, projectResponse{}, err
	}
	return nil, projectResponse{Slug: project.Slug, Name: project.Name, Environments: []string{env.Slug}}, nil
}

type orgScopedInput struct {
	Org string `json:"org" jsonschema:"organization slug"`
}

func (t *mcpToolset) listProjects(ctx context.Context, _ *mcp.CallToolRequest, in orgScopedInput) (*mcp.CallToolResult, []projectResponse, error) {
	org, err := t.resolveOrg(ctx, in.Org, store.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	projects, err := t.s.store.ListProjectsForOrg(ctx, org.ID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]projectResponse, 0, len(projects))
	for _, p := range projects {
		out = append(out, projectResponse{Slug: p.Slug, Name: p.Name})
	}
	return nil, out, nil
}

type setProjectEnvInput struct {
	Org     string            `json:"org" jsonschema:"organization slug"`
	Project string            `json:"project" jsonschema:"project slug"`
	Vars    map[string]string `json:"vars" jsonschema:"the full set of project-level environment variables — this REPLACES whatever was set before"`
}

func (t *mcpToolset) setProjectEnv(ctx context.Context, _ *mcp.CallToolRequest, in setProjectEnvInput) (*mcp.CallToolResult, actionResult, error) {
	project, err := t.resolveProject(ctx, in.Org, in.Project, store.RoleAdmin)
	if err != nil {
		return nil, actionResult{}, err
	}
	if err := t.s.store.SetProjectEnv(ctx, project.ID, in.Vars); err != nil {
		return nil, actionResult{}, err
	}
	return nil, actionResult{Message: fmt.Sprintf("updated env for project %s", project.Slug)}, nil
}

// --- environments ---

type createEnvironmentInput struct {
	Org     string `json:"org" jsonschema:"organization slug"`
	Project string `json:"project" jsonschema:"project slug"`
	Slug    string `json:"slug" jsonschema:"short identifier used in URLs and as the environment name apps request"`
	Name    string `json:"name"`
}

func (t *mcpToolset) createEnvironment(ctx context.Context, _ *mcp.CallToolRequest, in createEnvironmentInput) (*mcp.CallToolResult, environmentResponse, error) {
	project, err := t.resolveProject(ctx, in.Org, in.Project, store.RoleAdmin)
	if err != nil {
		return nil, environmentResponse{}, err
	}
	if !slugPattern.MatchString(in.Slug) {
		return nil, environmentResponse{}, errInvalidSlug
	}
	if _, err := t.s.store.GetEnvironmentBySlug(ctx, project.ID, in.Slug); err == nil {
		return nil, environmentResponse{}, fmt.Errorf("environment %q already exists", in.Slug)
	}
	env, err := t.s.store.CreateEnvironment(ctx, project.ID, in.Slug, in.Name)
	if err != nil {
		return nil, environmentResponse{}, err
	}
	return nil, environmentResponse{Slug: env.Slug, Name: env.Name}, nil
}

type projectScopedInput struct {
	Org     string `json:"org" jsonschema:"organization slug"`
	Project string `json:"project" jsonschema:"project slug"`
}

func (t *mcpToolset) listEnvironments(ctx context.Context, _ *mcp.CallToolRequest, in projectScopedInput) (*mcp.CallToolResult, []environmentResponse, error) {
	project, err := t.resolveProject(ctx, in.Org, in.Project, store.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	envs, err := t.s.store.ListEnvironmentsForProject(ctx, project.ID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]environmentResponse, 0, len(envs))
	for _, e := range envs {
		out = append(out, environmentResponse{Slug: e.Slug, Name: e.Name})
	}
	return nil, out, nil
}

type environmentScopedInput struct {
	Org         string `json:"org" jsonschema:"organization slug"`
	Project     string `json:"project" jsonschema:"project slug"`
	Environment string `json:"environment" jsonschema:"environment slug"`
}

type setEnvironmentEnvInput struct {
	Org         string            `json:"org" jsonschema:"organization slug"`
	Project     string            `json:"project" jsonschema:"project slug"`
	Environment string            `json:"environment" jsonschema:"environment slug"`
	Vars        map[string]string `json:"vars" jsonschema:"the full set of environment-level variables — this REPLACES whatever was set before"`
}

func (t *mcpToolset) setEnvironmentEnv(ctx context.Context, _ *mcp.CallToolRequest, in setEnvironmentEnvInput) (*mcp.CallToolResult, actionResult, error) {
	env, err := t.resolveEnvironment(ctx, in.Org, in.Project, in.Environment, store.RoleAdmin)
	if err != nil {
		return nil, actionResult{}, err
	}
	if err := t.s.store.SetEnvironmentEnv(ctx, env.ID, in.Vars); err != nil {
		return nil, actionResult{}, err
	}
	return nil, actionResult{Message: fmt.Sprintf("updated env for environment %s", env.Slug)}, nil
}

func (t *mcpToolset) deleteEnvironment(ctx context.Context, _ *mcp.CallToolRequest, in environmentScopedInput) (*mcp.CallToolResult, actionResult, error) {
	env, err := t.resolveEnvironment(ctx, in.Org, in.Project, in.Environment, store.RoleAdmin)
	if err != nil {
		return nil, actionResult{}, err
	}
	if env.Slug == store.ProductionEnvSlug {
		return nil, actionResult{}, errProductionEnvUndeletable
	}
	count, err := t.s.store.CountAppsInEnvironment(ctx, env.ID)
	if err != nil {
		return nil, actionResult{}, err
	}
	if count > 0 {
		return nil, actionResult{}, errEnvironmentHasApps
	}
	if err := t.s.store.DeleteEnvironment(ctx, env.ID); err != nil {
		return nil, actionResult{}, err
	}
	return nil, actionResult{Message: fmt.Sprintf("deleted environment %s", env.Slug)}, nil
}

// --- apps ---

type createAppInput struct {
	Org         string `json:"org" jsonschema:"organization slug"`
	Project     string `json:"project" jsonschema:"project slug"`
	Environment string `json:"environment,omitempty" jsonschema:"environment slug (default \"production\")"`
	Name        string `json:"name" jsonschema:"app name: lowercase letters, digits and dashes — becomes part of its registry image path"`
	Domain      string `json:"domain" jsonschema:"domain the app will be served on"`
}

func (t *mcpToolset) createApp(ctx context.Context, _ *mcp.CallToolRequest, in createAppInput) (*mcp.CallToolResult, appResponse, error) {
	if in.Environment == "" {
		in.Environment = store.ProductionEnvSlug
	}
	if !slugPattern.MatchString(in.Name) {
		return nil, appResponse{}, fmt.Errorf("name must be lowercase letters, digits and dashes, starting and ending with a letter or digit")
	}

	org, err := t.resolveOrg(ctx, in.Org, store.RoleMember)
	if err != nil {
		return nil, appResponse{}, err
	}
	project, err := t.s.store.GetProjectBySlug(ctx, org.ID, in.Project)
	if err != nil {
		return nil, appResponse{}, errProjectNotFound
	}
	env, err := t.s.store.GetEnvironmentBySlug(ctx, project.ID, in.Environment)
	if err != nil {
		return nil, appResponse{}, errEnvironmentNotFound
	}
	if _, err := t.s.store.GetAppByName(ctx, in.Name); err == nil {
		return nil, appResponse{}, fmt.Errorf("app %q already exists", in.Name)
	}

	image := t.s.registryHost + "/" + in.Org + "/" + in.Name
	app, err := t.s.store.CreateApp(ctx, org.ID, project.ID, env.ID, in.Name, in.Domain, image)
	if err != nil {
		return nil, appResponse{}, err
	}
	resp, err := t.s.toAppResponse(ctx, app)
	if err != nil {
		return nil, appResponse{}, err
	}
	return nil, resp, nil
}

func (t *mcpToolset) listApps(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []appResponse, error) {
	apps, err := t.s.store.ListApps(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]appResponse, 0, len(apps))
	for _, a := range apps {
		if !t.s.authorizeApp(ctx, t.user, a, store.RoleMember) {
			continue
		}
		resp, err := t.s.toAppResponse(ctx, a)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, resp)
	}
	return nil, out, nil
}

type appNameInput struct {
	Name string `json:"name" jsonschema:"app name"`
}

func (t *mcpToolset) getApp(ctx context.Context, _ *mcp.CallToolRequest, in appNameInput) (*mcp.CallToolResult, appResponse, error) {
	app, err := t.resolveApp(ctx, in.Name, store.RoleMember)
	if err != nil {
		return nil, appResponse{}, err
	}
	resp, err := t.s.toAppResponse(ctx, app)
	if err != nil {
		return nil, appResponse{}, err
	}
	return nil, resp, nil
}

type deployAppInput struct {
	Name string `json:"name" jsonschema:"app name"`
	Tag  string `json:"tag,omitempty" jsonschema:"image tag already pushed to the app's registry path (default \"latest\")"`
}

func (t *mcpToolset) deployApp(ctx context.Context, _ *mcp.CallToolRequest, in deployAppInput) (*mcp.CallToolResult, actionResult, error) {
	if in.Tag == "" {
		in.Tag = "latest"
	}
	app, err := t.resolveApp(ctx, in.Name, store.RoleMember)
	if err != nil {
		return nil, actionResult{}, err
	}
	pullRef := localPullRef(app.Image, in.Tag)
	if err := t.s.orch.Deploy(ctx, app.Name, pullRef); err != nil {
		return nil, actionResult{}, fmt.Errorf("deploy failed: %w", err)
	}
	return nil, actionResult{Message: fmt.Sprintf("deployed %s from tag %s", app.Name, in.Tag)}, nil
}

type setAppEnvInput struct {
	Name string            `json:"name" jsonschema:"app name"`
	Vars map[string]string `json:"vars" jsonschema:"the full set of app-level environment variables — this REPLACES whatever was set before"`
}

func (t *mcpToolset) setAppEnv(ctx context.Context, _ *mcp.CallToolRequest, in setAppEnvInput) (*mcp.CallToolResult, actionResult, error) {
	app, err := t.resolveApp(ctx, in.Name, store.RoleMember)
	if err != nil {
		return nil, actionResult{}, err
	}
	if err := t.s.store.SetAppEnv(ctx, app.ID, in.Vars); err != nil {
		return nil, actionResult{}, err
	}
	return nil, actionResult{Message: fmt.Sprintf("updated env for app %s", app.Name)}, nil
}

type getAppLogsInput struct {
	Name string `json:"name" jsonschema:"app name"`
	Tail string `json:"tail,omitempty" jsonschema:"number of trailing lines to return, e.g. \"500\", or \"all\" for the full log (default \"200\")"`
}

func (t *mcpToolset) getAppLogs(ctx context.Context, _ *mcp.CallToolRequest, in getAppLogsInput) (*mcp.CallToolResult, any, error) {
	if in.Tail == "" {
		in.Tail = "200"
	}
	app, err := t.resolveApp(ctx, in.Name, store.RoleMember)
	if err != nil {
		return nil, nil, err
	}

	rc, err := t.s.orch.Logs(ctx, app.Name, in.Tail)
	if err != nil {
		return nil, nil, fmt.Errorf("get logs: %w", err)
	}
	defer rc.Close()

	// Containers run without a TTY, so the Engine multiplexes
	// stdout/stderr behind an 8-byte binary frame header per chunk (see
	// handleGetLogs) — demultiplex it before this is readable at all.
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

// --- API keys ---

type createAPIKeyInput struct {
	Name string `json:"name" jsonschema:"a label to recognize this key by later, e.g. \"mcp\" or \"laptop\""`
}

type createAPIKeyOutput struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	APIKey string `json:"api_key"`
}

func (t *mcpToolset) createAPIKey(ctx context.Context, _ *mcp.CallToolRequest, in createAPIKeyInput) (*mcp.CallToolResult, createAPIKeyOutput, error) {
	created, generated, err := t.s.createAdditionalAPIKey(ctx, t.user, in.Name)
	if err != nil {
		return nil, createAPIKeyOutput{}, err
	}
	return nil, createAPIKeyOutput{ID: created.ID, Name: created.Name, APIKey: generated}, nil
}

func (t *mcpToolset) listAPIKeys(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []apiKeyResponse, error) {
	keys, err := t.s.store.ListAPIKeysForUser(ctx, t.user.ID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]apiKeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, apiKeyResponse{
			ID: k.ID, Name: k.Name, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt,
			CurrentKey: k.KeyHash == t.keyHash,
		})
	}
	return nil, out, nil
}

type revokeAPIKeyInput struct {
	ID int64 `json:"id" jsonschema:"the key's id, from list_api_keys"`
}

func (t *mcpToolset) revokeAPIKey(ctx context.Context, _ *mcp.CallToolRequest, in revokeAPIKeyInput) (*mcp.CallToolResult, actionResult, error) {
	if err := t.s.revokeAPIKey(ctx, t.user, in.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, actionResult{}, fmt.Errorf("api key %d not found", in.ID)
		}
		return nil, actionResult{}, err
	}
	return nil, actionResult{Message: fmt.Sprintf("revoked api key %d", in.ID)}, nil
}

type rotateMyAPIKeyOutput struct {
	APIKey string `json:"api_key"`
}

func (t *mcpToolset) rotateMyAPIKey(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, rotateMyAPIKeyOutput, error) {
	key, err := t.s.rotateAPIKey(ctx, t.user, t.keyHash)
	if err != nil {
		return nil, rotateMyAPIKeyOutput{}, err
	}
	return nil, rotateMyAPIKeyOutput{APIKey: key}, nil
}

// actionResult is the output shape for tools that perform a write with
// no natural resource to hand back — a one-line confirmation of what
// happened.
type actionResult struct {
	Message string `json:"message"`
}

// errInvalidSlug is shared by every tool that creates a slugged
// resource — see slugPattern.
var errInvalidSlug = errors.New("slug must be lowercase letters, digits and dashes, starting and ending with a letter or digit")
