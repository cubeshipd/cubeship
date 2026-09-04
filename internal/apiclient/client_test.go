package apiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAppSendsAuthAndReturnsImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodPost || r.URL.Path != "/apps" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "myapp" || body["domain"] != "myapp.example.com" || body["org"] != "acme" || body["project"] != "web" || body["environment"] != "production" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"image": "registry.example.com/acme/myapp"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	image, err := c.CreateApp(context.Background(), "myapp", "myapp.example.com", "acme", "web", "production")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if image != "registry.example.com/acme/myapp" {
		t.Fatalf("expected image registry.example.com/acme/myapp, got %q", image)
	}
}

func TestDeploySendsTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/myapp/deploy" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["tag"] != "v2" {
			t.Errorf("expected tag v2, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.Deploy(context.Background(), "myapp", "v2"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
}

func TestDeployReturnsErrorOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": "health check timed out"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	err := c.Deploy(context.Background(), "myapp", "latest")
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestSetEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/apps/myapp/env" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.SetEnv(context.Background(), "myapp", map[string]string{"PORT": "8080"}); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
}

func TestLogsStreamsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from the app\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	rc, err := c.Logs(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "hello from the app\n" {
		t.Fatalf("unexpected log output: %q", data)
	}
}

func TestCreateOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/orgs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["slug"] != "acme" || body["name"] != "Acme Inc" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.CreateOrg(context.Background(), "acme", "Acme Inc"); err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
}

func TestListOrgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]string{
			{"slug": "acme", "name": "Acme Inc"},
			{"slug": "globex", "name": "Globex Corp"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	orgs, err := c.ListOrgs(context.Background())
	if err != nil {
		t.Fatalf("ListOrgs: %v", err)
	}
	if len(orgs) != 2 || orgs[0].Slug != "acme" {
		t.Fatalf("unexpected orgs: %+v", orgs)
	}
}

func TestCreateOrgUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/orgs/acme/users" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "employee1" || body["role"] != "member" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"api_key": "new-key-123"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	key, err := c.CreateOrgUser(context.Background(), "acme", "employee1", "member")
	if err != nil {
		t.Fatalf("CreateOrgUser: %v", err)
	}
	if key != "new-key-123" {
		t.Fatalf("expected new-key-123, got %q", key)
	}
}

func TestRotateAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/users/me/api-key/rotate" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"api_key": "rotated-key-456"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	key, err := c.RotateAPIKey(context.Background())
	if err != nil {
		t.Fatalf("RotateAPIKey: %v", err)
	}
	if key != "rotated-key-456" {
		t.Fatalf("expected rotated-key-456, got %q", key)
	}
}

func TestWhoAmI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/users/me" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"username": "lucas", "is_super_admin": false})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	username, err := c.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	if username != "lucas" {
		t.Fatalf("expected lucas, got %q", username)
	}
}

func TestCreateProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/orgs/acme/projects" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["slug"] != "web" || body["name"] != "Web" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"slug": "web", "name": "Web", "environments": []string{"production"}})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	p, err := c.CreateProject(context.Background(), "acme", "web", "Web")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.Slug != "web" || len(p.Environments) != 1 || p.Environments[0] != "production" {
		t.Fatalf("unexpected project: %+v", p)
	}
}

func TestListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/orgs/acme/projects" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]string{{"slug": "web", "name": "Web"}})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	projects, err := c.ListProjects(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].Slug != "web" {
		t.Fatalf("unexpected projects: %+v", projects)
	}
}

func TestSetProjectEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/orgs/acme/projects/web/env" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.SetProjectEnv(context.Background(), "acme", "web", map[string]string{"DATABASE_URL": "postgres://shared"}); err != nil {
		t.Fatalf("SetProjectEnv: %v", err)
	}
}

func TestCreateEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/orgs/acme/projects/web/environments" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"slug": "staging", "name": "Staging"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	e, err := c.CreateEnvironment(context.Background(), "acme", "web", "staging", "Staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if e.Slug != "staging" {
		t.Fatalf("unexpected environment: %+v", e)
	}
}

func TestListEnvironments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/orgs/acme/projects/web/environments" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]string{{"slug": "production", "name": "Production"}})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	envs, err := c.ListEnvironments(context.Background(), "acme", "web")
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 1 || envs[0].Slug != "production" {
		t.Fatalf("unexpected environments: %+v", envs)
	}
}

func TestSetEnvironmentEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/orgs/acme/projects/web/environments/staging/env" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.SetEnvironmentEnv(context.Background(), "acme", "web", "staging", map[string]string{"LOG_LEVEL": "debug"}); err != nil {
		t.Fatalf("SetEnvironmentEnv: %v", err)
	}
}

func TestDeleteEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/orgs/acme/projects/web/environments/staging" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.DeleteEnvironment(context.Background(), "acme", "web", "staging"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
}
