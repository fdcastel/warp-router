//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestTopologySmoke verifies the basic test infrastructure:
// 1. Connect to PVE host via SSH
// 2. Create an internal LAN bridge
// 3. Create a router CT and a client CT on the bridge
// 4. Verify router can ping client (and vice versa) via pct exec
// 5. Tear everything down
func TestTopologySmoke(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()
	clientVMID := topo.ClientVMID()

	// Verify router CT is running
	t.Run("RouterRunning", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "hostname")
		if err != nil {
			t.Fatalf("exec on router: %v", err)
		}
		hostname := strings.TrimSpace(out)
		if !strings.HasPrefix(hostname, "wt-router-") {
			t.Errorf("router hostname = %q, want wt-router-*", hostname)
		}
	})

	// Verify client CT is running
	t.Run("ClientRunning", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(clientVMID, "hostname")
		if err != nil {
			t.Fatalf("exec on client: %v", err)
		}
		hostname := strings.TrimSpace(out)
		if !strings.HasPrefix(hostname, "wt-client-") {
			t.Errorf("client hostname = %q, want wt-client-*", hostname)
		}
	})

	// Verify router can ping client
	t.Run("RouterPingsClient", func(t *testing.T) {
		_, err := topo.PVE.ExecCT(routerVMID, "ping -c 2 -W 3 10.99.0.2")
		if err != nil {
			t.Fatalf("router cannot ping client: %v", err)
		}
	})

	// Verify client can ping router
	t.Run("ClientPingsRouter", func(t *testing.T) {
		_, err := topo.PVE.ExecCT(clientVMID, "ping -c 2 -W 3 10.99.0.1")
		if err != nil {
			t.Fatalf("client cannot ping router: %v", err)
		}
	})
}
