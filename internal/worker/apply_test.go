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
	created []dockerx.ContainerOpts
	// running is what RunningContainers answers, so a test can put the
	// agent in front of a machine that already has containers on it.
	running []dockerx.Running
	removed []string
	// capped is every SetResources call, newest last.
	capped []dockerx.Resources
}

func (e *fakeEngine) CreateContainer(_ context.Context, opts dockerx.ContainerOpts) (string, error) {
	e.created = append(e.created, opts)
	return "id-" + opts.Name, nil
}

func (e *fakeEngine) SetResources(_ context.Context, _ string, r dockerx.Resources) error {
	e.capped = append(e.capped, r)
	return nil
}

// createdNames is what the containers it made are called, in order.
func (e *fakeEngine) createdNames() []string {
	out := make([]string, 0, len(e.created))
	for _, o := range e.created {
		out = append(out, o.Name)
	}
	return out
}
func (e *fakeEngine) RunningNames(context.Context) ([]string, error) { return nil, nil }
func (e *fakeEngine) RunningContainers(context.Context) ([]dockerx.Running, error) {
	return e.running, nil
}
func (e *fakeEngine) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return nil }
func (e *fakeEngine) ContainerStats(context.Context, string) (dockerx.Stats, error) {
	return dockerx.Stats{}, nil
}
func (e *fakeEngine) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (e *fakeEngine) StartContainer(context.Context, string) error { return nil }
func (e *fakeEngine) StopContainer(context.Context, string) error  { return nil }
func (e *fakeEngine) RemoveContainer(_ context.Context, id string) error {
	e.removed = append(e.removed, id)
	return nil
}
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
	if names := engine.createdNames(); len(names) != 2 || names[0] == names[1] {
		t.Errorf("the copies were created as %v", names)
	}
}

// A ceiling is part of the instruction, not something the machine looks
// up: a worker has no database to ask what an app may use.
func TestTheCeilingArrivesWithThePlacement(t *testing.T) {
	engine := &fakeEngine{}
	a := New("https://cubeship.example.com", "token", "test", t.TempDir(), nil, engine, nil, nil)
	a.healthInterval = 0

	want := dockerx.Resources{NanoCPUs: 1_500_000_000, MemoryBytes: 512 << 20}
	a.apply(context.Background(), []node.Placement{
		{App: "web/production/api", Deploy: 7, Ordinal: 1,
			Container: "cubeship-web-production-api-7", Image: "nginx", Resources: want},
	}, "")

	if len(engine.created) != 1 {
		t.Fatalf("it created %d containers", len(engine.created))
	}
	if got := engine.created[0].Resources; got != want {
		t.Errorf("the container was created with %+v", got)
	}
}

// **A limit that changes does not wait for a deploy.** The Engine is the
// one thing that can write a new ceiling to a container that is already
// running, so the agent applies it on every pass rather than only when
// it creates something — which is what makes raising an app's memory on
// the control plane take effect on a worker in the next ten seconds.
func TestARunningCopyTakesANewCeilingWithoutBeingReplaced(t *testing.T) {
	engine := &fakeEngine{running: []dockerx.Running{{
		ID:     "already-there",
		Name:   "cubeship-web-production-api-7",
		Labels: map[string]string{node.LabelApp: "web/production/api"},
	}}}
	a := New("https://cubeship.example.com", "token", "test", t.TempDir(), nil, engine, nil, nil)
	a.healthInterval = 0

	want := dockerx.Resources{NanoCPUs: 2_000_000_000, MemoryBytes: 1 << 30}
	results := a.apply(context.Background(), []node.Placement{
		{App: "web/production/api", Deploy: 7, Ordinal: 1,
			Container: "cubeship-web-production-api-7", Image: "nginx", Resources: want},
	}, "")

	if len(engine.created) != 0 {
		t.Errorf("it replaced a container that was already running: %v", engine.createdNames())
	}
	if len(engine.removed) != 0 {
		t.Errorf("it removed %v", engine.removed)
	}
	// Nothing changed, so there is nothing to report: a result here
	// would mark the same deploy succeeded every ten seconds.
	if len(results) != 0 {
		t.Errorf("it reported %+v about a container it did not touch", results)
	}
	if len(engine.capped) != 1 || engine.capped[0] != want {
		t.Errorf("the new ceiling reached the Engine as %+v", engine.capped)
	}
}

// An app with no ceiling costs no Engine call, which is every app until
// somebody sets one — and an update could not lift a ceiling anyway,
// since the Engine reads a zero as "leave that one alone".
func TestAnUncappedCopyIsNotAskedAboutItsCeiling(t *testing.T) {
	engine := &fakeEngine{running: []dockerx.Running{{
		ID:     "already-there",
		Name:   "cubeship-web-production-api-7",
		Labels: map[string]string{node.LabelApp: "web/production/api"},
	}}}
	a := New("https://cubeship.example.com", "token", "test", t.TempDir(), nil, engine, nil, nil)
	a.healthInterval = 0

	a.apply(context.Background(), []node.Placement{
		{App: "web/production/api", Deploy: 7, Ordinal: 1,
			Container: "cubeship-web-production-api-7", Image: "nginx"},
	}, "")

	if len(engine.capped) != 0 {
		t.Errorf("it set a ceiling nobody asked for: %+v", engine.capped)
	}
}
