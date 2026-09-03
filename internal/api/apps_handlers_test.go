package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewServer(s, nil, "secret-token", "registry.example.com")
}

func authedRequest(method, path string, body []byte) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestCreateAppReturnsImagePath(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com"})
	req := authedRequest(http.MethodPost, "/apps", body)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["image"] != "registry.example.com/myapp" {
		t.Fatalf("expected image registry.example.com/myapp, got %q", got["image"])
	}
}

func TestCreateAppMissingFields(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp"})
	req := authedRequest(http.MethodPost, "/apps", body)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateAppDuplicateName(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com"})

	rec1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec1, authedRequest(http.MethodPost, "/apps", body))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, authedRequest(http.MethodPost, "/apps", body))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d", rec2.Code)
	}
}

func TestListAndGetApp(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com"})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, "/apps", body))

	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, authedRequest(http.MethodGet, "/apps", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}
	var apps []map[string]any
	json.Unmarshal(listRec.Body.Bytes(), &apps)
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	getRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(getRec, authedRequest(http.MethodGet, "/apps/myapp", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRec.Code)
	}

	missRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(missRec, authedRequest(http.MethodGet, "/apps/nope", nil))
	if missRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", missRec.Code)
	}
}
