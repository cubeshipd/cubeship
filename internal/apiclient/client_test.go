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
		if body["name"] != "myapp" || body["domain"] != "myapp.example.com" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"image": "registry.example.com/myapp"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	image, err := c.CreateApp(context.Background(), "myapp", "myapp.example.com")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if image != "registry.example.com/myapp" {
		t.Fatalf("expected image registry.example.com/myapp, got %q", image)
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
