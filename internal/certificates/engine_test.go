package certificates

import (
	"context"
	"io"
	"strings"
	"testing"

	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/dockerx"
)

// fakeEngine stands in for Docker: the two things this module asks it.
type fakeEngine struct {
	containers map[string]dockerx.ContainerInfo
	log        string
}

func (f fakeEngine) InspectContainerByName(_ context.Context, name string) (dockerx.ContainerInfo, error) {
	info, ok := f.containers[name]
	if !ok {
		return dockerx.ContainerInfo{}, io.EOF
	}
	return info, nil
}

func (f fakeEngine) Logs(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.log)), nil
}

// Three states the registry can be in, and only one of them means the
// name is really routed.
func TestWhenTheRegistrysNameCountsAsRouted(t *testing.T) {
	const host = "registry.example.com"
	routed := map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers.cubeship-registry.rule": "Host(`registry.example.com`)",
	}

	for name, tc := range map[string]struct {
		engine Engine
		want   bool
	}{
		"running with a router for it": {
			fakeEngine{containers: map[string]dockerx.ContainerInfo{
				bootstrap.RegistryContainerName: {ID: "r", Running: true, Labels: routed},
			}}, true,
		},
		"running with labels from before the domain": {
			fakeEngine{containers: map[string]dockerx.ContainerInfo{
				bootstrap.RegistryContainerName: {ID: "r", Running: true, Labels: map[string]string{}},
			}}, false,
		},
		"not running": {
			fakeEngine{containers: map[string]dockerx.ContainerInfo{
				bootstrap.RegistryContainerName: {ID: "r", Running: false, Labels: routed},
			}}, false,
		},
		"no container at all": {fakeEngine{}, false},
		// A report that cannot look must not accuse.
		"no engine to ask": {nil, true},
	} {
		s := &Service{}
		if tc.engine != nil {
			s.SetEngine(tc.engine)
		}
		if got := s.registryRouted(context.Background(), host); got != tc.want {
			t.Errorf("%s: routed = %v, want %v", name, got, tc.want)
		}
	}
}
