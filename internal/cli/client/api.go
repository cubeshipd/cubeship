package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errorsAs is errors.As, wrapped so client.go needn't import errors just
// for Status.
func errorsAs(err error, target any) bool { return errors.As(err, target) }

// --- wire types ---

type Project struct {
	Slug         string   `json:"slug"`
	Environments []string `json:"environments,omitempty"`
}

type Environment struct {
	Slug string `json:"slug"`
}

type App struct {
	// Reference identifies the app — org/project/environment/name — and
	// is also its registry repository path.
	Reference string `json:"reference"`
	Name      string `json:"name"`
	// Source is where the app's image comes from.
	Source string `json:"source"`
	// Domains are every name the app answers at, each with the port
	// behind it. An app can have several — one image can expose more
	// than one port, and each name says which it reaches.
	Domains     []Domain `json:"domains"`
	Image       string   `json:"image"`
	Status      string   `json:"status"`
	Project     string   `json:"project"`
	Environment string   `json:"environment"`
}

// Domain is one name an app is served at.
type Domain struct {
	ID   int64  `json:"id"`
	Host string `json:"host"`
	// Port is what this name reaches, or 0 for "read it from the
	// image".
	Port int `json:"port"`
}

// APIKey is one of the caller's keys. The key value itself is only ever
// returned once, at creation.
type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CurrentKey bool       `json:"current_key"`
}

// noContent is the Out type for an endpoint that answers with an empty
// body.
type noContent = struct{}

type envVars struct {
	Vars map[string]string `json:"vars"`
}

// --- identity ---

// WhoAmI returns the username the client's key belongs to. `registry
// login` uses it to learn the username to log Docker in as — the saved
// credentials file only ever stores the key itself.
func (c *Client) WhoAmI(ctx context.Context) (string, error) {
	out, err := request[struct {
		Username string `json:"username"`
	}](ctx, c, "look up your username", http.MethodGet, "/users/me", nil, http.StatusOK, DefaultTimeout)
	return out.Username, err
}

// --- organizations ---

// AddUser creates an account and returns the API key it authenticates
// with, shown exactly once.
func (c *Client) AddUser(ctx context.Context, username, role string) (string, error) {
	out, err := request[struct {
		APIKey string `json:"api_key"`
	}](ctx, c, "add user", http.MethodPost, "/users",
		map[string]string{"username": username, "role": role}, http.StatusCreated, DefaultTimeout)
	if err != nil {
		return "", err
	}
	return out.APIKey, nil
}

// User is one account on the instance.
type User struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// ListUsers returns every account. Admin only.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	out, err := request[struct {
		Users []User `json:"users"`
	}](ctx, c, "list users", http.MethodGet, "/users", nil, http.StatusOK, DefaultTimeout)
	return out.Users, err
}

// DeleteUser removes an account and every key and session it holds.
func (c *Client) DeleteUser(ctx context.Context, username string) error {
	_, err := request[struct{}](ctx, c, "delete user", http.MethodDelete,
		"/users/"+segment(username), nil, http.StatusNoContent, DefaultTimeout)
	return err
}

// Revoked is what revoking an account's credentials ended.
type Revoked struct {
	APIKeys  int64 `json:"api_keys"`
	Sessions int64 `json:"sessions"`
}

// RevokeUserCredentials ends every session and revokes every API key an
// account holds, leaving the account itself.
func (c *Client) RevokeUserCredentials(ctx context.Context, username string) (Revoked, error) {
	return request[Revoked](ctx, c, "revoke credentials", http.MethodDelete,
		"/users/"+segment(username)+"/credentials", nil, http.StatusOK, DefaultTimeout)
}

// --- projects and environments ---

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

// --- apps ---

// CreateApp registers an app and returns it, including the registry path
// to push to. environment may be empty, which means "production".
func (c *Client) CreateApp(ctx context.Context, name, projectSlug, environment, source string) (App, error) {
	return request[App](ctx, c, "create app", http.MethodPost, "/apps", map[string]string{
		"name": name, "project": projectSlug,
		"environment": environment, "source": source,
	}, http.StatusCreated, DefaultTimeout)
}

