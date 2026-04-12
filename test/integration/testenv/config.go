// Package testenv provides test infrastructure for Proxmox-based integration tests.
// All tests in this package require a Proxmox API connection and are gated behind
// the "integration" build tag.
package testenv

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds Proxmox API connection and test environment settings.
// All values are read from environment variables — never hardcoded.
type Config struct {
	// Proxmox API
	APIURL    string // PROXMOX_API_URL (e.g., https://pve.example.com:8006/api2/json)
	TokenID   string // PROXMOX_TOKEN_ID (e.g., root@pam!warp-test)
	Secret    string // PROXMOX_SECRET
	TLSVerify bool   // PROXMOX_TLS_VERIFY (default: true)

	// Node and storage
	Node        string // PROXMOX_NODE (e.g., pve)
	StoragePool string // PROXMOX_STORAGE (e.g., local-lvm)

	// Test images
	LXCTemplatePath string // WARP_LXC_TEMPLATE (path to .tar.xz)
	QCOWImagePath   string // WARP_QCOW2_IMAGE (path to .qcow2)

	// Test networking
	WANBridge  string // WARP_WAN_BRIDGE (default: vmbr1)
	LANBridge  string // WARP_LAN_BRIDGE (default: vmbr2)
	WANSubnet  string // WARP_WAN_SUBNET (default: 10.99.0.0/24)
	LANSubnet  string // WARP_LAN_SUBNET (default: 192.168.99.0/24)

	// Test SSH
	SSHUser    string // WARP_SSH_USER (default: root)
	SSHKeyPath string // WARP_SSH_KEY_PATH (default: ~/.ssh/id_ed25519)

	// VM resources
	VMIDBase int // WARP_VMID_BASE (default: 9000, tests use 9000-9099)
}

// LoadFromEnv reads configuration from environment variables.
// Returns an error if required variables are missing.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		APIURL:    os.Getenv("PROXMOX_API_URL"),
		TokenID:   os.Getenv("PROXMOX_TOKEN_ID"),
		Secret:    os.Getenv("PROXMOX_SECRET"),
		TLSVerify: envBool("PROXMOX_TLS_VERIFY", true),

		Node:        envDefault("PROXMOX_NODE", "pve"),
		StoragePool: envDefault("PROXMOX_STORAGE", "local-lvm"),

		LXCTemplatePath: os.Getenv("WARP_LXC_TEMPLATE"),
		QCOWImagePath:   os.Getenv("WARP_QCOW2_IMAGE"),

		WANBridge: envDefault("WARP_WAN_BRIDGE", "vmbr1"),
		LANBridge: envDefault("WARP_LAN_BRIDGE", "vmbr2"),
		WANSubnet: envDefault("WARP_WAN_SUBNET", "10.99.0.0/24"),
		LANSubnet: envDefault("WARP_LAN_SUBNET", "192.168.99.0/24"),

		SSHUser:    envDefault("WARP_SSH_USER", "root"),
		SSHKeyPath: envDefault("WARP_SSH_KEY_PATH", os.ExpandEnv("$HOME/.ssh/id_ed25519")),

		VMIDBase: envInt("WARP_VMID_BASE", 9000),
	}

	// Validate required fields
	if cfg.APIURL == "" {
		return nil, fmt.Errorf("PROXMOX_API_URL is required")
	}
	if cfg.TokenID == "" {
		return nil, fmt.Errorf("PROXMOX_TOKEN_ID is required")
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("PROXMOX_SECRET is required")
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
