//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestServiceHealth verifies that 'warp status' reports correct state
// after applying a config with all services.
func TestServiceHealth(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: testenv.WarpRouterTemplate,
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()
	topo.RunCTCommands(t, routerVMID, "ip link add dummy0 type dummy && ip link set dummy0 up")

	siteConfig := `---
hostname: warp-health

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
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`

	topo.ApplyConfig(t, routerVMID, siteConfig)

	t.Run("WarpStatusShowsRevision", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp status 2>&1")
		if err != nil {
			t.Fatalf("warp status failed: %v", err)
		}
		if !strings.Contains(out, "Current revision:") {
			t.Errorf("expected revision info in status: %s", out)
		}
		if strings.Contains(out, "No config") {
			t.Error("status shows 'No config' after successful apply")
		}
	})

	t.Run("ServicesRunningAfterApply", func(t *testing.T) {
		services := []string{"frr", "nftables", "ssh", "kea-dhcp4-server", "unbound"}
		for _, svc := range services {
			out, err := topo.PVE.ExecCT(routerVMID, "systemctl is-active "+svc)
			if err != nil || strings.TrimSpace(out) != "active" {
				t.Errorf("service %s: got %q, want active", svc, strings.TrimSpace(out))
			}
		}
	})

	t.Run("FRRConfigApplied", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/frr/frr.conf")
		if err != nil {
			t.Fatalf("reading frr.conf: %v", err)
		}
		if !strings.Contains(out, "hostname warp-health") {
			t.Error("frr.conf missing hostname")
		}
		if !strings.Contains(out, "ip route 0.0.0.0/0 192.0.2.254") {
			t.Errorf("frr.conf missing default route, got:\n%s", out)
		}
	})

	t.Run("NFTablesConfigApplied", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/nftables.conf")
		if err != nil {
			t.Fatalf("reading nftables.conf: %v", err)
		}
		if !strings.Contains(out, "masquerade") {
			t.Errorf("nftables.conf missing masquerade rule")
		}
	})

	t.Run("IPForwardingEnabled", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "sysctl -n net.ipv4.ip_forward")
		if err != nil {
			t.Fatalf("checking sysctl: %v", err)
		}
		if strings.TrimSpace(out) != "1" {
			t.Errorf("ip_forward = %q, want 1", strings.TrimSpace(out))
		}
	})
}

// TestConfigRollback verifies that 'warp rollback' restores the previous config.
func TestConfigRollback(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: testenv.WarpRouterTemplate,
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()
	topo.RunCTCommands(t, routerVMID, "ip link add dummy0 type dummy && ip link set dummy0 up")

	config1 := `---
hostname: warp-rollback-v1

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
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`

	config2 := `---
hostname: warp-rollback-v2

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
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`

	topo.ApplyConfig(t, routerVMID, config1)
	topo.ApplyConfig(t, routerVMID, config2)

	t.Run("SecondConfigApplied", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/frr/frr.conf")
		if err != nil {
			t.Fatalf("reading frr.conf: %v", err)
		}
		if !strings.Contains(out, "hostname warp-rollback-v2") {
			t.Error("frr.conf should show v2 hostname")
		}
	})

	t.Run("Rollback", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp rollback 2>&1")
		if err != nil {
			t.Fatalf("rollback failed: %v\n%s", err, out)
		}
	})

	t.Run("RollbackRestoredV1", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/frr/frr.conf")
		if err != nil {
			t.Fatalf("reading frr.conf: %v", err)
		}
		if !strings.Contains(out, "hostname warp-rollback-v1") {
			t.Errorf("frr.conf should show v1 hostname after rollback, got:\n%s", out)
		}
	})

	t.Run("RevisionsShowHistory", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp revisions 2>&1")
		if err != nil {
			t.Fatalf("revisions failed: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		revCount := 0
		for _, line := range lines {
			if strings.Contains(line, "20") {
				revCount++
			}
		}
		if revCount < 3 {
			t.Errorf("expected at least 3 revisions, found %d in:\n%s", revCount, out)
		}
	})
}
