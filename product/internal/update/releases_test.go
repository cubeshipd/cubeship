package update

import "testing"

func TestWhichReleaseIsOffered(t *testing.T) {
	all := []Available{
		{Version: "0.6.0"},
		{Version: "0.7.0-rc.1", Prerelease: true},
		{Version: "0.6.1"},
		{Version: "0.7.0-beta.1", Prerelease: true},
		{Version: "0.7.0-rc.2", Prerelease: true},
	}
	withStable := append(all, Available{Version: "0.7.0"})

	for _, tc := range []struct {
		name     string
		releases []Available
		current  string
		betas    bool
		want     string
	}{
		{"stable only skips betas", all, "0.6.0", false, "0.6.1"},
		{"betas offers the newest prerelease", all, "0.6.0", true, "0.7.0-rc.2"},
		{"a stable release beats its own betas", withStable, "0.6.0", true, "0.7.0"},
		{"on a candidate with betas off, only a newer stable", all, "0.7.0-rc.1", false, ""},
		{"on a candidate with betas off, the stable release", withStable, "0.7.0-rc.1", false, "0.7.0"},
		{"on a candidate, the next candidate", all, "0.7.0-rc.1", true, "0.7.0-rc.2"},
		{"an unstamped build is offered nothing", withStable, "", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := newest(tc.releases, tc.current, tc.betas)
			version := ""
			if got != nil {
				version = got.Version
			}
			if version != tc.want {
				t.Errorf("offered %q, want %q", version, tc.want)
			}
		})
	}
}
