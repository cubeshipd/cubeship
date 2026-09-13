package catalog

import (
	"slices"
	"testing"
)

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"cubeship-umami-template":       "Umami",
		"cubeship-uptime-kuma-template": "Uptime Kuma",
		"Cubeship-N8N-Template":         "N8N",
		"plausible":                     "Plausible",
		"my_app":                        "My App",
		"cubeship-template":             "Template",
	} {
		if got := DisplayName(in); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTagsLeaveTheListingTopicOut(t *testing.T) {
	if got := tagsOf([]string{Topic, "analytics"}, ""); !slices.Equal(got, []string{"analytics"}) {
		t.Errorf("tags = %v", got)
	}
}
