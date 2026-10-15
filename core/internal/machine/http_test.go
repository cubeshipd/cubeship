package machine_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// The machine's series is a member's to read, like an app's metrics and
// a database's log. What the box is doing is the context for every "why
// is this slow" anybody deploying here will have, and none of it is a
// secret: it says how much of the disk is left, never what is on it.
func TestTheMachinesMetricsAreAMembersToRead(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	rec := f.Do(t, http.MethodGet, "/instance/metrics", nil, memberKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read the instance's metrics as a member: %d %s", rec.Code, rec.Body.String())
	}

	var series struct {
		Window      string            `json:"window"`
		Samples     []map[string]any  `json:"samples"`
		Cores       int               `json:"cores"`
		DiskPath    string            `json:"disk_path"`
		Unavailable map[string]string `json:"unavailable"`
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
	// The facts about the machine come from the kernel on every read
	// rather than from the newest row, so a daemon that started a
	// minute ago — which this fixture is — can still say what box it is
	// on. That is most of what somebody opening the screen wanted.
	if series.DiskPath != f.DataDir {
		t.Errorf("disk_path = %q, want the data directory %q", series.DiskPath, f.DataDir)
	}
	// Whatever this test is running on, a measurement that did not read
	// is reported with a reason rather than as a zero.
	for measure, reason := range series.Unavailable {
		if reason == "" {
			t.Errorf("%s is unavailable and says nothing about why", measure)
		}
	}

	// A window this release does not offer is refused rather than
	// rounded to the nearest: a chart labelled 6h showing one hour is
	// worse than an error.
	if rec = f.Do(t, http.MethodGet, "/instance/metrics?window=3d", nil, memberKey); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown window: %d %s, want 400", rec.Code, rec.Body.String())
	}
	if rec = f.Do(t, http.MethodGet, "/instance/metrics", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("with no key: %d, want 401", rec.Code)
	}
}

// What is using this box crosses every module that runs a container, so
// it is served here rather than beside any one of them — and read by
// the same role, because it is the same screen and the same question.
//
// Nothing has been sampled in a fixture, so what this pins is the
// route, the role and the shape. The join between a reading and the
// name of the thing it measured is pinned in internal/metrics, where
// there is a database to put readings in.
func TestWhatEveryContainerIsUsingIsAMembersToRead(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	rec := f.Do(t, http.MethodGet, "/instance/containers", nil, memberKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("list what is running as a member: %d %s", rec.Code, rec.Body.String())
	}
	var usage []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &usage); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if usage == nil {
		t.Error("came back null rather than an empty list, which is a different bug in every client")
	}
	if rec = f.Do(t, http.MethodGet, "/instance/containers", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("with no key: %d, want 401", rec.Code)
	}
}
