package github_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"

	"cubeship/internal/github"
	"cubeship/internal/org"
	"cubeship/internal/server/servertest"
)

// A URL someone pastes and the name a webhook payload carries are rarely
// spelled the same. They have to end up comparable, or a push never
// finds the app it should deploy.
func TestParseRepositoryURL(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/acme/api.git":         "acme/api",
		"https://github.com/acme/api":             "acme/api",
		"https://github.com/acme/api/":            "acme/api",
		"http://github.com/acme/api.git":          "acme/api",
		"https://www.github.com/acme/api":         "acme/api",
		"https://GitHub.com/Acme/API":             "Acme/API",
		"https://github.com/acme/api/tree/main":   "acme/api",
		"https://x-access-token:t@github.com/a/b": "a/b",
	} {
		repo, ok := github.ParseRepositoryURL(url)
		if !ok {
			t.Errorf("%q was not recognized as a GitHub repository", url)
			continue
		}
		if repo.FullName() != want {
			t.Errorf("ParseRepositoryURL(%q) = %q, want %q", url, repo.FullName(), want)
		}
	}

	// Somewhere else is not an error — it just cannot use any of this.
	for _, url := range []string{
		"https://gitlab.com/acme/api.git",
		"https://gitea.internal/acme/api.git",
		"git://github.com/acme/api.git",
		"https://github.com/acme",
		"https://github.com/",
		"not a url",
	} {
		if _, ok := github.ParseRepositoryURL(url); ok {
			t.Errorf("%q was taken for a GitHub repository", url)
		}
	}
}

// An app tracking a branch must not deploy because someone pushed a tag.
func TestBranchOf(t *testing.T) {
	if branch, ok := github.BranchOf("refs/heads/main"); !ok || branch != "main" {
		t.Errorf("refs/heads/main gave %q, %v", branch, ok)
	}
	if branch, ok := github.BranchOf("refs/heads/feature/log-in"); !ok || branch != "feature/log-in" {
		t.Errorf("a branch with a slash gave %q, %v", branch, ok)
	}
	for _, ref := range []string{"refs/tags/v1.0.0", "refs/heads/", "main", ""} {
		if _, ok := github.BranchOf(ref); ok {
			t.Errorf("%q was taken for a branch", ref)
		}
	}
}

func sign(t *testing.T, secret string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

const webhookSecret = "a-shared-secret"

func configured(t *testing.T) *servertest.Fixture {
	t.Helper()
	f := servertest.New(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings", map[string]string{
		"github_app_id":         "12345",
		"github_app_slug":       "cubeship-test",
		"github_webhook_secret": webhookSecret,
	}, f.AdminKey), http.StatusOK)
	return f
}

func post(t *testing.T, f *servertest.Fixture, body, event, signature string) int {
	t.Helper()
	req := f.RawRequest(t, http.MethodPost, "/hooks/github", body)
	req.Header.Set("X-GitHub-Event", event)
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	return f.Serve(t, req).Code
}

// The webhook is the one route that starts work without an API key, so
// what stands between it and this instance building anything is the
// signature.
func TestAWebhookMustBeSigned(t *testing.T) {
	f := configured(t)
	body := `{"ref":"refs/heads/main","repository":{"full_name":"acme/api"},"installation":{"id":1}}`

	for _, signature := range []string{
		"",
		"sha256=" + hex.EncodeToString(make([]byte, 32)),
		"garbage",
		sign(t, "the-wrong-secret", []byte(body)),
	} {
		if code := post(t, f, body, "push", signature); code != http.StatusUnauthorized {
			t.Errorf("signature %q was answered %d, want 401", signature, code)
		}
	}

	// And the right one is accepted.
	if code := post(t, f, body, "push", sign(t, webhookSecret, []byte(body))); code != http.StatusOK {
		t.Errorf("a correctly signed delivery was answered %d", code)
	}
}

// A signature is over the exact bytes sent. One byte different is a
// different delivery.
func TestATamperedBodyIsRefused(t *testing.T) {
	f := configured(t)
	body := `{"ref":"refs/heads/main","repository":{"full_name":"acme/api"},"installation":{"id":1}}`
	signature := sign(t, webhookSecret, []byte(body))

	tampered := `{"ref":"refs/heads/main","repository":{"full_name":"evil/api"},"installation":{"id":1}}`
	if code := post(t, f, tampered, "push", signature); code != http.StatusUnauthorized {
		t.Errorf("a body that did not match its signature was answered %d, want 401", code)
	}
}

// With no secret configured a delivery cannot be trusted at all, and an
// endpoint that starts deploys on an unauthenticated POST is a way to
// make this instance build anything.
func TestWithNoSecretEveryDeliveryIsRefused(t *testing.T) {
	f := servertest.New(t)
	body := `{"ref":"refs/heads/main","repository":{"full_name":"acme/api"},"installation":{"id":1}}`

	if code := post(t, f, body, "push", sign(t, webhookSecret, []byte(body))); code != http.StatusUnauthorized {
		t.Errorf("a delivery was accepted with no secret configured: %d", code)
	}
}

