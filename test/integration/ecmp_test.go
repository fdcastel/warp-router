//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestECMPDistribution provisions a router with two WAN uplinks via separate
// bridges and verifies that FRR installs ECMP routes correctly.
// This tests the routing configuration aspect — actual traffic distribution
// requires real forwarding which is limited in LXC.
func TestECMPDistribution(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: testenv.WarpRouterTemplate,
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()

	siteConfig := `---
hostname: warp-ecmp-test

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

	t.Run("FRRConfigHasECMP", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/frr/frr.conf 2>&1")
		if err != nil {
			t.Fatalf("reading FRR config: %v", err)
		}
		// FRR should have nexthops for both gateways
		if !strings.Contains(out, "198.51.100.254") {
			t.Errorf("FRR config missing WAN1 nexthop 198.51.100.254")
		}
		if !strings.Contains(out, "203.0.113.254") {
			t.Errorf("FRR config missing WAN2 nexthop 203.0.113.254")
		}
		t.Logf("FRR config:\n%s", out)
	})

	t.Run("FRRServiceRunning", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "systemctl is-active frr 2>&1")
		if err != nil || !strings.Contains(out, "active") {
			t.Errorf("FRR not active: %s (err: %v)", out, err)
		}
	})

	t.Run("RoutingTableHasECMP", func(t *testing.T) {
		// FRR installs routes via zebra. With dummy interfaces (no real carrier),
		// routes may or may not appear in the kernel table. Check FRR's own RIB.
		out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
		if err != nil {
			t.Fatalf("vtysh show ip route: %v\noutput: %s", err, out)
		}
		t.Logf("FRR routing table:\n%s", out)

		hasWAN1 := strings.Contains(out, "198.51.100.254")
		hasWAN2 := strings.Contains(out, "203.0.113.254")

		if !hasWAN1 || !hasWAN2 {
			t.Errorf("FRR RIB should have both ECMP nexthops (wan1=%v, wan2=%v)", hasWAN1, hasWAN2)
		}
	})

	t.Run("DualWANMasquerade", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list chain inet nat postrouting 2>&1")
		if err != nil {
			t.Fatalf("nft list: %v", err)
		}
		if !strings.Contains(out, `"dummy1"`) || !strings.Contains(out, "masquerade") {
			t.Errorf("missing masquerade for WAN1 (dummy1):\n%s", out)
		}
		if !strings.Contains(out, `"dummy2"`) || !strings.Contains(out, "masquerade") {
			t.Errorf("missing masquerade for WAN2 (dummy2):\n%s", out)
		}
	})

	t.Run("WarpStatusShowsConfig", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp status 2>&1")
		if err != nil {
			t.Fatalf("warp status: %v", err)
		}
		// Status should show a revision was applied
		if !strings.Contains(out, "Current revision") {
			t.Errorf("warp status should show current revision:\n%s", out)
		}
		t.Logf("status:\n%s", out)
	})
}
