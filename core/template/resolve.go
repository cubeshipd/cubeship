package template

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ReplaceReferences substitutes every ${kind.key} and ${kind.key.attr} in
// value with what lookup answers for it. The file can only prove that a
// reference names something it declares; what the reference stands for —
// a generated password, a hostname — exists only on the instance
// installing it, which is why the answer is the caller's.
func ReplaceReferences(value string, lookup func(kind, key, attr string) (string, error)) (string, error) {
	var failed error
	out := referencePattern.ReplaceAllStringFunc(value, func(raw string) string {
		if failed != nil {
			return raw
		}
		m := referencePattern.FindStringSubmatch(raw)
		resolved, err := lookup(m[1], m[2], m[3])
		if err != nil {
			failed = fmt.Errorf("%s: %w", raw, err)
			return raw
		}
		return resolved
	})
	return out, failed
}

// Satisfies reports whether an instance running version may install a
// template asking for minCubeship.
//
// A bare version is a minimum — "0.6.0" is 0.6.0 or anything after it,
// which is what the key's name promises, and what every template written
// so far means by it. Anything with an operator is a range. A version
// that is not a release, like the "dev" a local build reports, satisfies
// every template: there is nothing to compare.
func Satisfies(minCubeship, version string) (bool, error) {
	running, err := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	if err != nil {
		return true, nil
	}
	want := strings.TrimSpace(minCubeship)
	if want == "" {
		return true, nil
	}
	if !strings.ContainsAny(want, "<>=~^|*xX, ") {
		floor, err := semver.NewVersion(strings.TrimPrefix(want, "v"))
		if err != nil {
			return false, fmt.Errorf("minCubeship %q is not a version", minCubeship)
		}
		return !running.LessThan(floor), nil
	}
	constraint, err := semver.NewConstraint(want)
	if err != nil {
		return false, fmt.Errorf("minCubeship %q is not a version range", minCubeship)
	}
	return constraint.Check(running), nil
}
