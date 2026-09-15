package audit

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"cubeship/internal/platform/httpx"
)

// phrase is one action in words. verb is what the caller did or tried
// to; past is the same after it happened; object names what it was done
// to, from the path's values or a tool's arguments — {a|b} takes the
// first one present — and noun stands in when none of them are.
type phrase struct {
	verb, past, object, noun string
}

// routes are every change the API makes, and the reads a key can be
// refused as secrets. A test holds this to the router, so a route added
// later without a sentence fails rather than showing its pattern.
var routes = map[string]phrase{
	"POST /auth/logout":                    {"sign out", "signed out", "", ""},
	"PUT /users/me/password":               {"change their password", "changed their password", "", ""},
	"PATCH /users/me":                      {"change their profile", "changed their profile", "", ""},
	"PUT /users/me/avatar":                 {"change their profile image", "changed their profile image", "", ""},
	"DELETE /users/me/avatar":              {"remove their profile image", "removed their profile image", "", ""},
	"POST /users/me/api-keys":              {"create an API key", "created an API key", "", ""},
	"POST /users/me/api-key/rotate":        {"rotate their API key", "rotated their API key", "", ""},
	"DELETE /users/me/api-keys/{id}":       {"revoke API key", "revoked API key", "{id}", ""},
	"POST /roles":                          {"create an access role", "created an access role", "", ""},
	"PUT /roles/{id}":                      {"change an access role", "changed an access role", "", ""},
	"DELETE /roles/{id}":                   {"delete an access role", "deleted an access role", "", ""},
	"POST /users":                          {"add an account", "added an account", "", ""},
	"PATCH /users/{username}":              {"change the account", "changed the account", "{username}", ""},
	"POST /users/{username}/password":      {"reset the password of", "reset the password of", "{username}", ""},
	"DELETE /users/{username}":             {"delete the account", "deleted the account", "{username}", ""},
	"DELETE /users/{username}/credentials": {"revoke every key and session of", "revoked every key and session of", "{username}", ""},
	"POST /releases/seen":                  {"mark the release notes read", "marked the release notes read", "", ""},
	"POST /updates":                        {"update the instance", "updated the instance", "", ""},
	"PUT /settings":                        {"change the instance's settings", "changed the instance's settings", "", ""},

	"POST /projects":                                           {"create a project", "created a project", "", ""},
	"DELETE /projects/{projectSlug}":                           {"delete project", "deleted project", "{projectSlug}", ""},
	"PUT /projects/{projectSlug}/image":                        {"change the picture of", "changed the picture of", "{projectSlug}", ""},
	"DELETE /projects/{projectSlug}/image":                     {"remove the picture of", "removed the picture of", "{projectSlug}", ""},
	"GET /projects/{projectSlug}/env":                          {"read the variables of", "read the variables of", "{projectSlug}", ""},
	"PUT /projects/{projectSlug}/env":                          {"replace the variables of", "replaced the variables of", "{projectSlug}", ""},
	"PATCH /projects/{projectSlug}/env":                        {"change the variables of", "changed the variables of", "{projectSlug}", ""},
	"POST /projects/{projectSlug}/environments":                {"create an environment in", "created an environment in", "{projectSlug}", ""},
	"DELETE /projects/{projectSlug}/environments/{envSlug}":    {"delete environment", "deleted environment", "{projectSlug}/{envSlug}", ""},
	"GET /projects/{projectSlug}/environments/{envSlug}/env":   {"read the variables of", "read the variables of", "{projectSlug}/{envSlug}", ""},
	"PUT /projects/{projectSlug}/environments/{envSlug}/env":   {"replace the variables of", "replaced the variables of", "{projectSlug}/{envSlug}", ""},
	"PATCH /projects/{projectSlug}/environments/{envSlug}/env": {"change the variables of", "changed the variables of", "{projectSlug}/{envSlug}", ""},

	"POST /apps":                                             {"create an app", "created an app", "", ""},
	"PATCH /apps/{project}/{env}/{name}":                     {"change", "changed", "{project}/{env}/{name}", "an app"},
	"DELETE /apps/{project}/{env}/{name}":                    {"delete", "deleted", "{project}/{env}/{name}", "an app"},
	"POST /apps/{project}/{env}/{name}/deploy":               {"deploy", "deployed", "{project}/{env}/{name}", "an app"},
	"DELETE /apps/{project}/{env}/{name}/deployments/{id}":   {"delete a deployment of", "deleted a deployment of", "{project}/{env}/{name}", "an app"},
	"POST /apps/{project}/{env}/{name}/domains":              {"add a domain to", "added a domain to", "{project}/{env}/{name}", "an app"},
	"PATCH /apps/{project}/{env}/{name}/domains/{domainID}":  {"change a domain of", "changed a domain of", "{project}/{env}/{name}", "an app"},
	"DELETE /apps/{project}/{env}/{name}/domains/{domainID}": {"remove a domain from", "removed a domain from", "{project}/{env}/{name}", "an app"},
	"GET /apps/{project}/{env}/{name}/env":                   {"read the variables of", "read the variables of", "{project}/{env}/{name}", "an app"},
	"PUT /apps/{project}/{env}/{name}/env":                   {"replace the variables of", "replaced the variables of", "{project}/{env}/{name}", "an app"},
	"PATCH /apps/{project}/{env}/{name}/env":                 {"change the variables of", "changed the variables of", "{project}/{env}/{name}", "an app"},
	"POST /apps/{project}/{env}/{name}/volumes":              {"add a volume to", "added a volume to", "{project}/{env}/{name}", "an app"},
	"DELETE /apps/{project}/{env}/{name}/volumes/{volumeID}": {"remove a volume from", "removed a volume from", "{project}/{env}/{name}", "an app"},
	"POST /apps/{project}/{env}/{name}/tcp-ports":            {"publish a TCP port of", "published a TCP port of", "{project}/{env}/{name}", "an app"},
	"DELETE /apps/{project}/{env}/{name}/tcp-ports/{portID}": {"stop publishing a TCP port of", "stopped publishing a TCP port of", "{project}/{env}/{name}", "an app"},
	"DELETE /volumes/orphans/{id}":                           {"delete the data of a removed volume", "deleted the data of a removed volume", "", ""},

	"POST /datastores":                                            {"create a database", "created a database", "", ""},
	"PATCH /datastores/{name}":                                    {"change database", "changed database", "{name}", ""},
	"DELETE /datastores/{name}":                                   {"delete database", "deleted database", "{name}", ""},
	"POST /datastores/{name}/stop":                                {"stop database", "stopped database", "{name}", ""},
	"POST /datastores/{name}/start":                               {"start database", "started database", "{name}", ""},
	"POST /datastores/{name}/expose":                              {"expose database", "exposed database", "{name}", ""},
	"DELETE /datastores/{name}/expose":                            {"stop exposing database", "stopped exposing database", "{name}", ""},
	"GET /datastores/{name}/credentials":                          {"read the credentials of database", "read the credentials of database", "{name}", ""},
	"POST /datastores/{name}/attachments":                         {"attach database", "attached database", "{name} to an app", ""},
	"DELETE /datastores/{name}/attachments/{project}/{env}/{app}": {"detach database", "detached database", "{name} from {project}/{env}/{app}", ""},

	"POST /objectstores":                                            {"add an object store", "added an object store", "", ""},
	"PATCH /objectstores/{name}":                                    {"change object store", "changed object store", "{name}", ""},
	"DELETE /objectstores/{name}":                                   {"delete object store", "deleted object store", "{name}", ""},
	"POST /objectstores/{name}/start":                               {"start object store", "started object store", "{name}", ""},
	"POST /objectstores/{name}/stop":                                {"stop object store", "stopped object store", "{name}", ""},
	"POST /objectstores/{name}/expose":                              {"expose object store", "exposed object store", "{name}", ""},
	"DELETE /objectstores/{name}/expose":                            {"stop exposing object store", "stopped exposing object store", "{name}", ""},
	"GET /objectstores/{name}/credentials":                          {"read the credentials of object store", "read the credentials of object store", "{name}", ""},
	"POST /objectstores/{name}/attachments":                         {"attach object store", "attached object store", "{name} to an app", ""},
	"DELETE /objectstores/{name}/attachments/{project}/{env}/{app}": {"detach object store", "detached object store", "{name} from {project}/{env}/{app}", ""},
	"POST /objectstores/{name}/buckets":                             {"create a bucket in", "created a bucket in", "{name}", ""},
	"DELETE /objectstores/{name}/buckets/{bucket}":                  {"delete bucket", "deleted bucket", "{name}/{bucket}", ""},
	"PUT /objectstores/{name}/buckets/{bucket}/objects":             {"upload a file to", "uploaded a file to", "{name}/{bucket}", ""},
	"DELETE /objectstores/{name}/buckets/{bucket}/objects":          {"delete files from", "deleted files from", "{name}/{bucket}", ""},
	"POST /objectstores/{name}/buckets/{bucket}/folders":            {"create a folder in", "created a folder in", "{name}/{bucket}", ""},
	"DELETE /objectstores/{name}/buckets/{bucket}/folders":          {"delete a folder from", "deleted a folder from", "{name}/{bucket}", ""},
	"GET /objectstores/{name}/buckets/{bucket}/download":            {"download a file from", "downloaded a file from", "{name}/{bucket}", ""},

	"POST /instance/backups":                                                  {"back up the instance", "backed up the instance", "", ""},
	"PUT /instance/backups/schedule":                                          {"schedule backups of the instance", "scheduled backups of the instance", "", ""},
	"DELETE /instance/backups/schedule":                                       {"stop the scheduled backups of the instance", "stopped the scheduled backups of the instance", "", ""},
	"POST /datastores/{name}/backups":                                         {"back up database", "backed up database", "{name}", ""},
	"PUT /datastores/{name}/backups/schedule":                                 {"schedule backups of database", "scheduled backups of database", "{name}", ""},
	"DELETE /datastores/{name}/backups/schedule":                              {"stop the scheduled backups of database", "stopped the scheduled backups of database", "{name}", ""},
	"POST /apps/{project}/{env}/{name}/volumes/{volumeID}/backups":            {"back up a volume of", "backed up a volume of", "{project}/{env}/{name}", "an app"},
	"PUT /apps/{project}/{env}/{name}/volumes/{volumeID}/backups/schedule":    {"schedule backups of a volume of", "scheduled backups of a volume of", "{project}/{env}/{name}", "an app"},
	"DELETE /apps/{project}/{env}/{name}/volumes/{volumeID}/backups/schedule": {"stop the scheduled backups of a volume of", "stopped the scheduled backups of a volume of", "{project}/{env}/{name}", "an app"},
	"POST /backups/{id}/restore":                                              {"restore backup", "restored backup", "{id}", ""},
	"DELETE /backups/{id}":                                                    {"delete backup", "deleted backup", "{id}", ""},
	"GET /backups/{id}/download":                                              {"download backup", "downloaded backup", "{id}", ""},

	"POST /templates/{owner}/{repo}/installs": {"install", "installed", "{owner}/{repo}", "a template"},
	"POST /template-installs/{id}/update":     {"update a template install", "updated a template install", "", ""},
	"POST /template-installs/{id}/uninstall":  {"uninstall a template", "uninstalled a template", "", ""},

	"POST /credentials":                    {"add a credential", "added a credential", "", ""},
	"PATCH /credentials/{id}":              {"change a credential", "changed a credential", "", ""},
	"DELETE /credentials/{id}":             {"delete a credential", "deleted a credential", "", ""},
	"POST /registries":                     {"add a registry", "added a registry", "", ""},
	"PUT /registries/{id}":                 {"change a registry", "changed a registry", "", ""},
	"DELETE /registries/{id}":              {"remove a registry", "removed a registry", "", ""},
	"DELETE /registries/{id}/images":       {"delete an image from a registry", "deleted an image from a registry", "", ""},
	"DELETE /registries/{id}/repositories": {"delete a repository from a registry", "deleted a repository from a registry", "", ""},
	"DELETE /registry/images":              {"delete an image from this instance's registry", "deleted an image from this instance's registry", "", ""},
	"DELETE /registry/repositories":        {"delete a repository from this instance's registry", "deleted a repository from this instance's registry", "", ""},
	"POST /registry/garbage-collect":       {"clean up this instance's registry", "cleaned up this instance's registry", "", ""},
	"POST /dns":                            {"connect a DNS provider", "connected a DNS provider", "", ""},
	"PATCH /dns/{id}":                      {"change a DNS provider", "changed a DNS provider", "", ""},
	"DELETE /dns/{id}":                     {"disconnect a DNS provider", "disconnected a DNS provider", "", ""},
	"PUT /dns/{id}/records":                {"write a DNS record", "wrote a DNS record", "", ""},
	"DELETE /dns/{id}/records":             {"delete a DNS record", "deleted a DNS record", "", ""},
	"POST /settings/github/manifest/state": {"start registering the GitHub App", "started registering the GitHub App", "", ""},
	"POST /settings/github/manifest":       {"register the GitHub App", "registered the GitHub App", "", ""},
	"POST /github":                         {"connect a GitHub installation", "connected a GitHub installation", "", ""},
	"DELETE /github/{id}":                  {"forget a GitHub installation", "forgot a GitHub installation", "", ""},
	"POST /firewall/enable":                {"turn the firewall on", "turned the firewall on", "", ""},
	"POST /firewall/disable":               {"turn the firewall off", "turned the firewall off", "", ""},
	"POST /firewall/rules":                 {"add a firewall rule", "added a firewall rule", "", ""},
	"PUT /firewall/rules/{index}":          {"change a firewall rule", "changed a firewall rule", "", ""},
	"DELETE /firewall/rules/{index}":       {"delete a firewall rule", "deleted a firewall rule", "", ""},
	"POST /firewall/docker":                {"put container ports under the firewall", "put container ports under the firewall", "", ""},
	"DELETE /firewall/docker":              {"take container ports out of the firewall", "took container ports out of the firewall", "", ""},
	"POST /nodes":                          {"add a server", "added a server", "", ""},
	"DELETE /nodes/{name}":                 {"remove server", "removed server", "{name}", ""},
}

