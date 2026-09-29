package templateinstall

import (
	"testing"

	"cubeship/template"
)

func TestMonorepoBuildUsesCatalogedCommitAndOwnDirectory(t *testing.T) {
	ref := "main:signoz"
	m := &template.Normalized{Apps: []template.NormalizedApp{{Source: template.NormalizedSource{
		Type: "dockerfile", Repo: "https://github.com/cubeshipd/cubeship-templates", Ref: &ref,
	}}}}
	pinMonorepoSources(m, "cubeshipd", "signoz", "abc1234")
	if got := *m.Apps[0].Source.Ref; got != "abc1234:signoz" {
		t.Fatalf("build ref = %q", got)
	}
}
