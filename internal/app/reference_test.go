package app_test

import (
	"net/http"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/org"
	"cubeship/internal/server/servertest"
)

// The whole point of scoping names to environments: the same app can
// exist in production and staging at once. It could not before.
func TestSameNameCanExistInTwoEnvironments(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleAdmin)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/projects/web/environments",
		map[string]string{"slug": "staging", "name": "Staging"}, key), http.StatusCreated)

	type appResponse struct {
		Reference string `json:"reference"`
		Image     string `json:"image"`
	}
	create := func(env string) appResponse {
		t.Helper()
		var got appResponse
		servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
			"name": "api", "domain": env + "-api.example.com",
			"org": "acme", "project": "web", "environment": env,
		}, key, &got), http.StatusCreated)
		return got
	}

	prod := create("production")
	staging := create("staging")

	if prod.Reference != "acme/web/production/api" || staging.Reference != "acme/web/staging/api" {
		t.Fatalf("references collided: %q and %q", prod.Reference, staging.Reference)
	}
	// Different registry paths, or one push would overwrite the other.
	if prod.Image == staging.Image {
		t.Fatalf("both environments push to %q", prod.Image)
	}
}

// Two organizations must likewise be able to have an app of the same
// name without one blocking the other.
func TestSameNameCanExistInTwoOrganizations(t *testing.T) {
	f := servertest.New(t)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs",
		map[string]string{"slug": "globex", "name": "Globex"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/globex/projects",
		map[string]string{"slug": "web", "name": "Web"}, f.AdminKey), http.StatusCreated)

	for _, o := range []string{"acme", "globex"} {
		servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
			"name": "api", "domain": o + "-api.example.com", "org": o, "project": "web",
		}, f.AdminKey), http.StatusCreated)
	}
}

// The same name in the same environment is still a conflict — that is
// the constraint that remains.
func TestSameNameTwiceInOneEnvironmentConflicts(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleMember)

	body := map[string]string{
		"name": "api", "domain": "api.example.com", "org": "acme", "project": "web",
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", body, key), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", body, key), http.StatusConflict)
}

func TestParseReference(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		invalid bool
	}{
		{in: "acme/web/production/api", want: "acme/web/production/api"},
		{in: "acme/web/staging/api", want: "acme/web/staging/api"},
		// Three parts mean the environment every project is guaranteed
		// to have.
		{in: "acme/web/api", want: "acme/web/production/api"},
		{in: "/acme/web/api/", want: "acme/web/production/api"},

		{in: "api", invalid: true},
		{in: "acme/api", invalid: true},
		{in: "acme/web/production/staging/api", invalid: true},
		{in: "", invalid: true},
		// Each part is a slug, so a traversal or an uppercase name never
		// reaches a registry path or a router name.
		{in: "acme/web/../api", invalid: true},
		{in: "acme/web/production/API", invalid: true},
		{in: "acme/web/production/my app", invalid: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			ref, err := app.ParseReference(tt.in)
			if tt.invalid {
				if err == nil {
					t.Fatalf("%q was accepted as %s", tt.in, ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseReference(%q): %v", tt.in, err)
			}
			if ref.String() != tt.want {
				t.Errorf("got %s, want %s", ref, tt.want)
			}
		})
	}
}

// The reference is the registry repository path, so what you push to and
// what you call the app are one thing.
func TestReferenceIsTheRegistryPath(t *testing.T) {
	ref, err := app.ParseReference("acme/web/staging/api")
	if err != nil {
		t.Fatal(err)
	}
	if got := ref.ImageFor("registry.example.com"); got != "registry.example.com/acme/web/staging/api" {
		t.Errorf("image is %q", got)
	}
}
