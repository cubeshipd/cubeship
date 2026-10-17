package app

import "testing"

func TestManualLifecycleStatuses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status string
		want   string
	}{
		{name: "stopped replicas stay stopped", status: StatusStopped, want: StatusStopped},
		{name: "starting replicas report starting", status: StatusStarting, want: StatusStarting},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := &App{Replicas: []Replica{{Status: test.status}, {Status: test.status}}}
			if got := app.Status(); got != test.want {
				t.Fatalf("Status() = %q, want %q", got, test.want)
			}
		})
	}
}