// tools are every tool that changes something or reads a secret, by
// name, with its arguments as the values.
var tools = map[string]phrase{
	"create_api_key":    {"create API key", "created API key", "{name}", ""},
	"revoke_api_key":    {"revoke API key", "revoked API key", "{id}", ""},
	"rotate_my_api_key": {"rotate their API key", "rotated their API key", "", ""},

	"create_project":      {"create project", "created project", "{slug}", ""},
	"delete_project":      {"delete project", "deleted project", "{project|slug}", ""},
	"get_project_env":     {"read the variables of", "read the variables of", "{project|slug}", "a project"},
	"set_project_env":     {"change the variables of", "changed the variables of", "{project}", "a project"},
	"create_environment":  {"create environment", "created environment", "{project}/{slug}", ""},
	"get_environment_env": {"read the variables of", "read the variables of", "{project}/{environment}", "an environment"},
	"set_environment_env": {"change the variables of", "changed the variables of", "{project}/{environment}", "an environment"},
	"delete_environment":  {"delete environment", "deleted environment", "{project}/{environment}", ""},

	"create_app":     {"create app", "created app", "{name} in {project}", ""},
	"deploy_app":     {"deploy", "deployed", "{app}", "an app"},
	"get_app_env":    {"read the variables of", "read the variables of", "{app}", "an app"},
	"set_app_env":    {"change the variables of", "changed the variables of", "{app}", "an app"},
	"update_app":     {"change", "changed", "{reference}", "an app"},
	"delete_app":     {"delete", "deleted", "{app}", "an app"},
	"add_app_volume": {"add a volume to", "added a volume to", "{app}", "an app"},
	"back_up_volume": {"back up a volume of", "backed up a volume of", "{reference}", "an app"},

	"create_datastore":         {"create database", "created database", "{name}", ""},
	"add_datastore_extensions": {"install extensions on database", "installed extensions on database", "{datastore}", ""},
	"attach_datastore":         {"attach database", "attached database", "{datastore} to {app}", ""},
	"detach_datastore":         {"detach database", "detached database", "{datastore} from {app}", ""},
	"delete_datastore":         {"delete database", "deleted database", "{datastore}", ""},
	"attach_object_store":      {"attach bucket", "attached bucket", "{store}/{bucket} to {app}", ""},
	"detach_object_store":      {"detach bucket", "detached bucket", "{store}/{bucket} from {app}", ""},

	"install_template":           {"install", "installed", "{owner}/{repo}", "a template"},
	"update_template_install":    {"update template install", "updated template install", "{id}", ""},
	"uninstall_template_install": {"uninstall template install", "uninstalled template install", "{id}", ""},
}

