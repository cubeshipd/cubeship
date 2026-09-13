package server

import (
	"cubeship/internal/user"
)

// What an MCP tool reaches, so a caller is offered only the tools its
// access allows.
//
// The services decide every call — this is not the check. It is what
// keeps an agent from being shown, and so from being talked into, a tool
// its key would refuse. Every tool is classified, and a test fails for
// one that is not.

type toolRule struct {
	// resource is what the tool reaches; empty is the caller's own
	// account, which everybody signed in has.
	resource user.Resource
	level    user.Level
	// secrets is whether it reads what somebody set.
	secrets bool
}

func tool(r user.Resource, l user.Level) toolRule { return toolRule{resource: r, level: l} }
func secret(r user.Resource) toolRule {
	return toolRule{resource: r, level: user.LevelView, secrets: true}
}

var own = toolRule{}

var toolRules = map[string]toolRule{
	"whoami":            own,
	"create_api_key":    own,
	"list_api_keys":     own,
	"revoke_api_key":    own,
	"rotate_my_api_key": own,
	"list_roles":        own,
	"list_audit_events": tool(user.ResAudit, user.LevelView),

	"create_project":      tool(user.ResProjects, user.LevelManage),
	"list_projects":       own,
	"delete_project":      tool(user.ResProjects, user.LevelManage),
	"get_project_env":     secret(user.ResProjects),
	"set_project_env":     tool(user.ResProjects, user.LevelManage),
	"create_environment":  tool(user.ResProjects, user.LevelManage),
	"list_environments":   own,
	"get_environment_env": secret(user.ResProjects),
	"set_environment_env": tool(user.ResProjects, user.LevelManage),
	"delete_environment":  tool(user.ResProjects, user.LevelManage),

	"create_app":          tool(user.ResApps, user.LevelManage),
	"list_apps":           own,
	"get_app":             tool(user.ResApps, user.LevelView),
	"deploy_app":          tool(user.ResApps, user.LevelManage),
	"get_app_env":         secret(user.ResApps),
	"set_app_env":         tool(user.ResApps, user.LevelManage),
	"get_app_deployments": tool(user.ResApps, user.LevelView),
	"update_app":          tool(user.ResApps, user.LevelManage),
	"delete_app":          tool(user.ResApps, user.LevelManage),
	"get_app_logs":        tool(user.ResApps, user.LevelView),
	"list_app_volumes":    tool(user.ResApps, user.LevelView),
	"add_app_volume":      tool(user.ResApps, user.LevelManage),
	"list_volume_backups": tool(user.ResBackups, user.LevelView),
	"back_up_volume":      tool(user.ResBackups, user.LevelManage),

	"create_datastore":       tool(user.ResDatabases, user.LevelManage),
	"list_datastores":        own,
	"get_datastore":          tool(user.ResDatabases, user.LevelView),
	"list_datastore_engines": own,
	"attach_datastore":       tool(user.ResDatabases, user.LevelManage),
	"detach_datastore":       tool(user.ResDatabases, user.LevelManage),
	"delete_datastore":       tool(user.ResDatabases, user.LevelManage),

	"list_object_stores":  own,
	"get_object_store":    tool(user.ResStorage, user.LevelView),
	"list_buckets":        secret(user.ResStorage),
	"list_objects":        secret(user.ResStorage),
	"attach_object_store": tool(user.ResStorage, user.LevelManage),
	"detach_object_store": tool(user.ResStorage, user.LevelManage),

	"instance_metrics":    tool(user.ResServers, user.LevelView),
	"instance_containers": tool(user.ResServers, user.LevelView),
	"list_servers":        tool(user.ResServers, user.LevelView),

	"list_templates":             tool(user.ResTemplates, user.LevelView),
	"list_template_releases":     tool(user.ResTemplates, user.LevelView),
	"install_template":           tool(user.ResTemplates, user.LevelManage),
	"list_template_installs":     tool(user.ResTemplates, user.LevelView),
	"get_template_install":       tool(user.ResTemplates, user.LevelView),
	"preview_template_update":    tool(user.ResTemplates, user.LevelView),
	"update_template_install":    tool(user.ResTemplates, user.LevelManage),
	"uninstall_template_install": tool(user.ResTemplates, user.LevelManage),
}

// toolAvailable reports whether caller is offered a tool. A listing a
// grant filters is always offered: it answers with what the caller sees.
func toolAvailable(caller *user.User, name string) bool {
	if caller == nil {
		return false
	}
	if caller.Admin() {
		return true
	}
	rule, known := toolRules[name]
	if !known {
		return false
	}
	if rule.resource == "" {
		return true
	}
	if rule.secrets {
		return user.HasSecrets(caller, rule.resource)
	}
	return user.CanAny(caller, rule.resource, rule.level)
}

// toolChanges reports whether calling a tool is worth recording: it is
// neither a read nor a secret read. An unclassified tool is, so a
// forgotten entry errs toward the log.
func toolChanges(name string) bool {
	rule, known := toolRules[name]
	if !known {
		return true
	}
	if name == "create_api_key" || name == "revoke_api_key" || name == "rotate_my_api_key" {
		return true
	}
	return rule.level == user.LevelManage
}