// AddAppDomain gives an app a name to answer at.
//
// port is what it reaches inside the container, or 0 to read it from the
// image — which is the normal answer, and the only one available before
// an app that builds has been built.
func (c *Client) AddAppDomain(ctx context.Context, ref, host string, port int) (App, error) {
	return request[App](ctx, c, "add app domain", http.MethodPost, appPath(ref)+"/domains",
		map[string]any{"host": host, "port": port}, http.StatusCreated, DefaultTimeout)
}

// RemoveAppDomain takes a name off an app.
func (c *Client) RemoveAppDomain(ctx context.Context, ref string, domainID int64) (App, error) {
	return request[App](ctx, c, "remove app domain", http.MethodDelete,
		fmt.Sprintf("%s/domains/%d", appPath(ref), domainID), nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	return request[[]App](ctx, c, "list apps", http.MethodGet, "/apps", nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) GetApp(ctx context.Context, ref string) (App, error) {
	return request[App](ctx, c, "get app", http.MethodGet, appPath(ref), nil, http.StatusOK, DefaultTimeout)
}

// DeleteApp removes an app and stops the container serving it. Images
// already pushed stay in the registry.
func (c *Client) DeleteApp(ctx context.Context, ref string) error {
	_, err := request[noContent](ctx, c, "delete app", http.MethodDelete,
		appPath(ref), nil, http.StatusOK, DeployTimeout)
	return err
}

func (c *Client) DeleteProject(ctx context.Context, projectSlug string) error {
	_, err := request[noContent](ctx, c, "delete project", http.MethodDelete,
		"/projects/"+segment(projectSlug), nil, http.StatusOK, DefaultTimeout)
	return err
}

// appPath turns an app reference into its URL. Each part is escaped
// separately, so the four segments stay four segments whatever they
// contain.
func appPath(ref string) string {
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = segment(p)
	}
	return "/apps/" + strings.Join(escaped, "/")
}

// Deployment is one deploy attempt.
type Deployment struct {
	ID        int64     `json:"id"`
	Status    string    `json:"status"`
	Image     string    `json:"image"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Deployment statuses.
const (
	DeploymentPending   = "pending"
	DeploymentSucceeded = "succeeded"
	DeploymentFailed    = "failed"
)

// Done reports whether the deploy has finished, either way.
func (d Deployment) Done() bool {
	return d.Status == DeploymentSucceeded || d.Status == DeploymentFailed
}

// Deploy asks the daemon to redeploy an app and returns as soon as the
// deploy is accepted. The work runs on the daemon, detached from this
// request — use WaitForDeployment to follow it, or don't, and it still
// runs.
func (c *Client) Deploy(ctx context.Context, ref, tag string) (Deployment, error) {
	return request[Deployment](ctx, c, "deploy", http.MethodPost,
		appPath(ref)+"/deploy", map[string]string{"tag": tag}, http.StatusAccepted, DefaultTimeout)
}

// WaitForDeployment blocks until a deploy finishes, or until ctx runs
// out — in which case it returns the deployment as it stands, with no
// error, because giving up on watching is not a failure.
func (c *Client) WaitForDeployment(ctx context.Context, ref string, id int64) (Deployment, error) {
	return request[Deployment](ctx, c, "check deploy", http.MethodGet,
		fmt.Sprintf("%s/deployments/%d?wait=true", appPath(ref), id), nil, http.StatusOK, DeployTimeout)
}

// Deployments returns an app's recent deploy history, newest first.
func (c *Client) Deployments(ctx context.Context, ref string) ([]Deployment, error) {
	return request[[]Deployment](ctx, c, "list deploys", http.MethodGet,
		appPath(ref)+"/deployments", nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) AppEnv(ctx context.Context, ref string) (EnvVars, error) {
	return request[EnvVars](ctx, c, "read app env", http.MethodGet,
		appPath(ref)+"/env", nil, http.StatusOK, DefaultTimeout)
}

// MergeAppEnv adds or overwrites set and removes unset, leaving every
// other variable alone.
func (c *Client) MergeAppEnv(ctx context.Context, ref string, set map[string]string, unset []string) error {
	_, err := request[noContent](ctx, c, "update app env", http.MethodPatch,
		appPath(ref)+"/env", mergeEnv{Set: set, Unset: unset}, http.StatusOK, DefaultTimeout)
	return err
}

// SetAppEnv replaces every app-level variable.
func (c *Client) SetAppEnv(ctx context.Context, ref string, vars map[string]string) error {
	_, err := request[noContent](ctx, c, "set app env", http.MethodPut,
		appPath(ref)+"/env", envVars{Vars: vars}, http.StatusOK, DefaultTimeout)
	return err
}

// Logs streams an app's container output. tail limits it to that many
// trailing lines; empty means the daemon's default. The caller closes
// the reader.
func (c *Client) Logs(ctx context.Context, ref, tail string) (io.ReadCloser, error) {
	const op = "read logs"

	path := appPath(ref) + "/logs"
	if tail != "" {
		path += "?" + url.Values{"tail": {tail}}.Encode()
	}

	// Not through request: the body is the result, so it must outlive
	// this call — which also means no deadline that would cut the stream
	// short. The caller's context still cancels it.
	ctx, cancel := context.WithTimeout(ctx, LogsTimeout)
	resp, err := c.send(ctx, http.MethodGet, path, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		defer cancel()
		return nil, apiError(op, resp)
	}
	return &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}, nil
}

// cancelOnClose releases the request context when the caller is done
// reading, so a stream abandoned early doesn't leak it.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// --- API keys ---
//
// These endpoints are absent from the OpenAPI document by design: they
// are self-service plumbing the CLI drives, not an integration surface.

func (c *Client) RotateAPIKey(ctx context.Context) (string, error) {
	out, err := request[struct {
		APIKey string `json:"api_key"`
	}](ctx, c, "rotate api key", http.MethodPost, "/users/me/api-key/rotate", nil, http.StatusOK, DefaultTimeout)
	return out.APIKey, err
}

// CreateAPIKey issues an additional key under name, independent of any
// the caller already holds.
func (c *Client) CreateAPIKey(ctx context.Context, name string) (id int64, apiKey string, err error) {
	out, err := request[struct {
		ID     int64  `json:"id"`
		APIKey string `json:"api_key"`
	}](ctx, c, "create api key", http.MethodPost, "/users/me/api-keys",
		map[string]string{"name": name}, http.StatusCreated, DefaultTimeout)
	return out.ID, out.APIKey, err
}

func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return request[[]APIKey](ctx, c, "list api keys", http.MethodGet, "/users/me/api-keys",
		nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) RevokeAPIKey(ctx context.Context, id int64) error {
	_, err := request[noContent](ctx, c, "revoke api key", http.MethodDelete,
		fmt.Sprintf("/users/me/api-keys/%d", id), nil, http.StatusOK, DefaultTimeout)
	return err
}

// --- datastores ---

// Datastore is one managed database. It belongs to the instance, not to
// a project, so its name is the whole of its address.
//
// Its password is deliberately not a field: it comes back once at
// creation, and afterwards only from DatastoreCredentials.
type Datastore struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Engine      string `json:"engine"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	Username    string `json:"username"`
	Database    string `json:"database,omitempty"`
	// Host and Port are where an app on the instance reaches it.
	Host string `json:"host"`
	Port int    `json:"port"`
	// ExposedPort is the host port it also answers on, 0 for none.
	ExposedPort  int                   `json:"exposed_port,omitempty"`
	ExternalHost string                `json:"external_host,omitempty"`
	Attachments  []DatastoreAttachment `json:"attachments"`
	// Password is set only on the response to creating one.
	Password string `json:"password,omitempty"`
}

// DatastoreAttachment is one app wired to a database, and what the
// variables it receives are called. App is a full reference — a
// datastore is not inside an environment, so a bare name would identify
// nothing.
type DatastoreAttachment struct {
	App       string   `json:"app"`
	Prefix    string   `json:"prefix,omitempty"`
	Variables []string `json:"variables"`
}

// DatastoreCredentials is the login and where to use it.
type DatastoreCredentials struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Database     string `json:"database,omitempty"`
	InternalURI  string `json:"internal_uri"`
	InternalHost string `json:"internal_host"`
	InternalPort int    `json:"internal_port"`
	ExternalURI  string `json:"external_uri,omitempty"`
	ExternalHost string `json:"external_host,omitempty"`
	ExternalPort int    `json:"external_port,omitempty"`
}