// Describes reports whether an action has a sentence, for the tests that
// hold the tables to the router and the tools.
func Describes(action string) bool {
	if tool, ok := strings.CutPrefix(action, "mcp "); ok {
		_, known := tools[tool]
		return known
	}
	_, known := routes[action]
	return known
}

// Summary is an event in a sentence: "Deployed web/production/api",
// "Tried to read the variables of web", "Could not delete database cache".
func Summary(e Event) string {
	var p phrase
	var values map[string]string
	var known bool
	if tool, ok := strings.CutPrefix(e.Action, "mcp "); ok {
		p, known = tools[tool]
		values = argValues(e.Target)
		if !known {
			p = phrase{verb: "use " + tool, past: "used " + tool}
		}
	} else {
		p, known = routes[e.Action]
		if known {
			values = pathValues(e.Action, e.Target)
		} else {
			// Only a refused read gets here without a sentence of its own.
			p = phrase{verb: "read", past: "read", object: e.Target}
		}
	}

	object := fill(p.object, values)
	if object == "" {
		object = p.noun
	}
	var s string
	switch e.Outcome {
	case OutcomeRefused:
		s = "tried to " + p.verb
	case OutcomeFailed:
		s = "could not " + p.verb
	default:
		s = p.past
	}
	if object != "" {
		s += " " + object
	}
	return capitalize(s)
}

