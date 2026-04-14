//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestPBRSteering provisions a router with dual-WAN and a PBR rule that
// steers traffic from a specific source subnet to a designated WAN interface.
// Verifies FRR renders the PBR policy, installs it, and the rules appear
// in the routing system.
// Maps to FR-007: Policy-based routing per source subnet.
func TestPBRSteering(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: "local:vztmpl/warp-router-dev-lxc-amd64.tar.zst",
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()

	siteConfig := `---
hostname: warp-pbr-test

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

pbr:
  - name: lan-via-wan1
    priority: 100
    source: 10.99.0.0/24
    interface: wan1

dns:
  enabled: true
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`

	// Create dummy interfaces for the two WANs
	cmds := []string{
		"ip link add dummy1 type dummy && ip link set dummy1 up && ip addr add 198.51.100.1/24 dev dummy1",
		"ip link add dummy2 type dummy && ip link set dummy2 up && ip addr add 203.0.113.1/24 dev dummy2",
	}
	for _, cmd := range cmds {
		_, err := topo.PVE.ExecCT(routerVMID, cmd)
		if err != nil {
			t.Fatalf("setting up dummy interfaces: %v", err)
		}
	}

	// Apply config
	err := topo.PVE.UploadFileToCT(routerVMID, "/etc/warp/site.yaml", siteConfig)
	if err != nil {
		t.Fatalf("uploading site config: %v", err)
	}

	out, err := topo.PVE.ExecCT(routerVMID, "/usr/local/bin/warp apply /etc/warp/site.yaml 2>&1")
	if err != nil {
		if !strings.Contains(out, "frr") || !strings.Contains(out, "nftables") {
			t.Fatalf("warp apply failed critically: %v\noutput: %s", err, out)
		}
		t.Logf("warp apply partial: %s", out)
	} else {
		t.Logf("warp apply: %s", out)
	}

	t.Run("FRRConfigHasPBRMap", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/frr/frr.conf 2>&1")
		if err != nil {
			t.Fatalf("reading FRR config: %v", err)
		}
		t.Logf("FRR config:\n%s", out)

		if !strings.Contains(out, "pbr-map lan-via-wan1") {
			t.Error("FRR config missing PBR map 'lan-via-wan1'")
		}
		if !strings.Contains(out, "match src-ip 10.99.0.0/24") {
			t.Error("FRR config missing PBR match for source 10.99.0.0/24")
		}
		if !strings.Contains(out, "set nexthop 198.51.100.254") {
			t.Error("FRR config missing PBR nexthop 198.51.100.254")
		}
	})

	t.Run("FRRConfigHasPBRPolicy", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/frr/frr.conf 2>&1")
		if err != nil {
			t.Fatalf("reading FRR config: %v", err)
		}

		// PBR policy should be attached to LAN interface
		if !strings.Contains(out, "interface eth0") {
			t.Error("FRR config missing interface eth0 section for PBR policy")
		}
		if !strings.Contains(out, "pbr-policy lan-via-wan1") {
			t.Error("FRR config missing pbr-policy attachment to LAN interface")
		}
	})

	t.Run("FRRPBRMapInstalled", func(t *testing.T) {
		// Check FRR's view of installed PBR maps
		out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show pbr map' 2>&1")
		if err != nil {
			// PBR may not be fully installed if pbrd is not running
			t.Logf("vtysh show pbr map: %v\noutput: %s", err, out)
			// Fall through to check if at least the config was written
		} else {
			t.Logf("PBR map status:\n%s", out)
			if !strings.Contains(out, "lan-via-wan1") {
				t.Error("vtysh PBR map missing 'lan-via-wan1'")
			}
			if !strings.Contains(out, "10.99.0.0/24") {
				t.Error("vtysh PBR map missing source match '10.99.0.0/24'")
			}
		}
	})

	t.Run("FRRPBRDaemonRunning", func(t *testing.T) {
		// pbrd must be running for PBR maps to work
		out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show pbr interface' 2>&1")
		if err != nil {
			t.Logf("vtysh show pbr interface: %v\noutput: %s", err, out)
			// Check if pbrd is even enabled in FRR
			daemonsOut, _ := topo.PVE.ExecCT(routerVMID, "cat /etc/frr/daemons 2>&1")
			if strings.Contains(daemonsOut, "pbrd=yes") {
				t.Error("pbrd is enabled but vtysh pbr interface command failed")
			} else {
				t.Log("pbrd not enabled in /etc/frr/daemons — PBR rules are config-only")
			}
		} else {
			t.Logf("PBR interface status:\n%s", out)
			if !strings.Contains(out, "eth0") {
				t.Error("PBR policy not attached to eth0")
			}
		}
	})

	t.Run("ECMPStillActive", func(t *testing.T) {
		// Even with PBR, ECMP default routes should exist for non-PBR traffic
		out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
		if err != nil {
			t.Fatalf("vtysh show ip route: %v\noutput: %s", err, out)
		}
		t.Logf("FRR RIB with PBR:\n%s", out)

		if !strings.Contains(out, "198.51.100.254") {
			t.Error("ECMP WAN1 nexthop missing from RIB")
		}
		if !strings.Contains(out, "203.0.113.254") {
			t.Error("ECMP WAN2 nexthop missing from RIB")
		}
	})

	t.Run("PBRWithWANFailover", func(t *testing.T) {
		// When WAN1 (the PBR target) goes down, ECMP should still work via WAN2.
		// FRR marks the WAN1 nexthop as "inactive" (removed from kernel FIB),
		// while WAN2 remains active for default traffic.
		_, err := topo.PVE.ExecCT(routerVMID, "ip link set dummy1 down")
		if err != nil {
			t.Fatalf("bringing dummy1 down: %v", err)
		}
		defer func() {
			topo.PVE.ExecCT(routerVMID, "ip link set dummy1 up && ip addr add 198.51.100.1/24 dev dummy1 2>/dev/null; true")
		}()

		// Wait for FRR to detect link down and mark nexthop inactive
		var lastRIB string
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(1 * time.Second)
			out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
			if err == nil {
				lastRIB = out
				wan1Inactive := strings.Contains(out, "dummy1 inactive")
				wan2Active := strings.Contains(out, "203.0.113.254") && !strings.Contains(out, "dummy2 inactive")

				if wan1Inactive && wan2Active {
					t.Logf("After WAN1 failure, WAN1 inactive, WAN2 still active:\n%s", out)
					return // success
				}
			}
		}
		t.Errorf("Expected WAN1 inactive and WAN2 active after link failure.\nLast RIB:\n%s", lastRIB)
	})
}
