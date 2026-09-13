package templateinstall

import (
	"testing"
	"time"

	"cubeship/internal/app"
	"cubeship/template"
)

// Apps deploy one after another, so each one that builds adds a build's
// whole budget: a fixed limit undid an install whose second build was
// still inside its own.
func TestARunGetsABuildsTimeForEveryAppThatBuilds(t *testing.T) {
	s := &Service{Timeout: 30 * time.Minute}
	m := &template.Normalized{Apps: []template.NormalizedApp{
		{Key: "web", Source: template.NormalizedSource{Type: "image"}},
		{Key: "agent", Source: template.NormalizedSource{Type: "dockerfile"}},
		{Key: "site", Source: template.NormalizedSource{Type: "railpack"}},
	}}

	if got, want := s.timeoutFor(m), 30*time.Minute+2*app.BuildTimeout; got != want {
		t.Errorf("timeout = %s, want %s", got, want)
	}
	if got := s.timeoutFor(&template.Normalized{}); got != s.Timeout {
		t.Errorf("a template that builds nothing got %s, want %s", got, s.Timeout)
	}
}
