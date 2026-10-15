package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// An app's series is served at the app's own address, which is what
// decides who may read it. The series itself is the metrics module's —
// what this pins is the route, the role and the shape.
func TestAnAppsMetricsAreAMembersToRead(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	rec := f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": "api", "project": f.Project.Slug, "environment": f.Environment.Slug,
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create app: %d %s", rec.Code, rec.Body.String())
	}

	_, memberKey := f.AddMember(t, "member", user.RoleMember)
	rec = f.Do(t, http.MethodGet, "/apps/web/production/api/metrics", nil, memberKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read metrics as a member: %d %s", rec.Code, rec.Body.String())
	}

	var series struct {
		Window     string           `json:"window"`
		Samples    []map[string]any `json:"samples"`
		Collecting bool             `json:"collecting"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if series.Window != "1h" {
		t.Errorf("no window asked for resolved to %q, want the default", series.Window)
	}
	if series.Samples == nil {
		t.Error("samples came back null, which is a different bug in every client")
	}
	// An app that has never deployed has no container, so there is
	// nothing to sample — which is a different sentence from "nothing
	// has been sampled yet", and the only one worth showing.
	if series.Collecting {
		t.Error("an app with no container reported that it is being collected from")
	}

	// The window is refused rather than rounded: a chart labelled 6h
	// showing one hour is worse than an error.
	rec = f.Do(t, http.MethodGet, "/apps/web/production/api/metrics?window=7d", nil, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown window: %d %s, want 400", rec.Code, rec.Body.String())
	}

	// And an app nobody can see is a 404, not an empty chart.
	rec = f.Do(t, http.MethodGet, "/apps/web/production/nope/metrics", nil, f.AdminKey)
	if rec.Code != http.StatusNotFound {
		t.Errorf("metrics for an unknown app: %d %s, want 404", rec.Code, rec.Body.String())
	}
}
