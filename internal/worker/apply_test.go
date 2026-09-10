package worker

import (
	"context"
	"io"
	"strings"
	"testing"

	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
)

// fakeEngine is a Docker that says yes to everything and remembers the
// containers it was asked to create. What these tests are about is what
// the agent reports back, not what Docker did with it.
type fakeEngine struct {
	created []string
}

func (e *fakeEngine) CreateContainer(_ context.Context, opts dockerx.ContainerOpts) (string, error) {
	e.created = append(e.created, opts.Name)
	return "id-" + opts.Name, nil
}
func (e *fakeEngine) RunningNames(context.Context) ([]string, error) { return nil, nil }
func (e *fakeEngine) RunningContainers(context.Context) ([]dockerx.Running, error) {
	return nil, nil
}
func (e *fakeEngine) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return nil }
func (e *fakeEngine) ContainerStats(context.Context, string) (dockerx.Stats, error) {
	return dockerx.Stats{}, nil
}
func (e *fakeEngine) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (e *fakeEngine) StartContainer(context.Context, string) error  { return nil }
func (e *fakeEngine) StopContainer(context.Context, string) error   { return nil }
func (e *fakeEngine) RemoveContainer(context.Context, string) error { return nil }
func (e *fakeEngine) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}
func (e *fakeEngine) Swarm(context.Context) (dockerx.SwarmState, error) {
	return dockerx.SwarmState{}, nil
}
func (e *fakeEngine) SwarmInit(context.Context, string) error                 { return nil }
func (e *fakeEngine) SwarmWorkerToken(context.Context) (string, error)        { return "", nil }
func (e *fakeEngine) SwarmJoin(context.Context, string, string, string) error { return nil }
func (e *fakeEngine) EnsureOverlayNetwork(context.Context, string) error      { return nil }
func (e *fakeEngine) NetworkExists(context.Context, string) (bool, error)     { return true, nil }

// **The ordinal goes home untouched.** It is how the control plane knows
// which copy of an app on this machine a result is about, and an agent
// that drops it makes every report one about a copy nothing asked for —
// so every remote deploy stays `pending` for ever, on every app, with
// nothing anywhere saying why.
//
// That is what happened: the field was added to both ends of the wire
// and never filled in on this one. This package had no tests at all,
// which is why it took a full CI run to find.
func TestTheAgentReportsWhichCopyItRan(t *testing.T) {
	engine := &fakeEngine{}
	a := New("https://cubeship.example.com", "token", "test", t.TempDir(), nil, engine, nil, nil)
	// The real watch is ten seconds a container, and what is under test
	// is what comes back rather than how long it waited.
	a.healthInterval = 0

	results := a.apply(context.Background(), []node.Placement{
		{App: "web/production/api", Deploy: 7, Ordinal: 1, Container: "cubeship-web-production-api-7", Image: "nginx"},
		{App: "web/production/api", Deploy: 7, Ordinal: 2, Container: "cubeship-web-production-api-7-2", Image: "nginx"},
	}, "")

	if len(results) != 2 {
		t.Fatalf("the agent reported %d of the two copies it was told to run", len(results))
	}
	for i, want := range []int{1, 2} {
		if results[i].Ordinal != want {
			t.Errorf("copy %d came back as ordinal %d, which is a report about a copy nothing asked for",
				want, results[i].Ordinal)
		}
		if results[i].Deploy != 7 || results[i].App != "web/production/api" {
			t.Errorf("result %d is %+v", i, results[i])
		}
	}
	// And the two are different containers, which is the whole of how
	// several copies on one machine are told apart.
	if len(engine.created) != 2 || engine.created[0] == engine.created[1] {
		t.Errorf("the copies were created as %v", engine.created)
	}
}
