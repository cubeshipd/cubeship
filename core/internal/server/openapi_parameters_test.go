package server_test

import (
	"regexp"
	"slices"
	"testing"

	"cubeship/internal/server/servertest"
)

func TestOpenAPIPathParametersMatchRoutes(t *testing.T) {
	doc := servertest.New(t).Server.OpenAPI()
	placeholder := regexp.MustCompile(`\{([^{}]+)\}`)
	for path, methods := range doc.Paths {
		var want []string
		for _, match := range placeholder.FindAllStringSubmatch(path, -1) {
			want = append(want, match[1])
		}
		slices.Sort(want)
		for method, operation := range methods {
			var got []string
			for _, parameter := range operation.Parameters {
				if parameter.In == "path" {
					got = append(got, parameter.Name)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("%s %s: path parameters %v, want %v", method, path, got, want)
			}
		}
	}
}