// DatastoreEngine is one engine the daemon can run, and the versions it
// offers. Read rather than hard-coded: a version is permanent once a
// datastore runs it, so the daemon is the only thing that knows which
// ones it will accept.
type DatastoreEngine struct {
	Engine         string   `json:"engine"`
	Versions       []string `json:"versions"`
	DefaultVersion string   `json:"default_version"`
	Port           int      `json:"port"`
	HasDatabase    bool     `json:"has_database"`
}

// DatastoreSpec is what a datastore is created from. An empty Password
// asks the daemon to generate one, which is what the response then
// carries.
type DatastoreSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Engine      string `json:"engine"`
	Version     string `json:"version,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	Database    string `json:"database,omitempty"`
	Expose      *int   `json:"expose,omitempty"`
}

// CreateDatastore provisions a database and returns it, including the
// password — generated by the daemon when the spec carries none.
//
// The default timeout is enough: the daemon answers as soon as the row
// exists and pulls the image afterwards, so nothing here waits on a
// download.
func (c *Client) CreateDatastore(ctx context.Context, spec DatastoreSpec) (Datastore, error) {
	return request[Datastore](ctx, c, "create datastore", http.MethodPost,
		"/datastores", spec, http.StatusCreated, DefaultTimeout)
}

func (c *Client) ListDatastores(ctx context.Context) ([]Datastore, error) {
	return request[[]Datastore](ctx, c, "list datastores", http.MethodGet,
		"/datastores", nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) ListDatastoreEngines(ctx context.Context) ([]DatastoreEngine, error) {
	return request[[]DatastoreEngine](ctx, c, "list datastore engines", http.MethodGet,
		"/datastores/engines", nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) GetDatastore(ctx context.Context, name string) (Datastore, error) {
	return request[Datastore](ctx, c, "get datastore", http.MethodGet,
		datastorePath(name), nil, http.StatusOK, DefaultTimeout)
}

func (c *Client) DatastoreCredentials(ctx context.Context, name string) (DatastoreCredentials, error) {
	return request[DatastoreCredentials](ctx, c, "read datastore credentials", http.MethodGet,
		datastorePath(name)+"/credentials", nil, http.StatusOK, DefaultTimeout)
}

// DeleteDatastore removes the database, its container and its data.
func (c *Client) DeleteDatastore(ctx context.Context, name string) error {
	_, err := request[noContent](ctx, c, "delete datastore", http.MethodDelete,
		datastorePath(name), nil, http.StatusOK, DeployTimeout)
	return err
}

// ExposeDatastore publishes it on a host port. port 0 asks the daemon
// to pick one.
func (c *Client) ExposeDatastore(ctx context.Context, name string, port int) (Datastore, error) {
	return request[Datastore](ctx, c, "expose datastore", http.MethodPost,
		datastorePath(name)+"/expose", map[string]int{"port": port}, http.StatusOK, DefaultTimeout)
}

func (c *Client) UnexposeDatastore(ctx context.Context, name string) (Datastore, error) {
	return request[Datastore](ctx, c, "unexpose datastore", http.MethodDelete,
		datastorePath(name)+"/expose", nil, http.StatusOK, DefaultTimeout)
}

// AttachDatastore wires an app to it. app is the app's full
// reference, and may name one in any project.
func (c *Client) AttachDatastore(ctx context.Context, name, app, prefix string) (Datastore, error) {
	return request[Datastore](ctx, c, "attach datastore", http.MethodPost,
		datastorePath(name)+"/attachments",
		map[string]string{"app": app, "prefix": prefix}, http.StatusCreated, DefaultTimeout)
}

func (c *Client) DetachDatastore(ctx context.Context, name, app string) (Datastore, error) {
	return request[Datastore](ctx, c, "detach datastore", http.MethodDelete,
		datastorePath(name)+"/attachments/"+appSegments(app), nil, http.StatusOK, DefaultTimeout)
}

// datastorePath turns a datastore name into its URL. One segment: a
// datastore belongs to the instance, so its name is its whole address.
func datastorePath(name string) string {
	return "/datastores/" + segment(name)
}

// appSegments renders an app reference as the three path segments the
// detach route takes, escaping each separately. A two-part reference is
// production, the same shorthand the daemon accepts.
func appSegments(ref string) string {
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) == 2 {
		parts = []string{parts[0], "production", parts[1]}
	}
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = segment(p)
	}
	return strings.Join(escaped, "/")
}
