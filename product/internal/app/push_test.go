package app_test

import (
	"net/http"
	"sort"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/server/servertest"
)

// A push carries a branch, and an app that named a ref asked not to be
// told about it. Selecting on the pushed branch as well would deploy an
// app pinned to `main` on every push to `main`, and — a tag is never a
// branch — a build of an app pinned to `v1.0.0` would run the branch,
// which is the pin gone after one push.
func TestAPushReachesOnlyTheAppsThatNamedNoRef(t *testing.T) {
	f := servertest.New(t)

	create := func(name, ref string) {
		t.Helper()
		body := railpackApp(name)
		if ref != "" {
			body["ref"] = ref
		}
		servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", body, f.AdminKey),
			http.StatusCreated)
	}
	create("follower", "")
	create("pinned-to-a-tag", "v1.0.0")
	create("pinned-to-the-pushed-branch", "main")

	found, err := app.NewRepository(f.DB).BuildingFromRepository(t.Context(), "acme/api")
	if err != nil {
		t.Fatalf("find the apps a push reaches: %v", err)
	}
	var names []string
	for _, a := range found {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	if len(names) != 1 || names[0] != "follower" {
		t.Fatalf("a push to acme/api reached %v, want only [follower]", names)
	}
}