// Connecting an account decides what code this instance will build and
// run, so it takes the same role building does.
func TestOnlyAnAdminConnectsAnAccount(t *testing.T) {
	f := configured(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/github",
		map[string]any{"installation_id": 99, "account": "acme"}, memberKey), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/orgs/acme/github", nil, memberKey),
		http.StatusForbidden)
}

// An organization's connections are invisible from outside it — the same
// 404 an unknown organization gets.
func TestAnOutsiderSeesNoConnections(t *testing.T) {
	f := configured(t)
	_, outsiderKey := servertest.CreateUser(t, f.DB, "outsider", false)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/orgs/acme/github", nil, outsiderKey),
		http.StatusNotFound)
}

// The install link is what someone follows to connect an account, and it
// only exists once the instance is registered as an App.
func TestTheInstallLinkAppearsWithTheApp(t *testing.T) {
	f := servertest.New(t)

	var before struct {
		InstallURL string `json:"install_url"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/orgs/acme/github", nil, f.AdminKey, &before),
		http.StatusOK)
	if before.InstallURL != "" {
		t.Errorf("an install link was offered with no App registered: %q", before.InstallURL)
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings",
		map[string]string{"github_app_slug": "cubeship-test"}, f.AdminKey), http.StatusOK)

	var after struct {
		InstallURL string `json:"install_url"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/orgs/acme/github", nil, f.AdminKey, &after),
		http.StatusOK)
	if after.InstallURL != "https://github.com/apps/cubeship-test/installations/new" {
		t.Errorf("the install link is %q", after.InstallURL)
	}
}

// A private key is written and never read back. An endpoint that handed
// one out would turn every read of the configuration into a way out for
// it.
func TestTheAppCredentialsAreNeverReturned(t *testing.T) {
	f := servertest.New(t)

	const key = "-----BEGIN RSA PRIVATE KEY-----\nnot-a-real-key\n-----END RSA PRIVATE KEY-----"
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings", map[string]string{
		"github_app_id": "12345", "github_private_key": key, "github_webhook_secret": webhookSecret,
	}, f.AdminKey), http.StatusOK)

	rec := f.Do(t, http.MethodGet, "/settings", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusOK)
	for _, secret := range []string{"not-a-real-key", webhookSecret} {
		if body := rec.Body.String(); contains(body, secret) {
			t.Errorf("the settings response contains a credential: %s", body)
		}
	}
	if !contains(rec.Body.String(), `"github_connected":true`) {
		t.Errorf("the settings response does not report the connection: %s", rec.Body.String())
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// connect records an installation, the way an organization that has
// finished the install flow holds one.
//
// It writes the row rather than going through the endpoint, because the
// endpoint's job is now to verify the installation against GitHub — a
// round trip these tests would have to fake in order to test something
// else entirely. What the endpoint does is tested on its own, below.
func connect(t *testing.T, f *servertest.Fixture, installationID int64, account string) {
	t.Helper()
	if _, err := github.NewRepository(f.DB).Upsert(t.Context(), f.Org.ID, installationID, account); err != nil {
		t.Fatalf("record installation: %v", err)
	}
}

// An installation id is a number the caller chose.
//
// The App is public — it has to be, or it could only ever be installed
// on the account that owns it, and no organization could use it. So
// anyone can install it and every id is somebody's real id. Without the
// code GitHub redirects back with, connecting one would mint tokens for
// a stranger's installation, which is read access to their private code.
func TestConnectingAnInstallationNeedsProofItIsYours(t *testing.T) {
	f := configured(t)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/github",
		map[string]any{"installation_id": 42}, f.AdminKey), http.StatusBadRequest)

	// And naming an account does not stand in for it: the account is
	// read from GitHub's answer, never from the request.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/github",
		map[string]any{"installation_id": 42, "account": "somebody-else"},
		f.AdminKey), http.StatusBadRequest)
}

func createBuildingApp(t *testing.T, f *servertest.Fixture, name, repo, ref string) string {
	t.Helper()
	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": name, "domain": name + ".example.com", "org": "acme", "project": "web",
		"source": "dockerfile", "repo": repo, "ref": ref,
	}, f.AdminKey, &created), http.StatusCreated)
	return created.Reference
}

func deployments(t *testing.T, f *servertest.Fixture, reference string) int {
	t.Helper()
	var history []struct{ ID int64 }
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/apps/"+reference+"/deployments", nil, f.AdminKey, &history), http.StatusOK)
	return len(history)
}

