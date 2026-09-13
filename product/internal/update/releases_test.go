package update

import "testing"

func TestWhichReleaseIsOffered(t *testing.T) {
	all := []Available{
		{Version: "0.6.0"},
		{Version: "0.7.0-rc.1", Prerelease: true},
		{Version: "0.6.1"},
		{Version: "0.7.0-rc.2", Prerelease: true},
	}
	withStable := append(all, Available{Version: "0.7.0"})

	for _, tc := range []struct {
		name       string
		releases   []Available
		current    string
		candidates bool
		want       string
	}{
		{"stable only skips candidates", all, "0.6.0", false, "0.6.1"},
		{"candidates offers the newest candidate", all, "0.6.0", true, "0.7.0-rc.2"},
		{"a stable release beats its own candidates", withStable, "0.6.0", true, "0.7.0"},
		{"on a candidate with candidates off, only a newer stable", all, "0.7.0-rc.1", false, ""},
		{"on a candidate with candidates off, the stable release", withStable, "0.7.0-rc.1", false, "0.7.0"},
		{"on a candidate, the next candidate", all, "0.7.0-rc.1", true, "0.7.0-rc.2"},
		{"an unstamped build is offered nothing", withStable, "", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := newest(tc.releases, tc.current, tc.candidates)
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
