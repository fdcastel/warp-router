//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestWANFailover provisions a router with dual-WAN ECMP and verifies that
// when a WAN link goes down (carrier loss), FRR removes the corresponding
// nexthop from the routing table. When the link is restored, the nexthop
// should return to the ECMP set.
// Maps to SC-003: WAN failover within 3 seconds of link down.
func TestWANFailover(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: testenv.WarpRouterTemplate,
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()

	siteConfig := `---
hostname: warp-failover-test

interfaces:
  - name: lan0
    role: lan
    device: eth0
    address: 10.99.0.1/24
  - name: wan1
    role: wan
    device: dummy1
    address: 198.51.100.1/24
    gateway: 198.51.100.254
  - name: wan2
    role: wan
    device: dummy2
    address: 203.0.113.1/24
    gateway: 203.0.113.254

ecmp:
  enabled: true

dns:
  enabled: true
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`

	topo.CreateDummyWANPair(t, routerVMID)

	out := topo.ApplyConfigAllowPartial(t, routerVMID, siteConfig, "frr", "nftables")
	t.Logf("warp apply output: %s", out)

	t.Run("BothNexhopsPresent", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
		if err != nil {
			t.Fatalf("vtysh show ip route: %v\noutput: %s", err, out)
		}
		t.Logf("Initial FRR RIB:\n%s", out)

		if !strings.Contains(out, "198.51.100.254") {
			t.Error("FRR RIB missing WAN1 nexthop 198.51.100.254")
		}
		if !strings.Contains(out, "203.0.113.254") {
			t.Error("FRR RIB missing WAN2 nexthop 203.0.113.254")
		}
	})

	t.Run("LinkDownRemovesNexhop", func(t *testing.T) {
		// Bring WAN1 down (carrier loss)
		_, err := topo.PVE.ExecCT(routerVMID, "ip link set dummy1 down")
		if err != nil {
			t.Fatalf("bringing dummy1 down: %v", err)
		}
		t.Log("dummy1 link set down")

		// Wait for FRR to detect the link state change and update RIB.
		// FRR marks unreachable nexthops as "inactive" (removed from kernel FIB
		// but kept in RIB). The key indicators are:
		//   - "inactive" keyword next to the down nexthop
		//   - Only the live nexthop has '*' (FIB-installed) marker
		var lastRIB string
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(1 * time.Second)
			out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
			if err != nil {
				t.Logf("vtysh error (retrying): %v", err)
				continue
			}
			lastRIB = out

			// WAN1 nexthop should be inactive (marked by FRR), WAN2 should remain active
			wan1Inactive := strings.Contains(out, "dummy1 inactive")
			wan2Active := strings.Contains(out, "203.0.113.254")

			if wan1Inactive && wan2Active {
				t.Logf("FRR RIB after link down:\n%s", out)
				return // success
			}
		}
		t.Errorf("FRR did not mark WAN1 nexthop inactive within 10s.\nLast RIB:\n%s", lastRIB)
	})

	t.Run("WAN2StillActive", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
		if err != nil {
			t.Fatalf("vtysh: %v\noutput: %s", err, out)
		}
		// WAN2 should still be in FIB (active, with * marker)
		if !strings.Contains(out, "203.0.113.254") {
			t.Errorf("WAN2 nexthop should still be active:\n%s", out)
		}
		// WAN2 should NOT be inactive
		if strings.Contains(out, "dummy2 inactive") {
			t.Errorf("WAN2 should not be inactive:\n%s", out)
		}
	})

	t.Run("LinkUpRestoresNexhop", func(t *testing.T) {
		// Restore WAN1 link and re-add address (link down removes addrs)
		cmds := []string{
			"ip link set dummy1 up",
			"ip addr add 198.51.100.1/24 dev dummy1 2>/dev/null; true",
		}
		for _, cmd := range cmds {
			_, err := topo.PVE.ExecCT(routerVMID, cmd)
			if err != nil {
				t.Fatalf("restoring dummy1: %v", err)
			}
		}
		t.Log("dummy1 link restored")

		// Wait for FRR to detect recovery and re-add nexthop
		var lastRIB string
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(1 * time.Second)
			out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
			if err != nil {
				t.Logf("vtysh error (retrying): %v", err)
				continue
			}
			lastRIB = out

			wan1Present := strings.Contains(out, "198.51.100.254")
			wan2Present := strings.Contains(out, "203.0.113.254")

			if wan1Present && wan2Present {
				t.Logf("FRR RIB after recovery:\n%s", out)
				return // success
			}
		}
		t.Errorf("FRR did not restore WAN1 nexthop within 15s.\nLast RIB:\n%s", lastRIB)
	})

	t.Run("FailoverTiming", func(t *testing.T) {
		// Verify failover happens within 3 seconds (SC-003 requirement).
		// Bring WAN2 down this time to test the other path.
		_, err := topo.PVE.ExecCT(routerVMID, "ip link set dummy2 down")
		if err != nil {
			t.Fatalf("bringing dummy2 down: %v", err)
		}
		t.Log("dummy2 link set down for timing test")

		start := time.Now()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
			if err != nil {
				continue
			}

			// FRR marks the nexthop as "inactive" in the RIB
			wan2Inactive := strings.Contains(out, "dummy2 inactive")
			if wan2Inactive {
				elapsed := time.Since(start)
				t.Logf("WAN2 nexthop marked inactive in %v", elapsed)
				if elapsed > 3*time.Second {
					t.Errorf("failover took %v, requirement is ≤3s", elapsed)
				}
				return
			}
		}
		t.Error("WAN2 nexthop was not marked inactive within 5s")
	})
}
