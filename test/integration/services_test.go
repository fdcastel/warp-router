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
	cfg, err := testenv.LoadFromEnv()
	if err != nil {
		t.Skipf("integration tests skipped: %v", err)
	}

	pve, err := testenv.ConnectPVE(cfg.PVEHost, cfg.PVEUser, cfg.SSHKeyPath)
	if err != nil {
		t.Fatalf("connecting to PVE: %v", err)
	}
	defer pve.Close()

	vmid := cfg.VMIDBase + 60
	pve.DestroyCT(vmid) // clean up from prior runs

	t.Cleanup(func() {
		pve.DestroyCT(vmid)
	})

	err = pve.CreateCT(testenv.CTSpec{
		VMID:     vmid,
		Hostname: "warp-health",
		Template: "local:vztmpl/warp-router-dev-lxc-amd64.tar.zst",
		Storage:  cfg.StoragePool,
		Cores:    1,
		MemoryMB: 512,
		DiskGB:   4,
		NICs: []testenv.NICSpec{
			{Bridge: "vmbr0", IP: "51.222.19.49/32", Gateway: "100.64.0.1", MAC: "02:00:00:48:58:c3"},
		},
		Password: "warp-test-2026",
		Nesting:  true,
	})
	if err != nil {
		t.Fatalf("creating CT: %v", err)
	}
	if err := pve.StartCT(vmid); err != nil {
		t.Fatalf("starting CT: %v", err)
	}
	if err := pve.WaitForCT(vmid, 30e9); err != nil {
		t.Fatalf("waiting for CT: %v", err)
	}

	// Apply a config
	siteConfig := `---
hostname: warp-health

interfaces:
  - name: wan0
    role: wan
    device: eth0
    address: 51.222.19.49/32
    gateway: 100.64.0.1
  - name: lan0
    role: lan
    device: dummy0
    address: 192.168.99.1/24

dns:
  enabled: true
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`
	if err := pve.UploadFileToCT(vmid, "/etc/warp/site.yaml", siteConfig); err != nil {
		t.Fatalf("uploading config: %v", err)
	}
	out, err := pve.ExecCT(vmid, "/usr/local/bin/warp apply /etc/warp/site.yaml 2>&1")
	if err != nil {
		t.Fatalf("warp apply failed: %v\noutput: %s", err, out)
	}

	t.Run("WarpStatusShowsRevision", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "/usr/local/bin/warp status 2>&1")
		if err != nil {
			t.Fatalf("warp status failed: %v", err)
		}
		if !strings.Contains(out, "Current revision:") {
			t.Errorf("expected revision info in status: %s", out)
		}
		if strings.Contains(out, "No config") {
			t.Error("status shows 'No config' after successful apply")
		}
		t.Logf("status: %s", out)
	})

	t.Run("ServicesRunningAfterApply", func(t *testing.T) {
		services := []string{"frr", "nftables", "ssh", "kea-dhcp4-server", "unbound"}
		for _, svc := range services {
			out, err := pve.ExecCT(vmid, "systemctl is-active "+svc)
			if err != nil || strings.TrimSpace(out) != "active" {
				t.Errorf("service %s: got %q, want active", svc, strings.TrimSpace(out))
			}
		}
	})

	t.Run("FRRConfigApplied", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "cat /etc/frr/frr.conf")
		if err != nil {
			t.Fatalf("reading frr.conf: %v", err)
		}
		if !strings.Contains(out, "hostname warp-health") {
			t.Error("frr.conf missing hostname")
		}
		if !strings.Contains(out, "ip route 0.0.0.0/0 100.64.0.1") {
			t.Errorf("frr.conf missing default route, got:\n%s", out)
		}
	})

	t.Run("NFTablesConfigApplied", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "cat /etc/nftables.conf")
		if err != nil {
			t.Fatalf("reading nftables.conf: %v", err)
		}
		if !strings.Contains(out, "masquerade") {
			t.Errorf("nftables.conf missing masquerade rule")
		}
	})

	t.Run("IPForwardingEnabled", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "sysctl -n net.ipv4.ip_forward")
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
	cfg, err := testenv.LoadFromEnv()
	if err != nil {
		t.Skipf("integration tests skipped: %v", err)
	}

	pve, err := testenv.ConnectPVE(cfg.PVEHost, cfg.PVEUser, cfg.SSHKeyPath)
	if err != nil {
		t.Fatalf("connecting to PVE: %v", err)
	}
	defer pve.Close()

	vmid := cfg.VMIDBase + 61
	pve.DestroyCT(vmid) // clean up from prior runs

	t.Cleanup(func() {
		pve.DestroyCT(vmid)
	})

	err = pve.CreateCT(testenv.CTSpec{
		VMID:     vmid,
		Hostname: "warp-rollback",
		Template: "local:vztmpl/warp-router-dev-lxc-amd64.tar.zst",
		Storage:  cfg.StoragePool,
		Cores:    1,
		MemoryMB: 512,
		DiskGB:   4,
		NICs: []testenv.NICSpec{
			{Bridge: "vmbr0", IP: "51.222.19.50/32", Gateway: "100.64.0.1", MAC: "02:00:00:70:8c:51"},
		},
		Password: "warp-test-2026",
		Nesting:  true,
	})
	if err != nil {
		t.Fatalf("creating CT: %v", err)
	}
	if err := pve.StartCT(vmid); err != nil {
		t.Fatalf("starting CT: %v", err)
	}
	if err := pve.WaitForCT(vmid, 30e9); err != nil {
		t.Fatalf("waiting for CT: %v", err)
	}

	// Apply first config
	config1 := `---
hostname: warp-rollback-v1

interfaces:
  - name: wan0
    role: wan
    device: eth0
    address: 51.222.19.50/32
    gateway: 100.64.0.1
  - name: lan0
    role: lan
    device: dummy0
    address: 192.168.99.1/24

dns:
  enabled: true
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`
	if err := pve.UploadFileToCT(vmid, "/etc/warp/site.yaml", config1); err != nil {
		t.Fatalf("uploading config1: %v", err)
	}
	out, err := pve.ExecCT(vmid, "/usr/local/bin/warp apply /etc/warp/site.yaml 2>&1")
	if err != nil {
		t.Fatalf("first apply failed: %v\n%s", err, out)
	}

	// Get first revision
	status1, _ := pve.ExecCT(vmid, "/usr/local/bin/warp status 2>&1")
	t.Logf("After first apply: %s", status1)

	// Apply second config with different hostname
	config2 := `---
hostname: warp-rollback-v2

interfaces:
  - name: wan0
    role: wan
    device: eth0
    address: 51.222.19.50/32
    gateway: 100.64.0.1
  - name: lan0
    role: lan
    device: dummy0
    address: 192.168.99.1/24

dns:
  enabled: true
  listen:
    - 127.0.0.1
  forwarders:
    - 1.1.1.1
`
	if err := pve.UploadFileToCT(vmid, "/etc/warp/site.yaml", config2); err != nil {
		t.Fatalf("uploading config2: %v", err)
	}
	out, err = pve.ExecCT(vmid, "/usr/local/bin/warp apply /etc/warp/site.yaml 2>&1")
	if err != nil {
		t.Fatalf("second apply failed: %v\n%s", err, out)
	}

	t.Run("SecondConfigApplied", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "cat /etc/frr/frr.conf")
		if err != nil {
			t.Fatalf("reading frr.conf: %v", err)
		}
		if !strings.Contains(out, "hostname warp-rollback-v2") {
			t.Error("frr.conf should show v2 hostname")
		}
	})

	t.Run("Rollback", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "/usr/local/bin/warp rollback 2>&1")
		if err != nil {
			t.Fatalf("rollback failed: %v\n%s", err, out)
		}
		t.Logf("rollback output: %s", out)
	})

	t.Run("RollbackRestoredV1", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "cat /etc/frr/frr.conf")
		if err != nil {
			t.Fatalf("reading frr.conf: %v", err)
		}
		if !strings.Contains(out, "hostname warp-rollback-v1") {
			t.Errorf("frr.conf should show v1 hostname after rollback, got:\n%s", out)
		}
	})

	t.Run("RevisionsShowHistory", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "/usr/local/bin/warp revisions 2>&1")
		if err != nil {
			t.Fatalf("revisions failed: %v", err)
		}
		// Should have at least 3 revisions: v1, v2, rollback
		lines := strings.Split(strings.TrimSpace(out), "\n")
		revCount := 0
		for _, l := range lines {
			if strings.Contains(l, "20") { // timestamp like 20260414...
				revCount++
			}
		}
		if revCount < 3 {
			t.Errorf("expected at least 3 revisions, found %d in:\n%s", revCount, out)
		}
		t.Logf("revisions: %s", out)
	})
}
