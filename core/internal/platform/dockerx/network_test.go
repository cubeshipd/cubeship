package dockerx

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
)

type networkAPI struct {
	*fakeAPI
	attached map[string]*network.EndpointSettings
	ops      []string
}

func (f *networkAPI) ContainerInspect(context.Context, string) (types.ContainerJSON, error) {
	return types.ContainerJSON{NetworkSettings: &types.NetworkSettings{Networks: f.attached}}, nil
}
func (f *networkAPI) NetworkConnect(_ context.Context, name, _ string, _ *network.EndpointSettings) error {
	if f.networkConnectErr != nil {
		return f.networkConnectErr
	}
	f.ops = append(f.ops, "connect:"+name)
	f.attached[name] = &network.EndpointSettings{}
	return nil
}
func (f *networkAPI) NetworkDisconnect(_ context.Context, name, _ string, _ bool) error {
	f.ops = append(f.ops, "disconnect:"+name)
	delete(f.attached, name)
	return nil
}

func TestNetworkMigrationConnectsBeforeDisconnectingAndIsIdempotent(t *testing.T) {
	f := &networkAPI{fakeAPI: &fakeAPI{}, attached: map[string]*network.EndpointSettings{"old": {}}}
	c := newWithAPI(f)
	for range 2 {
		if err := c.ReconcileNetworks(context.Background(), "daemon", []string{"new"}, []string{"old"}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(f.ops, []string{"connect:new", "disconnect:old"}) {
		t.Fatal(f.ops)
	}
}

func TestNetworkMigrationKeepsOldConnectionWhenNewNetworkFails(t *testing.T) {
	f := &networkAPI{fakeAPI: &fakeAPI{networkConnectErr: errors.New("network unavailable")}, attached: map[string]*network.EndpointSettings{"old": {}}}
	err := newWithAPI(f).ReconcileNetworks(context.Background(), "daemon", []string{"new"}, []string{"old"})
	if err == nil || f.attached["old"] == nil {
		t.Fatalf("error=%v networks=%v", err, f.attached)
	}
}

func TestAppContainersCannotUseRawSocketsOrGainPrivileges(t *testing.T) {
	f := &fakeAPI{}
	_, err := newWithAPI(f).CreateContainer(context.Background(), ContainerOpts{
		Image: "app:test", CapDrop: []string{"NET_RAW"}, SecurityOpt: []string{"no-new-privileges:true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]string(f.createdHostConfig.CapDrop), []string{"NET_RAW"}) || !reflect.DeepEqual(f.createdHostConfig.SecurityOpt, []string{"no-new-privileges:true"}) {
		t.Fatal("container security policy was not handed to Docker")
	}
}
