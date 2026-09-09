package node_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"cubeship/internal/node"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

type server struct {
	Name         string   `json:"name"`
	ControlPlane bool     `json:"control_plane"`
	Status       string   `json:"status"`
	Address      string   `json:"address"`
	Version      string   `json:"version"`
	Cores        int      `json:"cores"`
	MemoryTotal  int64    `json:"memory_total_bytes"`
	CPUPercent   *float64 `json:"cpu_percent"`
	Containers   int      `json:"containers"`
	Token        string   `json:"token"`
}

// Every instance is a cluster of one before anybody adds anything, and
// the machine it is installed on is in the table rather than implied.
// A screen listing "the other servers" is a screen that cannot answer
// where an app runs.
func TestTheInstanceIsAlreadyAClusterOfOne(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	var cluster []server
	rec := f.Do(t, http.MethodGet, "/nodes", nil, memberKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("list the cluster as a member: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cluster); err != nil {
		t.Fatal(err)
	}
	if len(cluster) != 1 {
		t.Fatalf("a fresh instance listed %d servers, want the one it runs on", len(cluster))
	}
	if !cluster[0].ControlPlane || cluster[0].Name != node.ControlPlaneSlug {
		t.Errorf("the one server is %+v, want the control plane", cluster[0])
	}
	// It is the process answering the question. It cannot be
	// unreachable from itself.
	if cluster[0].Status != node.StatusReady {
		t.Errorf("the control plane reported %q", cluster[0].Status)
	}

	// And it is not a machine this instance joined, so it cannot leave.
	if rec = f.Do(t, http.MethodDelete, "/nodes/"+node.ControlPlaneSlug, nil, f.AdminKey); rec.Code != http.StatusConflict {
		t.Errorf("removing the control plane: %d %s, want 409", rec.Code, rec.Body.String())
	}
}

// Adding a machine mints a credential and shows it once. What follows
// is the whole of joining: somebody runs the installer on the box with
// that credential, and the agent calls in.
//
// Nothing is contacted when the row is made. The machine may not exist
// yet — that is what `pending` means, and it is a state rather than a
// failure.
func TestAddingAServerMintsACredentialShownOnce(t *testing.T) {
	f := servertest.New(t)

	rec := f.Do(t, http.MethodPost, "/nodes",
		map[string]any{"name": "eu-1", "description": "Frankfurt"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add a server: %d %s", rec.Code, rec.Body.String())
	}
	var created server
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" {
		t.Fatal("no credential came back from the one request that carries one")
	}
	if created.Status != node.StatusPending {
		t.Errorf("a server nobody has installed yet is %q, want pending", created.Status)
	}

	// Every other read of that server, at any role, carries nothing.
	var one server
	rec = f.Do(t, http.MethodGet, "/nodes/eu-1", nil, f.AdminKey)
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.Token != "" {
		t.Error("a server's credential came back from a read")
	}
	var cluster []server
	rec = f.Do(t, http.MethodGet, "/nodes", nil, f.AdminKey)
	if err := json.Unmarshal(rec.Body.Bytes(), &cluster); err != nil {
		t.Fatal(err)
	}
	for _, s := range cluster {
		if s.Token != "" {
			t.Errorf("%s's credential came back from the listing", s.Name)
		}
	}

	// The name is taken now, and names here are permanent.
	if rec = f.Do(t, http.MethodPost, "/nodes", map[string]any{"name": "eu-1"}, f.AdminKey); rec.Code != http.StatusConflict {
		t.Errorf("re-using a name: %d, want 409", rec.Code)
	}
}

// The agent surface is a machine's, not a person's, and the two do not
// meet: a node's credential reaches one endpoint and nothing else, and
// an account's key does not reach that one at all.
func TestTheAgentsCredentialIsAMachinesAndReachesOneEndpoint(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")

	// A node's credential is not an account's.
	if rec := f.Do(t, http.MethodGet, "/nodes", nil, token); rec.Code != http.StatusUnauthorized {
		t.Errorf("a node's credential listing the cluster: %d, want 401", rec.Code)
	}
	// And an account's key is not a node's.
	if rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile", map[string]any{}, f.AdminKey); rec.Code != http.StatusUnauthorized {
		t.Errorf("an admin's key reconciling as a machine: %d, want 401", rec.Code)
	}
	if rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile", map[string]any{}, "not-a-credential"); rec.Code != http.StatusUnauthorized {
		t.Errorf("an invented credential: %d, want 401", rec.Code)
	}
}

