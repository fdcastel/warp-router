package testenv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// TestRunID generates a short unique ID for a test run to isolate resources.
func TestRunID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// SDNConfig holds the SDN zone and vnet names for a test run.
type SDNConfig struct {
	RunID   string
	Zone    string
	WANVnet string
	WAN2Vnet string
	LANVnet string
}

// NewSDNConfig creates SDN resource names derived from a test run ID.
func NewSDNConfig(runID string) *SDNConfig {
	return &SDNConfig{
		RunID:    runID,
		Zone:     fmt.Sprintf("wt%s", runID),
		WANVnet:  fmt.Sprintf("wwan1%s", runID),
		WAN2Vnet: fmt.Sprintf("wwan2%s", runID),
		LANVnet:  fmt.Sprintf("wlan%s", runID),
	}
}

// SDNProvisioner manages SDN zones and vnets on Proxmox.
// Actual Proxmox API calls require integration test context.
type SDNProvisioner struct {
	Config  *Config
	SDN     *SDNConfig
}

// NewSDNProvisioner creates a provisioner for the given test run.
func NewSDNProvisioner(cfg *Config, runID string) *SDNProvisioner {
	return &SDNProvisioner{
		Config: cfg,
		SDN:    NewSDNConfig(runID),
	}
}

// TODO: Implement Create() and Destroy() methods using go-proxmox API
// when Proxmox access is available.
// Create() should:
//   1. Create a simple SDN zone
//   2. Create WAN, WAN2, LAN vnets within it
//   3. Apply SDN changes
// Destroy() should:
//   1. Delete vnets
//   2. Delete zone
//   3. Apply SDN changes
