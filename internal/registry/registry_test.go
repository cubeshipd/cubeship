package registry_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"cubeship/internal/org"
	"cubeship/internal/platform/regauth"
	"cubeship/internal/server/servertest"

	"github.com/golang-jwt/jwt/v5"
)

// tokenAccess asks the registry token endpoint for a scope and returns
// the access entries the daemon actually granted.
func tokenAccess(t *testing.T, f *servertest.Fixture, username, key, scope string) []regauth.AccessEntry {
	t.Helper()

	req := httptestRequest(t, "/v2/token?scope="+url.QueryEscape(scope))
	req.SetBasicAuth(username, key)
	rec := newRecorder()
	f.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("token request for %q: expected 200, got %d: %s", scope, rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}

	// The token is signed with a key the test never sets, so this reads
	// the claims without verifying them — the claims are what's under test.
	var claims struct {
		Access []regauth.AccessEntry `json:"access"`
		jwt.RegisteredClaims
	}
	parser := jwt.NewParser()
	if _, _, err := parser.ParseUnverified(body.Token, &claims); err != nil {
		t.Fatalf("parse token: %v", err)
	}
	return claims.Access
}

// A push token must never carry an action Cubeship doesn't grant. The
// Docker client sends whatever it wants in the scope string, and echoing
// it back verbatim would hand a plain member the ability to delete
// another team's images.
func TestTokenGrantsOnlyPushAndPull(t *testing.T) {
	f := newSignedFixture(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)

	access := tokenAccess(t, f, "member", memberKey, "repository:acme/myapp:pull,push,delete,*")
	if len(access) != 1 {
		t.Fatalf("expected one access entry, got %v", access)
	}
	for _, action := range access[0].Actions {
		if action != "pull" && action != "push" {
			t.Errorf("token granted %q, which Cubeship never grants: %v", action, access[0].Actions)
		}
	}
	if len(access[0].Actions) != 2 {
		t.Errorf("expected pull and push to survive, got %v", access[0].Actions)
	}
}

// Membership is what scopes registry access: a valid login for one
// organization must grant nothing in another's namespace.
func TestTokenRefusesAnotherOrganizationsNamespace(t *testing.T) {
	f := newSignedFixture(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)

	if _, err := f.Server.Orgs.Repo().Create(t.Context(), "globex", "Globex"); err != nil {
		t.Fatalf("create the other organization: %v", err)
	}

	if access := tokenAccess(t, f, "member", memberKey, "repository:globex/theirapp:pull,push"); len(access) != 0 {
		t.Fatalf("a member of acme was granted access to globex: %v", access)
	}
}

func TestTokenRejectsABadAPIKey(t *testing.T) {
	f := newSignedFixture(t)
	f.AddMember(t, "member", org.RoleMember)

	req := httptestRequest(t, "/v2/token?scope=repository:acme/myapp:pull")
	req.SetBasicAuth("member", "not-their-key")
	rec := newRecorder()
	f.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a bad key, got %d", rec.Code)
	}
}

// The webhook is the one unauthenticated-looking route on the daemon. It
// is guarded by a shared secret, and a forged notification must not be
// able to force a redeploy.
func TestWebhookRejectsAForgedToken(t *testing.T) {
	f := servertest.New(t)

	body := `{"events":[{"action":"push","target":{"repository":"acme/myapp","tag":"latest"}}]}`
	for _, header := range []string{"", "Bearer wrong-secret", "wrong-secret"} {
		req := httptestPost(t, "/hooks/registry", body)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := newRecorder()
		f.Server.Router().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: expected 401, got %d", header, rec.Code)
		}
	}
}

func TestWebhookAcceptsTheDaemonsOwnToken(t *testing.T) {
	f := servertest.New(t)

	req := httptestPost(t, "/hooks/registry",
		`{"events":[{"action":"push","target":{"repository":"acme/nosuchapp","tag":"latest"}}]}`)
	req.Header.Set("Authorization", "Bearer "+servertest.WebhookToken)
	rec := newRecorder()
	f.Server.Router().ServeHTTP(rec, req)

	// No app tracks that repository, so nothing is deployed — but the
	// notification is accepted, since a retry would not help.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	f.Server.Registry.WaitForDeploys()
}

// A push under an external app's name must not deploy it. The registry
// still accepts the push — the repository path exists either way — but
// what that app runs comes from somewhere else, and deploying it because
// something landed here would run a version nobody asked for.
func TestAPushDoesNotDeployAnExternalApp(t *testing.T) {
	f := servertest.New(t)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": "acme", "project": "web",
		"source": "external", "image": "ghcr.io/acme/api",
	}, f.AdminKey, &created), http.StatusCreated)

	req := httptestPost(t, "/hooks/registry",
		`{"events":[{"action":"push","target":{"repository":"`+created.Reference+`","tag":"latest"}}]}`)
	req.Header.Set("Authorization", "Bearer "+servertest.WebhookToken)
	rec := newRecorder()
	f.Server.Router().ServeHTTP(rec, req)
	servertest.RequireStatus(t, rec, http.StatusOK)
	f.Server.Registry.WaitForDeploys()

	var history []struct{ ID int64 }
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/apps/"+created.Reference+"/deployments", nil, f.AdminKey, &history), http.StatusOK)
	if len(history) != 0 {
		t.Fatalf("a push started %d deploy(s) for an external app", len(history))
	}
}
