package bootstrap

import (
	"slices"
	"testing"

	"cubeship/internal/platform/dockerx"
)

func TestAdministrativeContainersDoNotJoinApplicationBridge(t *testing.T) {
	cfg := testConfig()
	cfg.InContainer = true
	for _, opts := range []dockerx.ContainerOpts{
		PostgresContainerOpts(cfg, "password"),
		RegistryContainerOpts(cfg, "registry.example.com", true, []byte("cert")),
		BuildKitContainerOpts(cfg),
		FrontendContainerOpts("frontend:test"),
	} {
		if opts.Network == "cubeship" || slices.Contains(opts.AlsoNetworks, "cubeship") {
			t.Errorf("%s exposes administrative traffic on the application bridge", opts.Name)
		}
	}
	proxy := TraefikContainerOpts(cfg, true, "", nil)
	if proxy.Network == "cubeship" || !slices.Contains(proxy.AlsoNetworks, "cubeship") {
		t.Error("ingress must bridge the private control network and application network")
	}
}
