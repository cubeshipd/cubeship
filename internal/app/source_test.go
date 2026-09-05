package app

import (
	"cubeship/internal/user"
	"testing"
)

// Running an image somebody already published is what a member is for.
// Turning source into an image means executing whatever that source
// contains, on this host, with the builder's privileges — a different
// kind of act, and an admin's.
//
// Every source is listed, so adding one is a decision about its role
// rather than an accident.
func TestTheRoleEachSourceNeeds(t *testing.T) {
	for source, want := range map[Source]user.Role{
		SourceRegistry:   user.RoleMember,
		SourceExternal:   user.RoleMember,
		SourceDockerfile: user.RoleAdmin,
		SourceRailpack:   user.RoleAdmin,
	} {
		if got := RoleToDeploy(source); got != want {
			t.Errorf("RoleToDeploy(%q) = %q, want %q", source, got, want)
		}
	}
}

// A Git ref is not a Docker tag: branches carry slashes and a commit-ish
// can carry anything. The built image is named after it, so it has to
// survive the trip.
func TestABuiltImageIsNamedAfterItsRef(t *testing.T) {
	a := &Scoped{ProjectSlug: "web", EnvironmentSlug: "production"}
	a.Name = "api"

	for ref, want := range map[string]string{
		"main":            "cubeship-build/web/production/api:main",
		"v1.2.0":          "cubeship-build/web/production/api:v1.2.0",
		"feature/log-in":  "cubeship-build/web/production/api:feature-log-in",
		"release~1":       "cubeship-build/web/production/api:release-1",
		"":                "cubeship-build/web/production/api:default",
		"---":             "cubeship-build/web/production/api:build",
		"refs/heads/main": "cubeship-build/web/production/api:refs-heads-main",
	} {
		if got := BuildImageName(a, ref); got != want {
			t.Errorf("BuildImageName(%q) = %q, want %q", ref, got, want)
		}
	}
}

// An app that could never deploy must not be creatable, and neither must
// one carrying a setting its source would silently ignore. Finding out
// at deploy time — minutes later, with nobody watching — is the
// alternative.
func TestWhatEachSourceMayBeGiven(t *testing.T) {
	tests := []struct {
		name   string
		source Source
		origin Origin
		want   error
	}{
		{"registry needs nothing", SourceRegistry, Origin{}, nil},
		{"registry refuses an image", SourceRegistry, Origin{Image: "nginx"}, ErrImageNotAllowed},
		{"registry refuses a repository", SourceRegistry, Origin{Repo: "https://x/y.git"}, ErrRepoNotAllowed},

		{"external needs an image", SourceExternal, Origin{}, ErrImageRequired},
		{"external refuses a tag", SourceExternal, Origin{Image: "nginx:1"}, ErrImageCarriesTag},
		{"external refuses a repository", SourceExternal, Origin{Image: "nginx", Repo: "https://x/y.git"}, ErrRepoNotAllowed},

		{"dockerfile needs a repository", SourceDockerfile, Origin{}, ErrRepoRequired},
		{"dockerfile takes https", SourceDockerfile, Origin{Repo: "https://github.com/acme/api.git"}, nil},
		{"dockerfile takes git", SourceDockerfile, Origin{Repo: "git://example.com/api.git"}, nil},
		// A private network with a self-hosted Git server is a real way
		// to run this, even though nothing authenticates what comes back.
		{"dockerfile takes http", SourceDockerfile, Origin{Repo: "http://gitea.internal/acme/api.git"}, nil},
		// ssh would need a key this instance does not have; a clone
		// failing on a host key inside the builder explains nothing.
		{"dockerfile refuses ssh", SourceDockerfile, Origin{Repo: "git@github.com:acme/api.git"}, ErrRepoNotSupported},
		// The ref belongs to the app or the deploy, never to the URL.
		{"dockerfile refuses a ref in the URL", SourceDockerfile,
			Origin{Repo: "https://github.com/acme/api.git#main"}, ErrRepoNotSupported},
		{"dockerfile refuses an image", SourceDockerfile,
			Origin{Repo: "https://github.com/acme/api.git", Image: "nginx"}, ErrImageNotAllowed},

		{"railpack needs a repository", SourceRailpack, Origin{}, ErrRepoRequired},
		{"railpack takes one", SourceRailpack,
			Origin{Repo: "https://github.com/acme/api.git", Ref: "main"}, nil},
		// Railpack works the build out itself, so a path it would ignore
		// is a setting someone meant to have an effect.
		{"railpack refuses a Dockerfile path", SourceRailpack,
			Origin{Repo: "https://github.com/acme/api.git", Dockerfile: "Dockerfile"},
			ErrDockerfileNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origin := tt.origin
			if got := checkOrigin(tt.source, &origin); got != tt.want {
				t.Errorf("checkOrigin = %v, want %v", got, tt.want)
			}
		})
	}
}
