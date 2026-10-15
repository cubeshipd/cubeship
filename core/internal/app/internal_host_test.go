package app

import (
	"strings"
	"testing"
)

// The one property the internal name has to keep: it is the container
// name with the changing part taken off, so an app is reachable at it
// through a deploy that renames the container underneath.
//
// **Both strings are built here, in this package, from the same
// helper** — which is exactly why this test exists rather than being
// redundant. A future name that carried the ordinal, a prefix, or a
// suffix would still compile and still look right on the screen: what
// would break is that the alias no longer names a container that is
// running, and nothing else in the suite would notice.
func TestTheInternalNameSurvivesADeploy(t *testing.T) {
	ref := Reference{Project: "web", Environment: "production", Name: "api"}
	host := InternalHost(ref)

	if host != "cubeship-web-production-api" {
		t.Fatalf("internal host is %q, and it is an address people write down", host)
	}

	// Two deploys, and the second copy of the second one, which is the
	// shape that carries the most on the end of it.
	for _, name := range []string{
		containerNameFor(host, 1, 1),
		containerNameFor(host, 1421, 1),
		containerNameFor(host, 1421, 3),
	} {
		if name == host {
			t.Fatalf("container %q is the alias itself, which cannot be attached to it", name)
		}
		if !strings.HasPrefix(name, host+"-") {
			t.Fatalf("container %q is not %q plus a deploy: the alias names nothing", name, host)
		}
	}
}

// A slug is the only thing a reference's parts can be, so the name
// cannot grow a character Docker refuses in a hostname — which is the
// other way an alias stops working, and it fails at container creation
// rather than at the call that uses it.
func TestTheInternalNameIsALegalHostname(t *testing.T) {
	host := InternalHost(Reference{
		Project: "my-shop", Environment: "staging-2", Name: "api-v2",
	})
	for _, r := range host {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			t.Fatalf("%q is in %q, and a hostname may not hold it", r, host)
		}
	}
}
