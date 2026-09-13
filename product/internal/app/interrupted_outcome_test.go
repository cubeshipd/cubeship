package app

import "testing"

// The decision SettleInterrupted makes, without the database it reads
// from: what a pending deploy found at startup ends as.
func TestWhatAnInterruptedDeployEndsAs(t *testing.T) {
	const here, eu1 = 1, 2
	on := func(node int64, deploy int64, status string) Replica {
		return Replica{NodeID: node, Ordinal: 1, Container: "c", Deploy: deploy, Status: status}
	}
	for _, tc := range []struct {
		name     string
		image    string
		replicas []Replica
		want     string
	}{
		{"asked for a tag and never built", "master", []Replica{on(eu1, 0, StatusPending)}, DeploymentFailed},
		{"asked for nothing and never resolved", "", []Replica{on(eu1, 0, StatusPending)}, DeploymentFailed},
		{"resolved, and this machine never swapped", "nginx:1.27", []Replica{on(here, 6, StatusRunning)}, DeploymentFailed},
		{"swapped here, row never closed", "nginx:1.27", []Replica{on(here, 7, StatusRunning)}, DeploymentSucceeded},
		{"swapped here, still waiting on eu-1", "nginx:1.27", []Replica{on(here, 7, StatusRunning), on(eu1, 6, StatusRunning)}, ""},
		{"only on eu-1, which has not taken it", "registry.example.com/web/production/api:v2", []Replica{on(eu1, 6, StatusRunning)}, ""},
		{"placed nowhere", "nginx:1.27", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Deployment{ID: 7, ImageRef: tc.image, Status: DeploymentPending}
			got, _ := interruptedOutcome(d, &App{Replicas: tc.replicas}, here)
			if got != tc.want {
				t.Errorf("ends as %q, want %q", got, tc.want)
			}
		})
	}
}
