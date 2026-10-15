package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPredeployCommandJSONAcceptsShellAndArgv(t *testing.T) {
	for _, raw := range []string{`"echo ready"`, `["./migrate","--check"]`} {
		var got Predeploy
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		if got.Empty() {
			t.Fatalf("decoded empty command from %s", raw)
		}
	}
	if (Predeploy{Shell: "echo", Timeout: time.Hour + time.Second}).Valid() {
		t.Fatal("accepted a timeout over one hour")
	}
}
