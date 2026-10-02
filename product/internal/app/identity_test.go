package app

import (
	"testing"

	"cubeship/internal/envvar"
)

func TestDeploymentEnvReservesCubeshipPrefix(t *testing.T) {
	a := &Scoped{App: App{Name: "api"}, ProjectSlug: "shop", EnvironmentSlug: "prod"}
	d := &Deployment{ID: 42, GitSHA: "abc123", GitBranch: "main"}
	env := deploymentEnv(a, d, envvar.Map{"CUBESHIP_APP": "spoof", "TOKEN": "kept"}, 2, 7)
	if env["CUBESHIP_DEPLOYMENT_ID"] != "42" || env["CUBESHIP_APP"] != "api" || env["CUBESHIP_PROJECT"] != "shop" || env["CUBESHIP_ENVIRONMENT"] != "prod" {
		t.Fatalf("identity env = %#v", env)
	}
	if env["CUBESHIP_GIT_SHA"] != "abc123" || env["CUBESHIP_GIT_BRANCH"] != "main" || env["CUBESHIP_REPLICA"] != "2" || env["CUBESHIP_NODE"] != "7" {
		t.Fatalf("provenance env = %#v", env)
	}
	if env["TOKEN"] != "kept" {
		t.Fatal("non-reserved variables should be preserved")
	}
}

func TestDeploymentEnvLeavesGitUnsetForManualDeploy(t *testing.T) {
	a := &Scoped{App: App{Name: "api"}, ProjectSlug: "shop", EnvironmentSlug: "prod"}
	env := deploymentEnv(a, &Deployment{ID: 1}, nil, 1, 2)
	if _, ok := env["CUBESHIP_GIT_SHA"]; ok {
		t.Fatal("manual deploy should not set git SHA")
	}
}