// One pass of the loop: the machine says what it is, and what it said
// is what the cluster screen shows. This is the whole of joining —
// there is no separate handshake, because a first pass is a join and
// every pass after it is the same request.
func TestAPassIsAJoinAndAHeartbeatAtOnce(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")

	cpu := 12.5
	memory := int64(2 << 30)
	rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{
		Version: "1.2.3", Address: "203.0.113.9",
		Cores: 8, MemoryTotalBytes: 16 << 30, DiskTotalBytes: 200 << 30,
		CPUPercent: &cpu, MemoryBytes: &memory, Containers: 3,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("reconcile: %d %s", rec.Code, rec.Body.String())
	}
	var answer node.AgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Name != "eu-1" {
		t.Errorf("the machine was told it is %q", answer.Name)
	}
	// The cadence is served rather than compiled into the agent, so a
	// cluster's clock is the control plane's to change.
	if answer.IntervalSeconds <= 0 {
		t.Errorf("interval = %d, and the agent has nothing to wait for", answer.IntervalSeconds)
	}
	// Nothing is placed yet, and the shape is what matters: the loop
	// already asks the question it will later be answered with.
	if answer.Desired.Apps == nil {
		t.Error("desired apps came back null, which is a different bug in every agent")
	}

	var one server
	rec = f.Do(t, http.MethodGet, "/nodes/eu-1", nil, f.AdminKey)
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.Status != node.StatusReady {
		t.Errorf("a machine that just called is %q", one.Status)
	}
	if one.Cores != 8 || one.MemoryTotal != 16<<30 || one.Containers != 3 {
		t.Errorf("what the machine said was not what the cluster shows: %+v", one)
	}
	if one.Address != "203.0.113.9" || one.Version != "1.2.3" {
		t.Errorf("address %q, version %q", one.Address, one.Version)
	}
	if one.CPUPercent == nil || *one.CPUPercent != 12.5 {
		t.Errorf("cpu = %v", one.CPUPercent)
	}
}

// Removing a machine is a local act — nothing reaches out to a box that
// may be off — and the credential goes with the row. What the agent
// gets on its next pass is the one refusal it is written to explain:
// this machine is not in that cluster any more.
func TestRemovingAServerRefusesItsAgentFromThenOn(t *testing.T) {
	f := servertest.New(t)
	token := add(t, f, "eu-1")

	if rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{Cores: 2}, token); rec.Code != http.StatusOK {
		t.Fatalf("the machine could not call in: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.Do(t, http.MethodDelete, "/nodes/eu-1", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile", node.AgentRequest{Cores: 2}, token); rec.Code != http.StatusUnauthorized {
		t.Errorf("a removed machine's credential still worked: %d", rec.Code)
	}
}

// A machine runs other people's code on hardware somebody pays for, and
// adding one hands out a credential. Reading which machines exist is
// not the same act: which box an app is on is part of knowing why it is
// slow.
func TestAddingAndRemovingIsAnAdminsAndReadingIsNot(t *testing.T) {
	f := servertest.New(t)
	add(t, f, "eu-1")
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	if rec := f.Do(t, http.MethodGet, "/nodes", nil, memberKey); rec.Code != http.StatusOK {
		t.Errorf("a member listing the cluster: %d", rec.Code)
	}
	if rec := f.Do(t, http.MethodPost, "/nodes", map[string]any{"name": "eu-2"}, memberKey); rec.Code != http.StatusForbidden {
		t.Errorf("a member adding a server: %d, want 403", rec.Code)
	}
	if rec := f.Do(t, http.MethodDelete, "/nodes/eu-1", nil, memberKey); rec.Code != http.StatusForbidden {
		t.Errorf("a member removing a server: %d, want 403", rec.Code)
	}
}

func add(t *testing.T, f *servertest.Fixture, name string) string {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/nodes", map[string]any{"name": name}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add %q: %d %s", name, rec.Code, rec.Body.String())
	}
	var created server
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created.Token
}
