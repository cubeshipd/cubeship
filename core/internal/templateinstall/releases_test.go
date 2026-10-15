package templateinstall

import (
	"context"
	"errors"
	"testing"
)

// An older version is read the way installing it reads it, and one this
// instance is too old for says so rather than failing: the picker shows
// why, and the install still refuses it.
func TestAVersionTheInstanceIsTooOldForIsShownAndRefused(t *testing.T) {
	f := newFixture("0.7.0")
	f.catalog.publish()

	fits, err := f.s.Manifest(context.Background(), admin, "cubeshipd", "cubeship-umami-template", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !fits.Fits || fits.Release.Commit != "abc1234" || fits.Manifest == nil {
		t.Fatalf("v1.1.0 on 0.7.0 = %+v", fits)
	}

	old := newFixture("0.1.0")
	got, err := old.s.Manifest(context.Background(), admin, "cubeshipd", "cubeship-umami-template", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest == nil || got.Manifest.MinCubeship == nil {
		t.Fatal("the fixture template names no minCubeship")
	}
	if got.Fits || got.Problem == "" {
		t.Errorf("on 0.1.0 = fits %v, problem %q", got.Fits, got.Problem)
	}
	req := umamiRequest()
	req.Release = "v1.1.0"
	if _, _, err := old.s.Install(context.Background(), admin, req); !errors.Is(err, ErrTooNew) {
		t.Errorf("installing it: %v, want ErrTooNew", err)
	}
}
