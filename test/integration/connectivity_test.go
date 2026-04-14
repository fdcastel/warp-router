//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestBasicConnectivity provisions a router + client topology on an internal LAN,
// configures the router with warp apply, and verifies:
// 1. Client can ping router on LAN
// 2. Router has IP forwarding enabled
// 3. warp validate/apply works with a basic config
func TestBasicConnectivity(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: "local:vztmpl/warp-router-dev-lxc-amd64.tar.zst",
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()
	clientVMID := topo.ClientVMID()

	// Write a minimal warp site config to the router
	siteConfig := `---
hostname: warp-test-router

interfaces:
  - name: lan0
    role: lan
    device: eth0
    address: 10.99.0.1/24
  - name: wan0
    role: wan
    device: dummy0
    address: 192.0.2.1/24
    gateway: 192.0.2.254

dns:
  enabled: true
  forwarders:
    - 1.1.1.1
    - 8.8.8.8
`

	t.Run("WarpValidate", func(t *testing.T) {
		// Write config file
		err := topo.PVE.UploadFileToCT(routerVMID, "/etc/warp/site.yaml", siteConfig)
		if err != nil {
			t.Fatalf("uploading site config: %v", err)
		}

		out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp validate /etc/warp/site.yaml 2>&1")
		if err != nil {
			t.Fatalf("warp validate failed: %v\noutput: %s", err, out)
		}
		t.Logf("warp validate output: %s", out)
	})

	t.Run("WarpApply", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp apply /etc/warp/site.yaml 2>&1")
		if err != nil {
			// Apply may fail on FRR reload (dummy0 doesn't exist in container)
			// or conntrack_max (unprivileged LXC). Check partial success.
			if !strings.Contains(out, "Saved revision") {
				t.Fatalf("warp apply failed without saving revision: %v\noutput: %s", err, out)
			}
			t.Logf("warp apply partial failure (expected in LXC): %s", out)
		} else {
			t.Logf("warp apply output: %s", out)
		}
	})

	t.Run("IPForwardingEnabled", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "sysctl net.ipv4.ip_forward")
		if err != nil {
			t.Fatalf("checking sysctl: %v", err)
		}
		if !strings.Contains(out, "= 1") {
			t.Errorf("ip_forward not enabled: %s", out)
		}
	})

	t.Run("ClientPingsRouter", func(t *testing.T) {
		_, err := topo.PVE.ExecCT(clientVMID, "ping -c 3 -W 3 10.99.0.1")
		if err != nil {
			t.Fatal("client cannot ping router")
		}
	})

	t.Run("WarpStatus", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp status 2>&1")
		if err != nil {
			t.Fatalf("warp status failed: %v", err)
		}
		if strings.Contains(out, "No config") {
			t.Error("warp status shows no config after apply")
		}
		t.Logf("warp status: %s", out)
	})
}
