package testenv

import (
	"os"
	"testing"
)

func TestLoadFromEnvMissingRequired(t *testing.T) {
	// Ensure required vars are unset
	os.Unsetenv("PROXMOX_API_URL")
	os.Unsetenv("PROXMOX_TOKEN_ID")
	os.Unsetenv("PROXMOX_SECRET")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when required env vars are missing")
	}
}

func TestLoadFromEnvWithDefaults(t *testing.T) {
	t.Setenv("PROXMOX_API_URL", "https://pve.test:8006/api2/json")
	t.Setenv("PROXMOX_TOKEN_ID", "root@pam!test")
	t.Setenv("PROXMOX_SECRET", "test-secret")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Node != "pve" {
		t.Errorf("Node = %q, want %q", cfg.Node, "pve")
	}
	if cfg.StoragePool != "local-lvm" {
		t.Errorf("StoragePool = %q, want %q", cfg.StoragePool, "local-lvm")
	}
	if cfg.VMIDBase != 9000 {
		t.Errorf("VMIDBase = %d, want %d", cfg.VMIDBase, 9000)
	}
	if cfg.SSHUser != "root" {
		t.Errorf("SSHUser = %q, want %q", cfg.SSHUser, "root")
	}
	if !cfg.TLSVerify {
		t.Error("TLSVerify should default to true")
	}
}

func TestLoadFromEnvCustomValues(t *testing.T) {
	t.Setenv("PROXMOX_API_URL", "https://custom:8006/api2/json")
	t.Setenv("PROXMOX_TOKEN_ID", "user@pam!token")
	t.Setenv("PROXMOX_SECRET", "secret")
	t.Setenv("PROXMOX_NODE", "node2")
	t.Setenv("PROXMOX_STORAGE", "ceph-pool")
	t.Setenv("WARP_VMID_BASE", "8000")
	t.Setenv("PROXMOX_TLS_VERIFY", "false")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Node != "node2" {
		t.Errorf("Node = %q, want %q", cfg.Node, "node2")
	}
	if cfg.StoragePool != "ceph-pool" {
		t.Errorf("StoragePool = %q, want %q", cfg.StoragePool, "ceph-pool")
	}
	if cfg.VMIDBase != 8000 {
		t.Errorf("VMIDBase = %d, want %d", cfg.VMIDBase, 8000)
	}
	if cfg.TLSVerify {
		t.Error("TLSVerify should be false")
	}
}
