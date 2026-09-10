package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/node"
	"cubeship/internal/server/servertest"
)

// reconcile is one pass of a machine's own loop: what it did since the
// last one, and what it is told to be running now.
func reconcile(t *testing.T, f *servertest.Fixture, token string, results ...node.Result) node.AgentResponse {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/nodes/agent/reconcile",
		node.AgentRequest{Cores: 2, Address: "203.0.113.9", Results: results}, token)
	servertest.RequireStatus(t, rec, http.StatusOK)
	var answer node.AgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	return answer
}

// balancerFixture is a cluster whose Docker says yes: these tests are
// about which machine runs what and where the traffic goes, and
// servertest's default answers "no Docker here", which would make every
// local half of a deploy a failure.
func balancerFixture(t *testing.T) *servertest.Fixture {
	t.Helper()
	return servertest.NewWithDocker(t, quietDocker{})
}

// deploy asks for one and waits for it to stop being this instance's
// problem — which for an app on another machine is as far as the
// control plane goes.
func deploy(t *testing.T, f *servertest.Fixture, ref string) int64 {
	t.Helper()
	var d struct {
		ID int64 `json:"id"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps/"+ref+"/deploy",
		nil, f.AdminKey, &d), http.StatusAccepted)
	f.Server.Apps.WaitForDeploys()
	return d.ID
}

func deploymentStatus(t *testing.T, f *servertest.Fixture, ref string, id int64) string {
	t.Helper()
	var d struct {
		Status string `json:"status"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		fmt.Sprintf("/apps/%s/deployments/%d", ref, id), nil, f.AdminKey, &d), http.StatusOK)
	return d.Status
}

func place(t *testing.T, f *servertest.Fixture, ref string, body map[string]any) placedApp {
	t.Helper()
	var out placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+ref, body, f.AdminKey, &out), http.StatusOK)
	return out
}

// The whole of scaling an app out: it runs on more than one machine.
// Nothing about the app changes shape — it has a set of machines, and
// one machine is the ordinary size of that set.
func TestAnAppCanRunOnSeveralMachinesAtOnce(t *testing.T) {
	f := balancerFixture(t)
	_ = addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "api")

	moved := place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})
	if len(moved.Nodes) != 2 {
		t.Fatalf("the app runs on %v", moved.Nodes)
	}
	// The edge nobody named is the one it already had. Scaling an app
	// out must not silently move the machine its DNS record points at.
	if moved.Node != node.ControlPlaneSlug {
		t.Errorf("adding a machine moved the app's traffic to %q", moved.Node)
	}
}

// Where an app runs and where its traffic arrives are two decisions, and
// the second is constrained by the first: an edge that does not run the
// app would be balancing across a set it is not in.
func TestTheMachineThatServesAnAppHasToRunIt(t *testing.T) {
	f := balancerFixture(t)
	_ = addServer(t, f, "eu-1")
	_ = addServer(t, f, "eu-2")
	created := createExternalApp(t, f, "api")

	rec := f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"nodes": []string{"eu-1"}, "node": "eu-2"}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("serving an app from a machine that does not run it: %d %s", rec.Code, rec.Body.String())
	}

	// And an edge that is dropped from the set follows it rather than
	// being left pointing at a machine that has been told to stop.
	moved := place(t, f, created.Reference, map[string]any{"nodes": []string{"eu-1"}})
	if moved.Node != "eu-1" {
		t.Errorf("the app is served from %q, which is not one of %v", moved.Node, moved.Nodes)
	}
}

// Every machine an app runs on is told to run it, and **none of their
// containers carries a router**: a label-router names one backend, the
// container it is on, so two machines carrying the labels for one name
// would be two Traefiks each sending all of the traffic to itself.
func TestEveryMachineRunsItAndNoneOfThemRoutesItAlone(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "web")
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": "web.example.com"}, f.AdminKey), http.StatusCreated)

	// One machine: the container routes its own name, exactly as it did
	// before any of this existed.
	place(t, f, created.Reference, map[string]any{"nodes": []string{"eu-1"}})
	deploy(t, f, created.Reference)
	answer := reconcile(t, f, token)
	if len(answer.Desired.Apps) != 1 {
		t.Fatalf("the machine was told to run %d apps", len(answer.Desired.Apps))
	}
	// The labels carry the name and the port behind it, not just the
	// switch: a placement that went out without them is a machine
	// serving none of the names of the apps it runs, and — because
	// starting its edge is decided by looking for them — one that never
	// starts a Traefik at all.
	labels := answer.Desired.Apps[0].Labels
	if labels["traefik.enable"] != "true" {
		t.Errorf("the only machine running it does not route it: %v", labels)
	}
	if !strings.Contains(strings.Join(mapValues(labels), " "), "web.example.com") {
		t.Errorf("the placement does not carry the name it has to serve: %v", labels)
	}

	// Two machines: neither does, and the edge's file is what routes.
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})
	deploy(t, f, created.Reference)
	answer = reconcile(t, f, token)
	if len(answer.Desired.Apps) != 1 {
		t.Fatalf("the machine was told to run %d apps", len(answer.Desired.Apps))
	}
	if _, routes := answer.Desired.Apps[0].Labels["traefik.enable"]; routes {
		t.Errorf("a replica still carries a router of its own: %v", answer.Desired.Apps[0].Labels)
	}
	// It is still labelled as ours, which is what the agent removes a
	// container it should no longer run by.
	if answer.Desired.Apps[0].Labels[node.LabelApp] == "" {
		t.Error("a replica lost the label that says whose it is")
	}
}

