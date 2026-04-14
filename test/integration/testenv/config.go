// Package testenv provides test infrastructure for Proxmox-based integration tests.
// Provisioning is done via SSH to the Proxmox host, using pct/qm CLI commands.
package testenv

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds Proxmox SSH connection and test environment settings.
// All values are read from environment variables — never hardcoded.
type Config struct {
	// Proxmox host SSH access
	PVEHost    string // PVE_HOST (e.g., bhs-host51.dw.net.br)
	PVEUser    string // PVE_USER (default: root)
	SSHKeyPath string // PVE_SSH_KEY (default: ~/.ssh/id_ed25519)

	// Storage
	StoragePool string // PVE_STORAGE (default: spool-zfs)

	// Template for test containers
	Template string // PVE_TEMPLATE (default: local:vztmpl/debian-12-standard_12.12-1_amd64.tar.zst)

	// Test networking
	WANBridge string // PVE_WAN_BRIDGE (default: vmbr0)
	LANBridge string // PVE_LAN_BRIDGE — created dynamically per test run

	// VMID range
	VMIDBase int // PVE_VMID_BASE (default: 9000, tests use 9000-9099)
}

// LoadFromEnv reads configuration from environment variables.
// Returns an error if required variables are missing.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		PVEHost:    envDefault("PVE_HOST", "bhs-host51.dw.net.br"),
		PVEUser:    envDefault("PVE_USER", "root"),
		SSHKeyPath: envDefault("PVE_SSH_KEY", os.ExpandEnv("$HOME/.ssh/id_ed25519")),

		StoragePool: envDefault("PVE_STORAGE", "spool-zfs"),

		Template: envDefault("PVE_TEMPLATE", "local:vztmpl/debian-12-standard_12.12-1_amd64.tar.zst"),

		WANBridge: envDefault("PVE_WAN_BRIDGE", "vmbr0"),

		VMIDBase: envInt("PVE_VMID_BASE", 9000),
	}

	// Validate reachability
	if cfg.PVEHost == "" {
		return nil, fmt.Errorf("PVE_HOST is required")
	}

	return cfg, nil
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}
