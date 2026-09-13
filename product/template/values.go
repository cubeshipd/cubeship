package template

import (
	"regexp"
	"strconv"
	"strings"

	"cubeship/internal/slug"
)

// Copied from the daemon rather than imported, because importing them
// would pull the whole daemon into everything that reads a template.
// rules_test.go compares each one with its original.
const (
	minCPU        = 0.01    // internal/limits.MinCPU
	minMemory     = 6 << 20 // internal/limits.MinMemory
	maxAutoscale  = 100     // internal/app.MaxAutoscale
	maxHealthPath = 255     // internal/app.MaxHealthPathLength
	defaultPort   = 8080
)

var (
	keyPattern    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	prefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*_$`)
	tagPattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// validSlugShape is slug.Valid without the reserved words, which
// semantics.go refuses with a sentence of their own.
func validSlugShape(s string) bool { return slug.Valid(s) || slug.Reserved(s) }

// reserved are the names an instance keeps for itself, per kind.
var reserved = map[string]map[string]bool{
	"apps":      {"settings": true},
	"databases": {"settings": true, "engines": true},
	"stores":    {"settings": true, "providers": true},
}

// parseSize reads a memory size the way the CLI does: "512M" and
// "512Mi" are the same request.
func parseSize(in string) (int64, bool) {
	s := strings.TrimSpace(in)
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		factor int64
	}{
		{"Gi", 1 << 30}, {"G", 1 << 30}, {"Mi", 1 << 20}, {"M", 1 << 20},
		{"Ki", 1 << 10}, {"K", 1 << 10}, {"B", 1},
	} {
		if len(s) > len(u.suffix) && strings.EqualFold(s[len(s)-len(u.suffix):], u.suffix) {
			mult, s = u.factor, s[:len(s)-len(u.suffix)]
			break
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || s == "" {
		return 0, false
	}
	return int64(n * float64(mult)), true
}

// healthPathProblem is why the daemon would refuse path, or "".
func healthPathProblem(path string) string {
	switch {
	case !strings.HasPrefix(path, "/"):
		return "a health path has to start with /"
	case len(path) > maxHealthPath:
		return "a health path is at most 255 characters"
	case strings.Contains(path, "?"):
		return "a health path carries no query string"
	case strings.Contains(path, "#"):
		return "a health path carries no fragment"
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.IndexByte("/-._~%!$&'()*+,;=:@", c) >= 0 {
			continue
		}
		return "a health path holds letters, digits and /-._~%!$&'()*+,;=:@ only"
	}
	return ""
}
