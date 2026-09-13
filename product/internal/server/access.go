package server

import (
	"net/http"
	"strings"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// What an API key may reach, decided at the two doors rather than inside
// each module.
//
// Role is already lowered by user.Authenticate, and projects are checked
// again where a project or an app is resolved. What only a door can
// decide is whether a request is a read, whether it reads a secret, and
// whether it is about a project at all — and a door is the one place a
// route added later cannot slip past: every route and every tool below
// is classified, and a test fails for one that is not.

// secretRoutes are reads a read-only key never gets: they hand back
// values somebody set, credentials, or whole files.
var secretRoutes = map[string]bool{
	"GET /apps/{project}/{env}/{name}/env":                   true,
	"GET /projects/{projectSlug}/env":                        true,
	"GET /projects/{projectSlug}/environments/{envSlug}/env": true,
	"GET /datastores/{name}/credentials":                     true,
	"GET /objectstores/{name}/credentials":                   true,
	"GET /backups/{id}/download":                             true,
	"GET /objectstores/{name}/buckets/{bucket}/download":     true,
}

// scopedRoutes are the routes a key held to projects keeps that name no
// project in their path: the listings, filtered by the services, creating
// an app, whose project the service resolves, and the key's own account.
var scopedRoutes = map[string]bool{
	"GET /projects":                  true,
	"GET /apps":                      true,
	"POST /apps":                     true,
	"GET /users/me":                  true,
	"PATCH /users/me":                true,
	"PUT /users/me/password":         true,
	"GET /users/me/api-keys":         true,
	"POST /users/me/api-keys":        true,
	"DELETE /users/me/api-keys/{id}": true,
	"POST /users/me/api-key/rotate":  true,
	"POST /auth/logout":              true,
	"GET /releases":                  true,
	"POST /releases/seen":            true,
}

// projectParams are the path values a project slug arrives in.
var projectParams = []string{"project", "projectSlug"}

// keyPolicy refuses what the request's API key does not allow. A session
// passes untouched: it is the person, with all of their role.
func keyPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller := user.FromContext(r.Context())
		if caller == nil || caller.Key == nil {
			next.ServeHTTP(w, r)
			return
		}
		pattern := routeOf(r)
		if status, err := allowRoute(caller, r, pattern); err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func routeOf(r *http.Request) string {
	return strings.Replace(r.Pattern, " "+httpx.APIPrefix+"/", " /", 1)
}

func allowRoute(caller *user.User, r *http.Request, pattern string) (int, error) {
	if caller.Key.Access == user.AccessRead {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return http.StatusForbidden, user.ErrKeyForbidden
		}
		if secretRoutes[pattern] {
			return http.StatusForbidden, user.ErrKeyForbidden
		}
	}
	if !caller.ProjectScoped() {
		return 0, nil
	}
	for _, param := range projectParams {
		if slug := r.PathValue(param); slug != "" {
			if !caller.SeesProject(slug) {
				return http.StatusNotFound, errProjectNotFound
			}
			return 0, nil
		}
	}
	if scopedRoutes[pattern] {
		return 0, nil
	}
	return http.StatusForbidden, user.ErrKeyForbidden
}

var errProjectNotFound = httpError("project not found")

type httpError string

func (e httpError) Error() string { return string(e) }

// toolKind is what calling a tool does.
type toolKind int

const (
	toolRead toolKind = iota
	// toolSecret reads values somebody set.
	toolSecret
	toolChange
)

type toolRule struct {
	kind toolKind
	// scoped is whether a key held to projects keeps the tool. Only
	// tools that name a project or an app, which the services check, or
	// that list what the services filter.
	scoped bool
}

var toolRules = map[string]toolRule{
	// account
	"whoami":            {toolRead, true},
	"create_api_key":    {toolChange, true},
	"list_api_keys":     {toolRead, true},
	"revoke_api_key":    {toolChange, true},
	"rotate_my_api_key": {toolChange, true},
	"list_audit_events": {toolRead, false},

	// projects
	"create_project":      {toolChange, false},
	"list_projects":       {toolRead, true},
	"delete_project":      {toolChange, true},
	"get_project_env":     {toolSecret, true},
	"set_project_env":     {toolChange, true},
	"create_environment":  {toolChange, true},
	"list_environments":   {toolRead, true},
	"get_environment_env": {toolSecret, true},
	"set_environment_env": {toolChange, true},
	"delete_environment":  {toolChange, true},

	// apps
	"create_app":          {toolChange, true},
	"list_apps":           {toolRead, true},
	"get_app":             {toolRead, true},
	"deploy_app":          {toolChange, true},
	"get_app_env":         {toolSecret, true},
	"set_app_env":         {toolChange, true},
	"get_app_deployments": {toolRead, true},
	"update_app":          {toolChange, true},
	"delete_app":          {toolChange, true},
	"get_app_logs":        {toolRead, true},
	"list_app_volumes":    {toolRead, true},
	"add_app_volume":      {toolChange, true},
	"list_volume_backups": {toolRead, true},
	"back_up_volume":      {toolChange, true},

	// what belongs to the whole instance
	"create_datastore":           {toolChange, false},
	"list_datastores":            {toolRead, false},
	"get_datastore":              {toolRead, false},
	"list_datastore_engines":     {toolRead, false},
	"attach_datastore":           {toolChange, false},
	"detach_datastore":           {toolChange, false},
	"delete_datastore":           {toolChange, false},
	"list_object_stores":         {toolRead, false},
	"get_object_store":           {toolRead, false},
	"list_buckets":               {toolRead, false},
	"list_objects":               {toolRead, false},
	"attach_object_store":        {toolChange, false},
	"detach_object_store":        {toolChange, false},
	"instance_metrics":           {toolRead, false},
	"instance_containers":        {toolRead, false},
	"list_servers":               {toolRead, false},
	"list_templates":             {toolRead, false},
	"list_template_releases":     {toolRead, false},
	"install_template":           {toolChange, false},
	"list_template_installs":     {toolRead, false},
	"get_template_install":       {toolRead, false},
	"preview_template_update":    {toolRead, false},
	"update_template_install":    {toolChange, false},
	"uninstall_template_install": {toolChange, false},
}

// toolAvailable reports whether caller has a tool at all. A tool a key
// does not allow is not refused when called — it is not listed, so an
// agent holding the key cannot be talked into calling it.
func toolAvailable(caller *user.User, name string) bool {
	rule, known := toolRules[name]
	if caller == nil || caller.Key == nil {
		return true
	}
	if !known {
		return !caller.Key.Restricted()
	}
	if caller.Key.Access == user.AccessRead && rule.kind != toolRead {
		return false
	}
	if caller.ProjectScoped() && !rule.scoped {
		return false
	}
	return true
}

// toolChanges reports whether calling a tool is worth recording. An
// unclassified tool is, so a forgotten entry errs toward the log.
func toolChanges(name string) bool {
	rule, known := toolRules[name]
	return !known || rule.kind == toolChange
}
