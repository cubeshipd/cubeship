package node_test

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"cubeship/internal/node"
	"cubeship/internal/server/servertest"
)

// A machine that could not run what it was told reports why, and that
// is the deployment's error — so a failure on another box reads on the
// app's screen exactly like a failure here.
func TestAMachineThatCouldNotRunItSaysSo(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]any{
		"name": "consumer", "project": "web",
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey, &created), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey), http.StatusOK)

	var deployed struct {
		ID int64 `json:"id"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps/"+created.Reference+"/deploy",
		nil, f.AdminKey, &deployed), http.StatusAccepted)
	f.Server.Apps.Orchestrator().Wait()

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{
		Cores:   2,
		Results: []node.Result{{App: created.Reference, Deploy: deployed.ID, Error: "pull nginx: no such host"}},
	}, token), http.StatusOK)

	var after struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/apps/"+created.Reference+"/deployments/"+strconv.FormatInt(deployed.ID, 10), nil, f.AdminKey, &after), http.StatusOK)
	if after.Status != "failed" || !strings.Contains(after.Error, "no such host") {
		t.Errorf("the deploy is %q with %q, want the machine's own words", after.Status, after.Error)
	}
}

// The reverse channel, end to end.
//
// A worker dials the control plane and nothing dials a worker, so this
// is the only way this instance can ask a machine anything: the request
// parks here, the machine's own poll carries the question, and its
// answer releases the request. What a caller gets back is the log —
// the same bytes a local one is.
func TestAMachineIsAskedForALogThroughItsOwnPoll(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")
	ref := runOnNode(t, f, token, "eu-1", "consumer")

	// Somebody opens the log. This parks until the machine answers.
	type read struct {
		body string
		code int
	}
	asked := make(chan read, 1)
	go func() {
		rec := f.Do(t, http.MethodGet, "/apps/"+ref+"/logs?tail=200", nil, f.AdminKey)
		asked <- read{body: rec.Body.String(), code: rec.Code}
	}()

	// The machine polls. What it is handed is the question.
	var answer node.AgentResponse
	deadline := time.Now().Add(10 * time.Second)
	for len(answer.Commands) == 0 && time.Now().Before(deadline) {
		rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{Cores: 2}, token)
		servertest.RequireStatus(t, rec, http.StatusOK)
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
	}
	if len(answer.Commands) != 1 {
		t.Fatal("the machine was never asked for the log")
	}
	cmd := answer.Commands[0]
	if cmd.Kind != node.CommandLogs || cmd.Container != "container-on-eu-1" || cmd.Tail != "200" {
		t.Errorf("the command is %+v, want the log of the container it reported", cmd)
	}

	// It answers with what the Engine gave it: stdout and stderr behind
	// Docker's own 8-byte frame header. Demultiplexing is the control
	// plane's, in the one place that already does it for every log it
	// serves — so what is posted here is deliberately not readable text.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes/agent/results/"+cmd.ID,
		dockerFrame("hello from eu-1\n"), token), http.StatusNoContent)

	select {
	case got := <-asked:
		if got.code != http.StatusOK {
			t.Fatalf("reading the log: %d %s", got.code, got.body)
		}
		if got.body != "hello from eu-1\n" {
			t.Errorf("the log came back as %q", got.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the request that asked for the log was never released")
	}
}

// A machine may only answer what was sent to it. Without that a
// worker's credential would be a way to feed somebody else's screen
// whatever it liked.
func TestAMachineCannotAnswerAnotherMachinesQuestion(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")
	other := add(t, f, "eu-2")
	ref := runOnNode(t, f, token, "eu-1", "consumer")

	asked := make(chan int, 1)
	go func() {
		asked <- f.Do(t, http.MethodGet, "/apps/"+ref+"/logs?tail=200", nil, f.AdminKey).Code
	}()

	var answer node.AgentResponse
	deadline := time.Now().Add(10 * time.Second)
	for len(answer.Commands) == 0 && time.Now().Before(deadline) {
		rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{Cores: 2}, token)
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
	}
	if len(answer.Commands) != 1 {
		t.Fatal("the machine was never asked")
	}

	// The other machine tries to answer it. Dropped without complaint —
	// it is not doing anything this instance has to explain to it — and
	// the request stays parked.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes/agent/results/"+answer.Commands[0].ID,
		[]byte("not mine to say"), other), http.StatusNoContent)

	select {
	case code := <-asked:
		t.Fatalf("the request was released by the wrong machine: %d", code)
	case <-time.After(time.Second):
		// Still waiting, which is right.
	}
}

// runOnNode places an app on a machine and reports it running there,
// which is the state every question about a remote app starts from.
func runOnNode(t *testing.T, f *servertest.Fixture, token, on, name string) string {
	t.Helper()
	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]any{
		"name": name, "project": "web",
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey, &created), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": on}, f.AdminKey), http.StatusOK)

	var deployed struct {
		ID int64 `json:"id"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps/"+created.Reference+"/deploy",
		nil, f.AdminKey, &deployed), http.StatusAccepted)
	f.Server.Apps.Orchestrator().Wait()

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{
		Cores:   2,
		Results: []node.Result{{App: created.Reference, Deploy: deployed.ID, Container: "container-on-eu-1"}},
	}, token), http.StatusOK)
	return created.Reference
}

