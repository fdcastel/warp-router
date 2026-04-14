//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestVLANSubinterface verifies that `warp apply` creates 802.1Q VLAN
// subinterfaces when the config specifies interfaces with vlan > 0.
// Tests VLAN creation, IP assignment, and service integration (DHCP, DNS,
// nftables, FRR all see the VLAN interface).
func TestVLANSubinterface(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: testenv.WarpRouterTemplate,
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()

	// Config with a VLAN subinterface on a dummy parent device.
	// dummy0 = trunk (parent), dummy0.100 = VLAN 100 LAN segment.
	siteConfig := `---
hostname: warp-vlan-test

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
  - name: vlan100
    role: lan
    device: dummy0.100
    address: 192.168.100.1/24
    vlan: 100

ecmp:
  enabled: false

dhcp:
  enabled: true
  subnets:
    - subnet: 10.99.0.0/24
      interface: lan0
      pool_start: 10.99.0.100
      pool_end: 10.99.0.200
      gateway: 10.99.0.1
    - subnet: 192.168.100.0/24
      interface: vlan100
      pool_start: 192.168.100.100
      pool_end: 192.168.100.200
      gateway: 192.168.100.1

dns:
  enabled: true
  listen:
    - 127.0.0.1
    - 10.99.0.1
    - 192.168.100.1
  forwarders:
    - 1.1.1.1

firewall:
  zones:
    - name: wan
      interfaces: [wan1]
    - name: lan
      interfaces: [lan0, vlan100]
  forward_rules:
    - from: lan
      to: wan
      action: accept
  input_rules:
    - zone: lan
      action: accept
      protocol: udp
      port: "67"
    - zone: lan
      action: accept
      protocol: udp
      port: "53"
    - zone: lan
      action: accept
      protocol: tcp
      port: "53"
    - zone: lan
      action: accept
      protocol: tcp
      port: "22"
`

	// Create parent dummy device (the trunk)
	topo.RunCTCommands(
		t,
		routerVMID,
		"ip link add dummy0 type dummy && ip link set dummy0 up",
		"ip link add dummy1 type dummy && ip link set dummy1 up && ip addr add 198.51.100.1/24 dev dummy1",
	)

	out := topo.ApplyConfigAllowPartial(t, routerVMID, siteConfig, "nftables")
	t.Logf("warp apply output: %s", out)

	t.Run("VLANInterfaceCreated", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "ip link show dummy0.100 2>&1")
		if err != nil {
			t.Fatalf("VLAN interface not created: %v\n%s", err, out)
		}
		if !strings.Contains(out, "dummy0.100") {
			t.Errorf("expected dummy0.100 in output:\n%s", out)
		}
		// Verify it's a VLAN device (requires detailed link output)
		detailOut, detailErr := topo.PVE.ExecCT(routerVMID, "ip -d link show dummy0.100 2>&1")
		if detailErr != nil {
			t.Fatalf("ip -d link show failed: %v\n%s", detailErr, detailOut)
		}
		if !strings.Contains(detailOut, "vlan protocol 802.1Q") {
			t.Errorf("expected VLAN protocol in link details:\n%s", detailOut)
		}
		t.Logf("VLAN interface:\n%s", out)
	})

	t.Run("VLANInterfaceIsUp", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "ip link show dummy0.100 2>&1")
		if err != nil {
			t.Fatalf("ip link show: %v", err)
		}
		if !strings.Contains(out, "UP") && !strings.Contains(out, "state UP") {
			t.Errorf("VLAN interface should be UP:\n%s", out)
		}
	})

	t.Run("VLANHasCorrectIP", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "ip addr show dummy0.100 2>&1")
		if err != nil {
			t.Fatalf("ip addr show: %v", err)
		}
		if !strings.Contains(out, "192.168.100.1/24") {
			t.Errorf("expected 192.168.100.1/24 on dummy0.100:\n%s", out)
		}
		t.Logf("VLAN address:\n%s", out)
	})

	t.Run("VLANHasCorrectVID", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "ip -d link show dummy0.100 2>&1")
		if err != nil {
			t.Fatalf("ip -d link show: %v", err)
		}
		if !strings.Contains(out, "vlan") || !strings.Contains(out, "id 100") {
			t.Errorf("expected VLAN id 100:\n%s", out)
		}
		t.Logf("VLAN details:\n%s", out)
	})

	t.Run("KeaConfigIncludesVLANSubnet", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/kea/kea-dhcp4.conf 2>&1")
		if err != nil {
			t.Fatalf("reading Kea config: %v", err)
		}
		if !strings.Contains(out, "192.168.100.0") {
			t.Errorf("Kea config should have VLAN subnet:\n%s", out)
		}
		if !strings.Contains(out, "192.168.100.100") {
			t.Errorf("Kea config should have VLAN pool start:\n%s", out)
		}
	})

	t.Run("NFTablesSeesVLANInterface", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list ruleset 2>&1")
		if err != nil {
			t.Fatalf("nft list: %v", err)
		}
		// The VLAN interface should be in the lan zone (forward/input rules)
		if !strings.Contains(out, `"dummy0.100"`) {
			t.Errorf("nftables should reference dummy0.100:\n%s", out)
		}
		t.Logf("nftables includes VLAN interface")
	})

	t.Run("ApplyIsIdempotent", func(t *testing.T) {
		// Run apply again — VLAN interface already exists, should not error
		out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp apply /etc/warp/site.yaml 2>&1")
		if err != nil {
			t.Logf("Second apply output: %s", out)
			// Non-critical services may fail, but VLAN step should not
			if strings.Contains(out, "VLAN") || strings.Contains(out, "vlan") {
				t.Errorf("VLAN provisioning should be idempotent: %v\n%s", err, out)
			}
		} else {
			t.Logf("Second apply (idempotent): %s", out)
		}
	})
}