func push(t *testing.T, f *servertest.Fixture, installationID int64, fullName, ref string) {
	t.Helper()
	body := `{"ref":"` + ref + `","repository":{"full_name":"` + fullName +
		`"},"installation":{"id":` + strconv.FormatInt(installationID, 10) + `}}`
	if code := post(t, f, body, "push", sign(t, webhookSecret, []byte(body))); code != http.StatusOK {
		t.Fatalf("the delivery was answered %d", code)
	}
	f.Server.WaitForGitHubDeploys()
}

// A push is what makes a build happen without anybody asking for one.
func TestAPushDeploysTheAppsBuiltFromIt(t *testing.T) {
	f := configured(t)
	connect(t, f, 42, "acme")

	tracking := createBuildingApp(t, f, "tracking", "https://github.com/acme/api.git", "main")
	// No ref of its own: it tracks whatever branch it is told about.
	untracked := createBuildingApp(t, f, "untracked", "https://github.com/acme/api", "")
	elsewhere := createBuildingApp(t, f, "elsewhere", "https://github.com/acme/other.git", "main")

	push(t, f, 42, "acme/api", "refs/heads/main")

	if got := deployments(t, f, tracking); got != 1 {
		t.Errorf("the app tracking main has %d deployments, want 1", got)
	}
	if got := deployments(t, f, untracked); got != 1 {
		t.Errorf("the app with no ref has %d deployments, want 1", got)
	}
	if got := deployments(t, f, elsewhere); got != 0 {
		t.Errorf("an app on another repository has %d deployments, want 0", got)
	}
}

// An app pinned to a branch is pinned. Naming a ref is how you opt out
// of deploying on every push.
func TestAPushToAnotherBranchDeploysNothing(t *testing.T) {
	f := configured(t)
	connect(t, f, 42, "acme")
	pinned := createBuildingApp(t, f, "pinned", "https://github.com/acme/api.git", "main")

	push(t, f, 42, "acme/api", "refs/heads/develop")
	if got := deployments(t, f, pinned); got != 0 {
		t.Errorf("an app pinned to main deployed on a push to develop (%d deployments)", got)
	}

	// And a tag is not a branch.
	push(t, f, 42, "acme/api", "refs/tags/v1.0.0")
	if got := deployments(t, f, pinned); got != 0 {
		t.Errorf("a tag started %d deployment(s)", got)
	}
}

// The installation is what says whose repository this is. A delivery for
// an installation nobody has connected must deploy nothing, however well
// signed — otherwise the instance's own webhook secret would be enough
// to deploy any app whose repository URL you can guess.
func TestAPushForAnUnconnectedInstallationDeploysNothing(t *testing.T) {
	f := configured(t)
	app := createBuildingApp(t, f, "app", "https://github.com/acme/api.git", "main")

	push(t, f, 999, "acme/api", "refs/heads/main")
	if got := deployments(t, f, app); got != 0 {
		t.Errorf("a push from an unconnected installation started %d deployment(s)", got)
	}
}

// Uninstalling on GitHub has to be noticed: a grant that no longer
// exists must stop being offered to clones, which would fail with a
// token GitHub has already revoked.
func TestUninstallingForgetsTheConnection(t *testing.T) {
	f := configured(t)
	connect(t, f, 42, "acme")

	body := `{"action":"deleted","installation":{"id":42}}`
	if code := post(t, f, body, "installation", sign(t, webhookSecret, []byte(body))); code != http.StatusOK {
		t.Fatalf("the delivery was answered %d", code)
	}

	var got struct {
		Installations []github.Installation `json:"installations"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/orgs/acme/github", nil, f.AdminKey, &got),
		http.StatusOK)
	if len(got.Installations) != 0 {
		t.Errorf("the connection survived being uninstalled: %+v", got.Installations)
	}
}

// The load-bearing isolation: a push GitHub signed for one
// organization's installation must not deploy another organization's
// app, even when both name the same repository.
//
// Two tenants can legitimately build the same public repository. What
// separates them is whose installation the delivery came through.
func TestAPushOnlyDeploysTheConnectedOrganizationsApps(t *testing.T) {
	f := configured(t)
	connect(t, f, 42, "acme")

	// A second organization, with an app on the same repository and no
	// connection of its own.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs",
		map[string]string{"slug": "globex", "name": "Globex"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/globex/projects",
		map[string]string{"slug": "web", "name": "Web"}, f.AdminKey), http.StatusCreated)

	var theirs struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "api", "domain": "globex-api.example.com", "org": "globex", "project": "web",
		"source": "dockerfile", "repo": "https://github.com/acme/api.git", "ref": "main",
	}, f.AdminKey, &theirs), http.StatusCreated)

	ours := createBuildingApp(t, f, "api", "https://github.com/acme/api.git", "main")

	push(t, f, 42, "acme/api", "refs/heads/main")

	if got := deployments(t, f, ours); got != 1 {
		t.Errorf("the connected organization's app has %d deployments, want 1", got)
	}
	if got := deployments(t, f, theirs.Reference); got != 0 {
		t.Errorf("another organization's app deployed on our push (%d deployments)", got)
	}
}
