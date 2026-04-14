//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestWANProbeFailover provisions a router with dual-WAN ECMP and runs
// `warp monitor` to detect when a gateway becomes unreachable via ICMP
// probes (even though the link stays up). When the probe declares the
// WAN down, the monitor removes the static route from FRR via vtysh.
// When the gateway becomes reachable again, the route is restored.
// Maps to SC-003: WAN failover via probe-based detection.
func TestWANProbeFailover(t *testing.T) {
	topo := testenv.NewTopology(t, testenv.TopologySpec{
		RouterTemplate: "local:vztmpl/warp-router-dev-lxc-amd64.tar.zst",
	})
	topo.Setup(t)

	routerVMID := topo.RouterVMID()

	// Config with health checks: 1s interval, 1s timeout, 3 failures → 3s to detect
	siteConfig := `---
hostname: warp-probe-test

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
    health_check:
      target: 198.51.100.254
      interval: 1
      timeout: 1
      failures: 3
  - name: wan2
    role: wan
    device: dummy2
    address: 203.0.113.1/24
    gateway: 203.0.113.254
    health_check:
      target: 203.0.113.254
      interval: 1
      timeout: 1
      failures: 3

ecmp:
  enabled: true

dns:
  enabled: true
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`

	// Create dummy interfaces and add "gateway" addresses that respond to ping.
	// The gateway is just a secondary local address on the dummy interface.
	cmds := []string{
		"ip link add dummy1 type dummy && ip link set dummy1 up",
		"ip addr add 198.51.100.1/24 dev dummy1",
		"ip addr add 198.51.100.254/24 dev dummy1",
		"ip link add dummy2 type dummy && ip link set dummy2 up",
		"ip addr add 203.0.113.1/24 dev dummy2",
		"ip addr add 203.0.113.254/24 dev dummy2",
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

	// Verify both nexthops present before starting monitor
	out, err = topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
	if err != nil {
		t.Fatalf("vtysh: %v\n%s", err, out)
	}
	if !strings.Contains(out, "198.51.100.254") || !strings.Contains(out, "203.0.113.254") {
		t.Fatalf("ECMP routes not present before monitor starts:\n%s", out)
	}
	t.Logf("Initial RIB:\n%s", out)

	// Start warp monitor in background
	_, err = topo.PVE.ExecCT(routerVMID,
		"nohup /usr/local/bin/warp monitor /etc/warp/site.yaml > /tmp/monitor.log 2>&1 &")
	if err != nil {
		t.Fatalf("starting monitor: %v", err)
	}

	// Wait for monitor to start and probes to stabilize (healthy)
	time.Sleep(5 * time.Second)

	t.Run("MonitorRunning", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "pgrep -f 'warp monitor' && echo running")
		if err != nil || !strings.Contains(out, "running") {
			monitorLog, _ := topo.PVE.ExecCT(routerVMID, "cat /tmp/monitor.log 2>&1")
			t.Fatalf("warp monitor not running: %v\noutput: %s\nmonitor log: %s", err, out, monitorLog)
		}
	})

	t.Run("HealthStatusFileExists", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "cat /run/warp/health.json 2>&1")
		if err != nil {
			t.Fatalf("health.json missing: %v\n%s", err, out)
		}
		t.Logf("Health status:\n%s", out)
		if !strings.Contains(out, "wan1") || !strings.Contains(out, "wan2") {
			t.Error("health.json should contain wan1 and wan2")
		}
		if !strings.Contains(out, `"healthy"`) {
			t.Error("expected at least one uplink to be healthy")
		}
	})

	t.Run("BothRoutesPresent", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
		if err != nil {
			t.Fatalf("vtysh: %v\n%s", err, out)
		}
		if !strings.Contains(out, "198.51.100.254") {
			t.Error("WAN1 route should be present")
		}
		if !strings.Contains(out, "203.0.113.254") {
			t.Error("WAN2 route should be present")
		}
	})

	t.Run("ProbeFailureRemovesRoute", func(t *testing.T) {
		// Remove the "gateway" IP from dummy1 — probe will fail but link stays up
		_, err := topo.PVE.ExecCT(routerVMID, "ip addr del 198.51.100.254/24 dev dummy1")
		if err != nil {
			t.Fatalf("removing gateway IP: %v", err)
		}
		t.Log("Removed 198.51.100.254 from dummy1 (link still up)")

		// Wait for probe to detect failure: 3 failures × 1s interval + margin
		var lastRIB string
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(1 * time.Second)
			out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
			if err != nil {
				continue
			}
			lastRIB = out

			// The monitor should have removed the WAN1 static route from FRR.
			// Check that 198.51.100.254 is no longer in the default route,
			// but 203.0.113.254 remains.
			wan1InDefault := false
			wan2InDefault := false
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "0.0.0.0/0") || (strings.HasPrefix(strings.TrimSpace(line), "*") || strings.HasPrefix(strings.TrimSpace(line), ">")) {
					if strings.Contains(line, "198.51.100.254") {
						// Check it's not inactive (link is still up, so FRR won't mark inactive)
						wan1InDefault = true
					}
					if strings.Contains(line, "203.0.113.254") {
						wan2InDefault = true
					}
				}
			}

			// Simpler check: just look for the gateway IP in the full output
			// after the route was removed by vtysh
			hasWan1Route := strings.Contains(out, "198.51.100.254") &&
				strings.Contains(out, "0.0.0.0/0")
			_ = wan1InDefault
			_ = wan2InDefault
			hasWan2Route := strings.Contains(out, "203.0.113.254")

			if !hasWan1Route && hasWan2Route {
				t.Logf("Monitor removed WAN1 route after probe failure:\n%s", out)

				// Check health file shows wan1 as down
				healthOut, _ := topo.PVE.ExecCT(routerVMID, "cat /run/warp/health.json 2>&1")
				if strings.Contains(healthOut, `"down"`) {
					t.Log("Health file shows WAN as down")
				}
				return
			}
		}
		// Show monitor log for debugging
		monLog, _ := topo.PVE.ExecCT(routerVMID, "cat /tmp/monitor.log 2>&1")
		t.Errorf("Monitor did not remove WAN1 route within 15s.\nLast RIB:\n%s\nMonitor log:\n%s", lastRIB, monLog)
	})

	t.Run("WAN2StillActive", func(t *testing.T) {
		out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
		if err != nil {
			t.Fatalf("vtysh: %v\n%s", err, out)
		}
		if !strings.Contains(out, "203.0.113.254") {
			t.Errorf("WAN2 route should still be active:\n%s", out)
		}
	})

	t.Run("ProbeRecoveryRestoresRoute", func(t *testing.T) {
		// Restore the "gateway" IP — probe will succeed again
		_, err := topo.PVE.ExecCT(routerVMID, "ip addr add 198.51.100.254/24 dev dummy1 2>/dev/null; true")
		if err != nil {
			t.Fatalf("restoring gateway IP: %v", err)
		}
		t.Log("Restored 198.51.100.254 on dummy1")

		// Wait for probe to detect recovery: 1 successful probe should restore
		var lastRIB string
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(1 * time.Second)
			out, err := topo.PVE.ExecCT(routerVMID, "vtysh -c 'show ip route' 2>&1")
			if err != nil {
				continue
			}
			lastRIB = out

			hasWan1 := strings.Contains(out, "198.51.100.254")
			hasWan2 := strings.Contains(out, "203.0.113.254")

			if hasWan1 && hasWan2 {
				t.Logf("Monitor restored WAN1 route after recovery:\n%s", out)
				return
			}
		}
		monLog, _ := topo.PVE.ExecCT(routerVMID, "cat /tmp/monitor.log 2>&1")
		t.Errorf("Monitor did not restore WAN1 route within 15s.\nLast RIB:\n%s\nMonitor log:\n%s", lastRIB, monLog)
	})

	// Cleanup: stop monitor
	topo.PVE.ExecCT(routerVMID, "pkill -f 'warp monitor' 2>/dev/null; true")
}
