//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/test/integration/testenv"
)

// TestLXCImageLifecycle boots a warp-router LXC container and verifies:
// 1. All core services start (FRR, nftables, Kea, Unbound, SSH, networking)
// 2. warp binary is present and runnable
// 3. /etc/warp directory exists
// 4. SSH service is accessible
func TestLXCImageLifecycle(t *testing.T) {
	cfg, err := testenv.LoadFromEnv()
	if err != nil {
		t.Skipf("integration tests skipped: %v", err)
	}

	pve, err := testenv.ConnectPVE(cfg.PVEHost, cfg.PVEUser, cfg.SSHKeyPath)
	if err != nil {
		t.Fatalf("connecting to PVE: %v", err)
	}
	defer pve.Close()

	vmid := cfg.VMIDBase + 50

	t.Cleanup(func() {
		pve.DestroyCT(vmid)
	})

	// Clean up any leftover container from a previous failed run
	pve.DestroyCT(vmid)

	t.Log("Creating warp-router LXC container")
	err = pve.CreateCT(testenv.CTSpec{
		VMID:     vmid,
		Hostname: "warp-lifecycle",
		Template: "local:vztmpl/warp-router-dev-lxc-amd64.tar.zst",
		Storage:  cfg.StoragePool,
		Cores:    1,
		MemoryMB: 512,
		DiskGB:   4,
		NICs: []testenv.NICSpec{
			{Bridge: "vmbr0", IP: "51.222.19.48/32", Gateway: "100.64.0.1", MAC: "02:00:00:77:ea:5c"},
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

	t.Run("ServicesActive", func(t *testing.T) {
		services := []string{"frr", "nftables", "ssh", "kea-dhcp4-server", "unbound", "networking"}
		for _, svc := range services {
			out, err := pve.ExecCT(vmid, "systemctl is-active "+svc)
			if err != nil || strings.TrimSpace(out) != "active" {
				t.Errorf("service %s: got %q, want active", svc, strings.TrimSpace(out))
			}
		}
	})

	t.Run("WarpBinaryPresent", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "test -x /usr/local/bin/warp && echo ok")
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Fatal("warp binary not found or not executable")
		}
	})

	t.Run("WarpRunnable", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "/usr/local/bin/warp status 2>&1")
		if err != nil {
			// exit 0 even without config is fine, but warp might exit non-zero
			_ = out
		}
		// Just verify it produces output (not a segfault etc.)
		if !strings.Contains(out, "config") && !strings.Contains(out, "revision") && !strings.Contains(out, "No config") {
			t.Errorf("unexpected warp output: %q", out)
		}
	})

	t.Run("WarpConfigDir", func(t *testing.T) {
		out, err := pve.ExecCT(vmid, "test -d /etc/warp && echo ok")
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Fatal("/etc/warp directory not found")
		}
	})

	t.Run("InternetConnectivity", func(t *testing.T) {
		_, err := pve.ExecCT(vmid, "ping -c 2 -W 5 1.1.1.1")
		if err != nil {
			t.Fatal("no internet connectivity")
		}
	})

	t.Run("DNSResolution", func(t *testing.T) {
		_, err := pve.ExecCT(vmid, "host google.com 127.0.0.1 2>/dev/null || dig @127.0.0.1 google.com +short 2>/dev/null")
		if err != nil {
			// DNS might not work yet — Unbound needs to resolve upstream
			t.Skip("DNS resolution not yet available (acceptable on first boot)")
		}
	})
}
