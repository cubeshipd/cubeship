package template

import (
	"errors"
	"testing"
)

func TestReplaceReferencesAsksForEachOne(t *testing.T) {
	got, err := ReplaceReferences("postgres://${db.main.user}:${db.main.password}@${db.main.host}/x?app=${input.name}",
		func(kind, key, attr string) (string, error) {
			return kind + "-" + key + "-" + attr, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if want := "postgres://db-main-user:db-main-password@db-main-host/x?app=input-name-"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReplaceReferencesStopsAtTheFirstItCannotAnswer(t *testing.T) {
	_, err := ReplaceReferences("${input.a}${input.b}", func(kind, key, attr string) (string, error) {
		return "", errors.New("no")
	})
	if err == nil || err.Error() != "${input.a}: no" {
		t.Errorf("err = %v", err)
	}
}

func TestSatisfies(t *testing.T) {
	for _, c := range []struct {
		min, version string
		want         bool
	}{
		{"0.6.0", "0.6.0", true},
		{"0.6.0", "0.7.1", true},
		{"0.6.0", "v1.0.0", true},
		{"0.6.0", "0.5.9", false},
		{">=0.6.0 <1.0.0", "1.2.0", false},
		{">=0.6.0, <1.0.0", "0.9.0", true},
		{"^0.6", "0.6.3", true},
		{"^0.6 || ^1", "1.4.0", true},
		{"0.6.0", "dev", true},
		{"", "0.1.0", true},
	} {
		got, err := Satisfies(c.min, c.version)
		if err != nil {
			t.Errorf("Satisfies(%q, %q): %v", c.min, c.version, err)
			continue
		}
		if got != c.want {
			t.Errorf("Satisfies(%q, %q) = %v, want %v", c.min, c.version, got, c.want)
		}
	}
}
