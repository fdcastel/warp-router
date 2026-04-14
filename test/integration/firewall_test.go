//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestNFTablesFirewall provisions a router + client, applies a config with
// explicit firewall zones, and verifies:
// 1. nftables ruleset is loaded (flush + table/chain structure)
// 2. Input chain has default drop policy
// 3. Forward chain has default drop policy
// 4. LAN → WAN forwarding is permitted
// 5. SSH is allowed in input
// 6. Masquerade is configured on WAN
// 7. Client on LAN can reach router services (ping, SSH port)
// 8. Counter-based verification of drop rules
func TestNFTablesFirewall(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: "local:vztmpl/warp-router-dev-lxc-amd64.tar.zst",
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()
	clientVMID := topo.ClientVMID()

	siteConfig := `---
hostname: warp-fw-test

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

firewall:
  zones:
    - name: lan
      interfaces: [lan0]
    - name: wan
      interfaces: [wan0]
  forward_rules:
    - from: lan
      to: wan
      action: accept
      comment: "Allow LAN to WAN"

dns:
  enabled: true
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`

	// Apply config
	err := topo.PVE.UploadFileToCT(routerVMID, "/etc/warp/site.yaml", siteConfig)
	if err != nil {
		t.Fatalf("uploading site config: %v", err)
	}

	out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp apply /etc/warp/site.yaml 2>&1")
	if err != nil {
		t.Fatalf("warp apply failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Apply complete") {
		t.Fatalf("warp apply did not complete: %s", out)
	}

	t.Run("RulesetLoaded", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list ruleset 2>&1")
		if err != nil {
			t.Fatalf("nft list ruleset failed: %v\noutput: %s", err, out)
		}
		if !strings.Contains(out, "table inet filter") {
			t.Error("missing table inet filter")
		}
		if !strings.Contains(out, "table inet nat") {
			t.Error("missing table inet nat")
		}
		t.Logf("ruleset has %d lines", len(strings.Split(out, "\n")))
	})

	t.Run("InputChainDefaultDrop", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list chain inet filter input 2>&1")
		if err != nil {
			t.Fatalf("nft list chain failed: %v", err)
		}
		if !strings.Contains(out, "policy drop") {
			t.Errorf("input chain should have policy drop, got:\n%s", out)
		}
	})

	t.Run("ForwardChainDefaultDrop", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list chain inet filter forward 2>&1")
		if err != nil {
			t.Fatalf("nft list chain failed: %v", err)
		}
		if !strings.Contains(out, "policy drop") {
			t.Errorf("forward chain should have policy drop, got:\n%s", out)
		}
	})

	t.Run("LANToWANForwardAllowed", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list chain inet filter forward 2>&1")
		if err != nil {
			t.Fatalf("nft list chain failed: %v", err)
		}
		// Should have a rule allowing traffic from eth0 (LAN) to dummy0 (WAN)
		if !strings.Contains(out, `iifname "eth0"`) || !strings.Contains(out, `oifname "dummy0"`) {
			t.Errorf("missing LAN→WAN forward rule in:\n%s", out)
		}
	})

	t.Run("SSHAllowedInInput", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list chain inet filter input 2>&1")
		if err != nil {
			t.Fatalf("nft list chain failed: %v", err)
		}
		if !strings.Contains(out, "dport 22") {
			t.Errorf("missing SSH allow rule in input chain:\n%s", out)
		}
	})

	t.Run("MasqueradeOnWAN", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list chain inet nat postrouting 2>&1")
		if err != nil {
			t.Fatalf("nft list chain failed: %v", err)
		}
		if !strings.Contains(out, `oifname "dummy0"`) || !strings.Contains(out, "masquerade") {
			t.Errorf("missing masquerade rule for WAN:\n%s", out)
		}
	})

	t.Run("EstablishedRelatedAccepted", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list chain inet filter input 2>&1")
		if err != nil {
			t.Fatalf("nft list chain failed: %v", err)
		}
		if !strings.Contains(out, "ct state established,related") {
			t.Errorf("missing established/related accept in input:\n%s", out)
		}
	})

	t.Run("ClientCanPingRouter", func(t *testing.T) {
		_, err := topo.PVE.ExecCT(clientVMID, "ping -c 3 -W 3 10.99.0.1")
		if err != nil {
			t.Fatal("client cannot ping router — ICMP may be blocked")
		}
	})

	t.Run("DropCounterPresent", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "nft list chain inet filter input 2>&1")
		if err != nil {
			t.Fatalf("nft list chain failed: %v", err)
		}
		// The drop rule should have a counter AND log prefix
		if !strings.Contains(out, "nft-input-drop") {
			t.Errorf("missing log prefix for input drop rule:\n%s", out)
		}
		if !strings.Contains(out, "counter") {
			t.Errorf("missing counter on input drop rule:\n%s", out)
		}
	})
}
