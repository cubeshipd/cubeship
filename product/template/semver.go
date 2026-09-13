package template

import (
	"regexp"
	"strings"
)

var (
	comparator = regexp.MustCompile(`^(<=|>=|<|>|=|~>|~|\^)?v?(\d+|[xX*])(\.(\d+|[xX*]))?(\.(\d+|[xX*]))?(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	operator   = regexp.MustCompile(`^(<=|>=|<|>|=|~>|~|\^)$`)
)

// validRange reports whether s is a version range in npm's grammar —
// "0.6.0", ">=0.6.0 <1.0.0", "^0.6 || ^1" — which is what minCubeship
// has always been documented as.
func validRange(s string) bool {
	for _, alt := range strings.Split(s, "||") {
		tokens := strings.Fields(alt)
		if len(tokens) == 0 {
			if strings.TrimSpace(s) == "" {
				return false
			}
			continue
		}
		for i := 0; i < len(tokens); i++ {
			t := tokens[i]
			if operator.MatchString(t) && i+1 < len(tokens) {
				t += tokens[i+1]
				i++
			}
			if t == "-" && i > 0 && i+1 < len(tokens) {
				continue
			}
			if !comparator.MatchString(t) {
				return false
			}
		}
	}
	return true
}