// fill replaces every {name} with its value, and answers "" when one has
// none, so the noun stands in rather than half a name.
func fill(template string, values map[string]string) string {
	if !strings.Contains(template, "{") {
		return template
	}
	var b strings.Builder
	rest := template
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			b.WriteString(rest)
			return b.String()
		}
		end := strings.IndexByte(rest[open:], '}')
		if end < 0 {
			return ""
		}
		b.WriteString(rest[:open])
		value := ""
		for _, name := range strings.Split(rest[open+1:open+end], "|") {
			if v := values[name]; v != "" {
				value = v
				break
			}
		}
		if value == "" {
			return ""
		}
		b.WriteString(value)
		rest = rest[open+end+1:]
	}
}

// pathValues reads a pattern's wildcards out of the path it matched.
func pathValues(action, target string) map[string]string {
	_, pattern := httpx.SplitPattern(action)
	names := strings.Split(strings.Trim(pattern, "/"), "/")
	parts := strings.Split(strings.Trim(target, "/"), "/")
	values := map[string]string{}
	for i, name := range names {
		if i < len(parts) && strings.HasPrefix(name, "{") && strings.HasSuffix(name, "}") {
			values[strings.Trim(name, "{}")] = parts[i]
		}
	}
	return values
}

// argValues reads back what targetOf wrote.
func argValues(target string) map[string]string {
	values := map[string]string{}
	for _, pair := range strings.Fields(target) {
		if k, v, ok := strings.Cut(pair, "="); ok {
			values[k] = v
		}
	}
	return values
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
