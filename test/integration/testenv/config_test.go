package testenv

import (
	"testing"
)

func TestLoadFromEnvDefaults(t *testing.T) {
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PVEHost != "bhs-host51.dw.net.br" {
		t.Errorf("PVEHost = %q, want default", cfg.PVEHost)
	}
	if cfg.PVEUser != "root" {
		t.Errorf("PVEUser = %q, want %q", cfg.PVEUser, "root")
	}
	if cfg.StoragePool != "spool-zfs" {
		t.Errorf("StoragePool = %q, want %q", cfg.StoragePool, "spool-zfs")
	}
	if cfg.VMIDBase != 9000 {
		t.Errorf("VMIDBase = %d, want %d", cfg.VMIDBase, 9000)
	}
	if cfg.WANBridge != "vmbr0" {
		t.Errorf("WANBridge = %q, want %q", cfg.WANBridge, "vmbr0")
	}
}

func TestLoadFromEnvCustomValues(t *testing.T) {
	t.Setenv("PVE_HOST", "custom-host")
	t.Setenv("PVE_STORAGE", "local-lvm")
	t.Setenv("PVE_VMID_BASE", "8000")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PVEHost != "custom-host" {
		t.Errorf("PVEHost = %q, want %q", cfg.PVEHost, "custom-host")
	}
	if cfg.StoragePool != "local-lvm" {
		t.Errorf("StoragePool = %q, want %q", cfg.StoragePool, "local-lvm")
	}
	if cfg.VMIDBase != 8000 {
		t.Errorf("VMIDBase = %d, want %d", cfg.VMIDBase, 8000)
	}
}

func TestLoadFromEnvMissingHost(t *testing.T) {
	t.Setenv("PVE_HOST", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		// Good — empty host is rejected
		return
	}
	// envDefault returns fallback when env var is empty,
	// so this should succeed with the default host
	if cfg.PVEHost == "" {
		t.Fatal("PVEHost should not be empty after LoadFromEnv")
	}
}
