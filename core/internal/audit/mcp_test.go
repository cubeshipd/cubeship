package audit

import (
	"strings"
	"testing"
)

// **An argument that could carry a value somebody set is never written
// down.** The log is read by every admin, and a variable's value in it
// would be a secret handed to all of them.
func TestATargetKeepsNamesAndDropsValues(t *testing.T) {
	got := targetOf([]byte(`{"app":"web/production/api","name":"api","environment":"staging","set":{"TOKEN":"s3cret"},"inputs":{"pw":"s3cret"},"password":"hunter2","api_key":"k","tag":"v1","volume_id":2}`))
	for _, want := range []string{"app=web/production/api", "name=api", "environment=staging", "tag=v1", "volume_id=2"} {
		if !strings.Contains(got, want) {
			t.Errorf("target %q lacks %q", got, want)
		}
	}
	for _, leaked := range []string{"s3cret", "hunter2", "api_key"} {
		if strings.Contains(got, leaked) {
			t.Errorf("target %q carries %q", got, leaked)
		}
	}
}
