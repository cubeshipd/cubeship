package template

import (
	"encoding/json"
	"regexp"
	"slices"
	"testing"
)

// The published schema refuses volumes beside a minCubeship older than
// volumes, so an editor says it before a release is refused. Its pattern
// can only read simple ranges, so it may flag one the validator accepts —
// but it must never pass one the validator refuses.
func TestTheSchemaHoldsVolumesToTheirReleaseAsTheValidatorDoes(t *testing.T) {
	var s struct {
		If struct {
			Not struct {
				Properties struct {
					MinCubeship struct {
						Pattern string `json:"pattern"`
					} `json:"minCubeship"`
				} `json:"properties"`
			} `json:"not"`
		} `json:"if"`
	}
	if err := json.Unmarshal(JSONSchema, &s); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(s.If.Not.Properties.MinCubeship.Pattern)

	refused := func(min string) bool {
		file := "version: 1\nproject: q\nminCubeship: \"" + min + "\"\napps:\n  - key: b\n    image: rabbitmq\n    volumes:\n      - path: /data\n"
		return slices.Contains(codesOf(Validate([]byte(file)).Diagnostics), "volume.min-cubeship")
	}

	// What authors write, which the editor must not flag.
	for _, min := range []string{
		"0.7.0", ">=0.7.0", "^0.7", "~0.7.2", "0.8.1", "1.0.0", ">=0.7.0 <1.0.0",
		"^0.7 || ^1", "0.7.0-rc.6", " >= 0.9 ",
	} {
		if !pattern.MatchString(min) {
			t.Errorf("the schema flags minCubeship %q", min)
		}
		if refused(min) {
			t.Errorf("the validator refuses minCubeship %q", min)
		}
	}
	// Ranges older instances satisfy: both refuse.
	for _, min := range []string{
		"0.6.0", ">=0.6.0", "^0.6", "0.7.0-rc.5", "^0.6 || ^1", "<1.0.0", "*", "",
	} {
		if pattern.MatchString(min) {
			t.Errorf("the schema lets minCubeship %q through", min)
		}
		if min != "" && !refused(min) {
			t.Errorf("the validator accepts minCubeship %q", min)
		}
	}
}
