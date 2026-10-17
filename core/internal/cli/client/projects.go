package client

import (
	"context"
	"net/http"
)

type Project struct {
	Slug         string   `json:"slug"`
	Environments []string `json:"environments,omitempty"`
}

type Environment struct {
	Slug string `json:"slug"`
}

func (c *Client) CreateProject(ctx context.Context, slug string) (Project, error) {
	return request[Project](ctx, c, "create project", http.MethodPost,
		"/projects",
		map[string]string{"slug": slug}, http.StatusCreated, DefaultTimeout)
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	return request[[]Project](ctx, c, "list projects", http.MethodGet,
		"/projects", nil, http.StatusOK, DefaultTimeout)
}

// EnvVars is what reading variables at one level returns: the ones set
// there, and — below a project — the inherited result with each value's
// source.
type EnvVars struct {
	Vars      map[string]string `json:"vars"`
	Effective []ResolvedVar     `json:"effective,omitempty"`
}

// ResolvedVar is one variable in the final environment, and the level
// that set it.
type ResolvedVar struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// mergeEnv is the PATCH body: change only what is named.
type mergeEnv struct {
	Set   map[string]string `json:"set,omitempty"`
	Unset []string          `json:"unset,omitempty"`
}

func (c *Client) ProjectEnv(ctx context.Context, projectSlug string) (EnvVars, error) {
	return request[EnvVars](ctx, c, "read project env", http.MethodGet,
		"/projects/"+segment(projectSlug)+"/env", nil, http.StatusOK, DefaultTimeout)
}

// MergeProjectEnv adds or overwrites set and removes unset, leaving every
// other variable alone.
func (c *Client) MergeProjectEnv(ctx context.Context, projectSlug string, set map[string]string, unset []string) error {
	_, err := request[noContent](ctx, c, "update project env", http.MethodPatch,
		"/projects/"+segment(projectSlug)+"/env",
		mergeEnv{Set: set, Unset: unset}, http.StatusOK, DefaultTimeout)
	return err
}

// SetProjectEnv replaces every project-level variable. Callers that mean
// "change these" want MergeProjectEnv.
func (c *Client) SetProjectEnv(ctx context.Context, projectSlug string, vars map[string]string) error {
	_, err := request[noContent](ctx, c, "set project env", http.MethodPut,
		"/projects/"+segment(projectSlug)+"/env",
		envVars{Vars: vars}, http.StatusOK, DefaultTimeout)
	return err
}

func (c *Client) CreateEnvironment(ctx context.Context, projectSlug, slug string) (Environment, error) {
	return request[Environment](ctx, c, "create environment", http.MethodPost,
		"/projects/"+segment(projectSlug)+"/environments",
		map[string]string{"slug": slug}, http.StatusCreated, DefaultTimeout)
}

func (c *Client) ListEnvironments(ctx context.Context, projectSlug string) ([]Environment, error) {
	return request[[]Environment](ctx, c, "list environments", http.MethodGet,
		"/projects/"+segment(projectSlug)+"/environments",
		nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) EnvironmentEnv(ctx context.Context, projectSlug, envSlug string) (EnvVars, error) {
	return request[EnvVars](ctx, c, "read environment env", http.MethodGet,
		"/projects/"+segment(projectSlug)+"/environments/"+segment(envSlug)+"/env",
		nil, http.StatusOK, DefaultTimeout)
}

// MergeEnvironmentEnv adds or overwrites set and removes unset, leaving
// every other variable alone.
func (c *Client) MergeEnvironmentEnv(ctx context.Context, projectSlug, envSlug string, set map[string]string, unset []string) error {
	_, err := request[noContent](ctx, c, "update environment env", http.MethodPatch,
		"/projects/"+segment(projectSlug)+"/environments/"+segment(envSlug)+"/env",
		mergeEnv{Set: set, Unset: unset}, http.StatusOK, DefaultTimeout)
	return err
}

// SetEnvironmentEnv replaces every environment-level variable.
func (c *Client) SetEnvironmentEnv(ctx context.Context, projectSlug, envSlug string, vars map[string]string) error {
	_, err := request[noContent](ctx, c, "set environment env", http.MethodPut,
		"/projects/"+segment(projectSlug)+"/environments/"+segment(envSlug)+"/env",
		envVars{Vars: vars}, http.StatusOK, DefaultTimeout)
	return err
}

func (c *Client) DeleteEnvironment(ctx context.Context, projectSlug, envSlug string) error {
	_, err := request[noContent](ctx, c, "delete environment", http.MethodDelete,
		"/projects/"+segment(projectSlug)+"/environments/"+segment(envSlug),
		nil, http.StatusOK, DefaultTimeout)
	return err
}

func (c *Client) DeleteProject(ctx context.Context, projectSlug string) error {
	_, err := request[noContent](ctx, c, "delete project", http.MethodDelete,
		"/projects/"+segment(projectSlug), nil, http.StatusOK, DefaultTimeout)
	return err
}
