package app_test

import (
	"fmt"
	"net/http"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/server/servertest"
)

type tcpPortView struct {
	ID            int64 `json:"id"`
	ContainerPort int   `json:"container_port"`
	HostPort      int   `json:"host_port"`
}

func publishTCP(t *testing.T, f *servertest.Fixture, ref string, body map[string]any) tcpPortView {
	t.Helper()
	var p tcpPortView
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps/"+ref+"/tcp-ports", body, f.AdminKey, &p),
		http.StatusCreated)
	return p
}

// A port is published on a host port the instance picks or one it is told,
// listed, and taken off again.
func TestATCPPortIsPublishedListedAndRemoved(t *testing.T) {
	f := servertest.New(t)
	git := createExternalApp(t, f, "git")

	picked := publishTCP(t, f, git.Reference, map[string]any{"container_port": 22})
	if picked.ContainerPort != 22 || picked.HostPort != app.TCPPortRangeStart {
		t.Fatalf("picked %+v", picked)
	}
	named := publishTCP(t, f, git.Reference, map[string]any{"container_port": 9418, "host_port": 2222})
	if named.HostPort != 2222 {
		t.Fatalf("named %+v", named)
	}

	var listed []tcpPortView
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+git.Reference+"/tcp-ports",
		nil, f.AdminKey, &listed), http.StatusOK)
	if len(listed) != 2 || listed[0].ContainerPort != 22 || listed[1].ContainerPort != 9418 {
		t.Fatalf("listed %+v", listed)
	}

	path := fmt.Sprintf("/apps/%s/tcp-ports/%d", git.Reference, picked.ID)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, path, nil, f.AdminKey), http.StatusNoContent)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, path, nil, f.AdminKey), http.StatusNotFound)
}

// A host port is published once on the instance, a container port once per
// app, and neither outside the numbers this instance publishes.
func TestATCPPortIsPublishedOnce(t *testing.T) {
	f := servertest.New(t)
	git := createExternalApp(t, f, "git")
	mqtt := createExternalApp(t, f, "mqtt")
	publishTCP(t, f, git.Reference, map[string]any{"container_port": 22, "host_port": 2222})

	for name, tc := range map[string]struct {
		ref  string
		body map[string]any
		want int
	}{
		"the same container port": {git.Reference, map[string]any{"container_port": 22, "host_port": 2223}, http.StatusConflict},
		"a taken host port":       {mqtt.Reference, map[string]any{"container_port": 1883, "host_port": 2222}, http.StatusConflict},
		"a privileged host port":  {mqtt.Reference, map[string]any{"container_port": 1883, "host_port": 22}, http.StatusBadRequest},
		"no container port":       {mqtt.Reference, map[string]any{"host_port": 2224}, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+tc.ref+"/tcp-ports", tc.body, f.AdminKey), tc.want)
		})
	}
}

// A published port is bound on the control plane by one container, so an
// app with one runs as one copy there, and an app that is not cannot
// publish one.
func TestATCPPortPinsTheAppToOneCopyOnTheControlPlane(t *testing.T) {
	f := servertest.New(t)
	_ = addServer(t, f, "eu-1")

	pinned := createExternalApp(t, f, "git")
	publishTCP(t, f, pinned.Reference, map[string]any{"container_port": 22})
	for name, body := range map[string]map[string]any{
		"two copies":      {"scale": 2},
		"another machine": {"nodes": []string{"eu-1"}},
		"spread":          {"spread": true},
		"autoscaling":     {"autoscale": map[string]any{"min": 1, "max": 3, "cpu": 70}},
	} {
		t.Run(name, func(t *testing.T) {
			servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/"+pinned.Reference, body, f.AdminKey),
				http.StatusConflict)
		})
	}

	spread := createExternalApp(t, f, "mqtt")
	place(t, f, spread.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+spread.Reference+"/tcp-ports",
		map[string]any{"container_port": 1883}, f.AdminKey), http.StatusConflict)
}