// The load balancer itself: the machine an app's traffic arrives at is
// given every replica as a backend, addressed by container name — which
// is what resolves from any machine on the mesh.
func TestTheEdgeIsGivenEveryReplicaAsABackend(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "web")
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": "web.example.com", "port": 3000}, f.AdminKey), http.StatusCreated)

	// Served from eu-1 and running on both, so eu-1 is the machine with
	// the routes and the control plane has none.
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}, "node": "eu-1"})
	deploy(t, f, created.Reference)

	// Nothing is a backend until it is actually running something. The
	// control plane's own container came up during the deploy; eu-1's
	// has not, so the route names one machine.
	answer := reconcile(t, f, token)
	if len(answer.Desired.Apps) != 1 {
		t.Fatalf("eu-1 was told to run %d apps", len(answer.Desired.Apps))
	}
	placement := answer.Desired.Apps[0]

	// And now it has.
	answer = reconcile(t, f, token, node.Result{
		App: created.Reference, Deploy: placement.Deploy, Container: "container-on-eu-1",
	})
	if len(answer.Desired.Routes) != 1 {
		t.Fatalf("the edge was given %d routes, want the one name it serves: %+v", len(answer.Desired.Routes), answer.Desired.Routes)
	}
	route := answer.Desired.Routes[0]
	if route.Host != "web.example.com" {
		t.Errorf("the route is for %q", route.Host)
	}
	if len(route.Servers) != 2 {
		t.Fatalf("the balancer has %d backends, want one per machine: %v", len(route.Servers), route.Servers)
	}
	for _, s := range route.Servers {
		// The port is the domain's, not the container's guess, and the
		// host is a container name — an address, not an IP that would
		// go stale the next time that replica is replaced.
		if !strings.HasSuffix(s, ":3000") || !strings.HasPrefix(s, "http://cubeship-") {
			t.Errorf("backend %q is not a container of this app on its own port", s)
		}
	}
}

// A deploy is not finished when the first machine has it. An app on two
// boxes whose deploy was marked succeeded by the first would report a
// rollout that is half done as done, and the machine still pulling would
// look like nothing was happening.
func TestADeployIsPendingUntilEveryMachineHasIt(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})

	d := deploy(t, f, created.Reference)
	if got := deploymentStatus(t, f, created.Reference, d); got != "pending" {
		t.Fatalf("the deploy is %q with one of two machines still to report", got)
	}

	answer := reconcile(t, f, token)
	if len(answer.Desired.Apps) != 1 {
		t.Fatalf("eu-1 was told to run %d apps", len(answer.Desired.Apps))
	}
	reconcile(t, f, token, node.Result{
		App: created.Reference, Deploy: answer.Desired.Apps[0].Deploy, Container: "container-on-eu-1",
	})

	if got := deploymentStatus(t, f, created.Reference, d); got != app.DeploymentSucceeded {
		t.Errorf("the deploy is %q with every machine reporting it running", got)
	}
}

// And one machine failing fails it, at once rather than when the last
// machine has been heard from: a deploy that is going to be reported
// failed should say so while somebody is still watching it.
func TestOneMachineFailingFailsTheDeploy(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})

	d := deploy(t, f, created.Reference)
	answer := reconcile(t, f, token)
	reconcile(t, f, token, node.Result{
		App: created.Reference, Deploy: answer.Desired.Apps[0].Deploy,
		Error: "no space left on device",
	})

	if got := deploymentStatus(t, f, created.Reference, d); got != app.DeploymentFailed {
		t.Errorf("the deploy is %q after a machine refused it", got)
	}
}

// Scaling back down must not take a name off the internet. The
// container left behind was created while the app was on two machines,
// so it carries no router of its own — the edge goes on routing it
// until a deploy puts the labels back.
func TestScalingBackDownKeepsTheNameServed(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "web")
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": "web.example.com"}, f.AdminKey), http.StatusCreated)

	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})
	deploy(t, f, created.Reference)
	answer := reconcile(t, f, token)
	reconcile(t, f, token, node.Result{
		App: created.Reference, Deploy: answer.Desired.Apps[0].Deploy, Container: "container-on-eu-1",
	})

	// Back to this machine alone. Its container routes nothing, so the
	// edge still has to.
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane"}})
	routes, err := f.Server.Apps.RoutesFor(t.Context(), controlPlaneID(t, f))
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("the edge serves %d names after scaling down: %+v", len(routes), routes)
	}
	if len(routes[0].Servers) != 1 {
		t.Errorf("the route still names the machine that left: %v", routes[0].Servers)
	}
}

func controlPlaneID(t *testing.T, f *servertest.Fixture) int64 {
	t.Helper()
	id, err := app.NewRepository(f.DB).ControlPlaneID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// The health path reaches the machine that serves the app, because that
// is the Traefik doing the checking — the replicas are on other boxes
// and this one is the only thing in front of them.
func TestTheHealthPathReachesTheEdgeThatBalances(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "web")
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": "web.example.com"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"health_path": "/healthz"}, f.AdminKey), http.StatusOK)

	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}, "node": "eu-1"})
	deploy(t, f, created.Reference)
	answer := reconcile(t, f, token)
	reconcile(t, f, token, node.Result{
		App: created.Reference, Deploy: answer.Desired.Apps[0].Deploy, Container: "container-on-eu-1",
	})

	answer = reconcile(t, f, token)
	if len(answer.Desired.Routes) != 1 {
		t.Fatalf("the edge was given %d routes: %+v", len(answer.Desired.Routes), answer.Desired.Routes)
	}
	if answer.Desired.Routes[0].Health != "/healthz" {
		t.Errorf("the route checks %q, so a replica that is up and broken keeps its share of the traffic",
			answer.Desired.Routes[0].Health)
	}
}
