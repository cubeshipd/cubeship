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

// Every app declares where its image comes from. Today there is one
// answer, and the discriminator exists so adding another is a new case
// rather than a new special case scattered through the deploy path.
func TestAppsCarryTheirSource(t *testing.T) {
	f := servertest.New(t)

	var created struct {
		Reference string `json:"reference"`
		Source    string `json:"source"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": "acme", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)

	if created.Source != string(app.SourceRegistry) {
		t.Errorf("source defaulted to %q, want %q", created.Source, app.SourceRegistry)
	}

	// Naming it explicitly is the same thing.
	var explicit struct {
		Source string `json:"source"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "other", "domain": "other.example.com", "org": "acme", "project": "web",
		"source": "registry",
	}, f.AdminKey, &explicit), http.StatusCreated)
	if explicit.Source != "registry" {
		t.Errorf("source is %q", explicit.Source)
	}
}

// A source the daemon cannot act on must be refused at creation.
// Accepting one would let someone create an app that can never deploy,
// and only find out later.
func TestUnsupportedSourceIsRefused(t *testing.T) {
	f := servertest.New(t)

	for _, source := range []string{"git", "build", "ftp", "REGISTRY", "EXTERNAL"} {
		rec := f.Do(t, http.MethodPost, "/apps", map[string]string{
			"name": "myapp", "domain": "myapp.example.com", "org": "acme", "project": "web",
			"source": source,
		}, f.AdminKey)
		servertest.RequireStatus(t, rec, http.StatusBadRequest)
	}
}

// The source is asked whether a deploy is possible before one is
// recorded, so a misconfiguration is a refusal the caller sees rather
// than a deployment that fails on its own later.
func TestDeployIsRefusedBeforeItStartsWhenTheSourceCannotProduceAnImage(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": "acme", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost,
		"/apps/"+created.Reference+"/deploy", nil, f.AdminKey), http.StatusConflict)

	// And nothing was recorded: a refused deploy is not a failed one.
	var history []struct{ ID int64 }
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/apps/"+created.Reference+"/deployments", nil, f.AdminKey, &history), http.StatusOK)
	if len(history) != 0 {
		t.Fatalf("a refused deploy left %d deployment(s) behind", len(history))
	}
}

// An external app is defined by the image it pulls, and an app that can
// never deploy must not be creatable. Both halves of that are refused at
// creation rather than discovered at deploy time.
func TestAnExternalAppNeedsAnImageAndNothingElseMayHaveOne(t *testing.T) {
	f := servertest.New(t)

	create := func(body map[string]string) int {
		return f.Do(t, http.MethodPost, "/apps", body, f.AdminKey).Code
	}
	base := map[string]string{"domain": "x.example.com", "org": "acme", "project": "web"}
	with := func(extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	if got := create(with(map[string]string{"name": "no-image", "source": "external"})); got != http.StatusBadRequest {
		t.Errorf("an external app with no image was answered %d, want 400", got)
	}
	// The tag is what a deploy chooses; an app pinned to one could never
	// be told to run another.
	if got := create(with(map[string]string{
		"name": "tagged", "source": "external", "image": "ghcr.io/acme/api:v1",
	})); got != http.StatusBadRequest {
		t.Errorf("an image with a tag was answered %d, want 400", got)
	}
	// A registry app derives its image; naming one would be silently
	// ignored, which is worse than a refusal.
	if got := create(with(map[string]string{
		"name": "confused", "source": "registry", "image": "ghcr.io/acme/api",
	})); got != http.StatusBadRequest {
		t.Errorf("a registry app naming an image was answered %d, want 400", got)
	}

	// And the shape that is right.
	var ok struct {
		Source string `json:"source"`
		Image  string `json:"image"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", with(map[string]string{
		"name": "external-app", "source": "external", "image": "ghcr.io/acme/api",
	}), f.AdminKey, &ok), http.StatusCreated)
	if ok.Source != "external" || ok.Image != "ghcr.io/acme/api" {
		t.Errorf("created %+v", ok)
	}
}

// An external app deploys with no domain configured — the embedded
// registry needs one before anything can be pushed, and this path needs
// nothing at all. That is what makes an instance useful immediately
// after installing.
func TestAnExternalAppCanDeployBeforeAnyDomainExists(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": "acme", "project": "web",
		"source": "external", "image": "ghcr.io/acme/api",
	}, f.AdminKey, &created), http.StatusCreated)

	// Accepted, where a registry app is refused for having nowhere to
	// pull from.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost,
		"/apps/"+created.Reference+"/deploy", nil, f.AdminKey), http.StatusAccepted)
}
