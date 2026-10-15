package app

import "testing"

func TestValidPathFilters(t *testing.T) {
	for _, tc := range []struct {
		name          string
		watch, ignore []string
		valid         bool
	}{
		{"empty", nil, nil, true},
		{"valid", []string{"services/api/**"}, []string{"**/*.md"}, true},
		{"absolute", []string{"/etc/**"}, nil, false},
		{"backslash", []string{"services\\api/**"}, nil, false},
		{"empty pattern", []string{""}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidPathFilters(tc.watch, tc.ignore); got != tc.valid {
				t.Fatalf("ValidPathFilters() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestWatchPathsMatch(t *testing.T) {
	if !WatchPathsMatch([]string{"services/api/**"}, nil, []string{"services/api/main.go"}) {
		t.Fatal("matching watched path should deploy")
	}
	if WatchPathsMatch([]string{"services/api/**"}, nil, []string{"services/web/main.go"}) {
		t.Fatal("unmatched path should skip")
	}
	if WatchPathsMatch(nil, []string{"**/*.md"}, []string{"README.md"}) {
		t.Fatal("ignored path should skip")
	}
	if !WatchPathsMatch(nil, nil, []string{"README.md"}) {
		t.Fatal("empty watch list should deploy")
	}
}
