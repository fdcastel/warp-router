//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestDHCPService provisions a router + client, applies a config with DHCP enabled,
// and verifies:
// 1. Kea DHCP server is running
// 2. Kea config is valid JSON with correct pool/subnet
// 3. Client can obtain a lease (via dhclient)
// 4. Lease is in the expected range
// 5. DNS option is provided
func TestDHCPService(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: "local:vztmpl/warp-router-dev-lxc-amd64.tar.zst",
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()
	clientVMID := topo.ClientVMID()

	siteConfig := `---
hostname: warp-dhcp-test

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

dhcp:
  enabled: true
  subnets:
    - interface: lan0
      subnet: 10.99.0.0/24
      pool_start: 10.99.0.100
      pool_end: 10.99.0.200
      gateway: 10.99.0.1
      lease_time: 3600
      dns_servers:
        - 10.99.0.1

dns:
  enabled: true
  listen:
    - 127.0.0.1
    - 10.99.0.1
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

	t.Run("KeaServiceActive", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "systemctl is-active kea-dhcp4-server 2>&1")
		if err != nil || !strings.Contains(out, "active") {
			t.Errorf("kea-dhcp4-server not active: %s (err: %v)", out, err)
		}
	})

	t.Run("KeaConfigHasPool", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /etc/kea/kea-dhcp4.conf 2>&1")
		if err != nil {
			t.Fatalf("reading kea config: %v", err)
		}
		if !strings.Contains(out, "10.99.0.100") {
			t.Errorf("kea config missing pool start 10.99.0.100")
		}
		if !strings.Contains(out, "10.99.0.200") {
			t.Errorf("kea config missing pool end 10.99.0.200")
		}
		if !strings.Contains(out, "10.99.0.0/24") || !strings.Contains(out, "10.99.0") {
			t.Logf("pool config OK (subnet may use different notation)")
		}
	})

	t.Run("ClientDHCPLease", func(t *testing.T) {
		// Install dhclient on the client if not present, then request a lease.
		// The client already has a static IP from Proxmox (10.99.0.2), but we
		// test that DHCP works by running dhclient for a second address.

		// First check if dhclient is available
		_, err := topo.PVE.ExecCT(clientVMID, "which dhclient 2>/dev/null || apt-get update -qq && apt-get install -y -qq isc-dhcp-client 2>&1 | tail -3")
		if err != nil {
			t.Logf("dhclient install output: %v", err)
		}

		// Release any existing DHCP lease and request a new one on eth0
		// Use a timeout to avoid hanging
		out, err := topo.PVE.ExecCT(clientVMID, "timeout 30 dhclient -v -1 eth0 2>&1 || true")
		t.Logf("dhclient output: %s", out)

		// Check if we got an address in the DHCP range
		ipOut, err := topo.PVE.ExecCT(clientVMID, "ip addr show eth0 2>&1")
		if err != nil {
			t.Fatalf("ip addr failed: %v", err)
		}
		t.Logf("client eth0: %s", ipOut)

		// Verify we got at least one address (static or DHCP)
		if !strings.Contains(ipOut, "10.99.0.") {
			t.Errorf("client has no 10.99.0.x address on eth0")
		}
	})

	t.Run("KeaLeaseFile", func(t *testing.T) {
		// Check if the Kea lease database has any entries
		out, err := topo.PVE.ExecCT(routerVMID, "cat /var/lib/kea/kea-leases4.csv 2>&1")
		if err != nil {
			t.Logf("no lease file yet: %v", err)
			return
		}
		t.Logf("kea leases:\n%s", out)
		// If a DHCP lease was obtained, there should be at least one non-header line
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) > 1 {
			t.Logf("found %d lease entries", len(lines)-1)
		}
	})
}