// A poll that asks to wait comes back the moment there is something to
// say, rather than when its wait runs out. That is the whole of what
// the channel buys: without it a machine hears about a deploy, or a
// question, on its next interval.
func TestAWaitingPollIsReleasedTheMomentThereIsSomethingToSay(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")
	ref := runOnNode(t, f, token, "eu-1", "consumer")

	// A deploy placed on a machine wakes it, and that mark is still on
	// the channel: one poll consumes it and comes back with nothing,
	// which is what a wake with nothing behind it looks like. Drained
	// here so what follows is measuring the release this test is about.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes/agent/reconcile",
		node.AgentRequest{Cores: 2, Wait: true}, token), http.StatusOK)

	parked := make(chan time.Duration, 1)
	go func() {
		started := time.Now()
		rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile",
			node.AgentRequest{Cores: 2, Wait: true}, token)
		servertest.RequireStatus(t, rec, http.StatusOK)
		var answer node.AgentResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Error(err)
		}
		if len(answer.Commands) != 1 {
			t.Errorf("the poll came back with %d commands", len(answer.Commands))
		}
		parked <- time.Since(started)
	}()

	// Give the poll a moment to park, then ask for something.
	time.Sleep(100 * time.Millisecond)
	go func() {
		f.Do(t, http.MethodGet, "/apps/"+ref+"/logs?tail=200", nil, f.AdminKey)
	}()

	select {
	case took := <-parked:
		if took > node.PollWait/2 {
			t.Errorf("the poll waited %s, which is its timeout rather than the answer", took)
		}
	case <-time.After(node.PollWait):
		t.Fatal("the poll was never released")
	}
}

// dockerFrame wraps text the way the Engine hands a log over: one
// 8-byte header per chunk, the stream in the first byte and the length
// in the last four.
func dockerFrame(text string) []byte {
	out := make([]byte, 8, 8+len(text))
	out[0] = 1 // stdout
	binary.BigEndian.PutUint32(out[4:], uint32(len(text)))
	return append(out, text...)
}

// A container on another machine is charted like one here.
//
// The reading comes from the machine, because nothing on the control
// plane can reach its Engine — and it arrives as a percentage rather
// than as counters, since a percentage is a difference and only the
// machine holding the previous reading can take it. What it lands in is
// the same table, on the same axis.
func TestWhatAContainerOnAnotherMachineIsUsingIsCharted(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")
	ref := runOnNode(t, f, token, "eu-1", "consumer")

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{
		Cores: 2,
		Readings: []node.Reading{{
			Container: "container-on-eu-1", CPUPercent: 42.5,
			MemoryBytes: 128 << 20, MemoryLimitBytes: 2 << 30,
		}},
	}, token), http.StatusOK)

	var series struct {
		Samples []struct {
			CPUPercent  float64 `json:"cpu_percent"`
			MemoryBytes int64   `json:"memory_bytes"`
		} `json:"samples"`
		Collecting bool `json:"collecting"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+ref+"/metrics", nil, f.AdminKey, &series), http.StatusOK)
	if len(series.Samples) != 1 {
		t.Fatalf("the app's chart has %d points, want the one its machine reported", len(series.Samples))
	}
	if series.Samples[0].CPUPercent != 42.5 || series.Samples[0].MemoryBytes != 128<<20 {
		t.Errorf("the point is %+v, want what the machine said", series.Samples[0])
	}
	if !series.Collecting {
		t.Error("an app running on a machine that reports readings is not being collected")
	}
}

// A reading for a container this instance does not know is dropped.
//
// It is how a machine that is behind reports on a container that has
// since been replaced, and writing it against the app anyway would draw
// the old version's line on the new one's chart.
func TestAReadingForAContainerNobodyKnowsIsDropped(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")
	ref := runOnNode(t, f, token, "eu-1", "consumer")

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{
		Cores: 2,
		Readings: []node.Reading{
			{Container: "a-container-from-before", CPUPercent: 99, MemoryBytes: 1 << 30},
		},
	}, token), http.StatusOK)

	var series struct {
		Samples []map[string]any `json:"samples"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+ref+"/metrics", nil, f.AdminKey, &series), http.StatusOK)
	if len(series.Samples) != 0 {
		t.Errorf("a reading for a container nobody knows was charted: %v", series.Samples)
	}
}
