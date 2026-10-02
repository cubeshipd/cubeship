package app

import (
	"strconv"

	"cubeship/internal/envvar"
)

// deploymentEnv overlays Cubeship's reserved identity variables after user
// variables have been resolved. This makes the CUBESHIP_ namespace
// deterministic: an app cannot spoof the deployment it is running.
func deploymentEnv(a *Scoped, d *Deployment, env envvar.Map, ordinal int, nodeID int64) envvar.Map {
	if env == nil {
		env = envvar.Map{}
	} else {
		copy := envvar.Map{}
		for key, value := range env {
			copy[key] = value
		}
		env = copy
	}
	for key := range env {
		if len(key) >= len("CUBESHIP_") && key[:len("CUBESHIP_")] == "CUBESHIP_" {
			delete(env, key)
		}
	}
	env["CUBESHIP_DEPLOYMENT_ID"] = strconv.FormatInt(d.ID, 10)
	env["CUBESHIP_APP"] = a.Name
	env["CUBESHIP_APP_NAME"] = a.Name
	env["CUBESHIP_PROJECT"] = a.ProjectSlug
	env["CUBESHIP_PROJECT_NAME"] = a.ProjectSlug
	env["CUBESHIP_ENVIRONMENT"] = a.EnvironmentSlug
	env["CUBESHIP_ENVIRONMENT_NAME"] = a.EnvironmentSlug
	env["CUBESHIP_REPLICA"] = strconv.Itoa(ordinal)
	env["CUBESHIP_REPLICA_INDEX"] = strconv.Itoa(ordinal)
	env["CUBESHIP_NODE"] = strconv.FormatInt(nodeID, 10)
	env["CUBESHIP_NODE_ID"] = strconv.FormatInt(nodeID, 10)
	if d.GitSHA != "" {
		env["CUBESHIP_GIT_SHA"] = d.GitSHA
	}
	if d.GitBranch != "" {
		env["CUBESHIP_GIT_BRANCH"] = d.GitBranch
	}
	return env
}
