package testenv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Topology represents a complete test environment on a Proxmox host.
type Topology struct {
	Config *Config
	PVE    *PVE
	RunID  string

	// Bridges created for this test run
	LANBridge string // Internal bridge for LAN segment

	// Container VMIDs allocated
	Containers []int

	// Addresses assigned
	RouterLANIP   string
	ClientLANIP   string
}

// TopologySpec defines the desired test topology.
type TopologySpec struct {
	// RouterTemplate overrides the default template for the router CT.
	// If empty, uses Config.Template (standard Debian).
	RouterTemplate string
}

// TestRunID generates a short unique ID for a test run.
func TestRunID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// NewTopology creates a topology builder for a test.
// Connects to PVE host and registers cleanup.
func NewTopology(t *testing.T, spec TopologySpec) *Topology {
	t.Helper()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Skipf("integration tests skipped: %v", err)
	}

	pve, err := ConnectPVE(cfg.PVEHost, cfg.PVEUser, cfg.SSHKeyPath)
	if err != nil {
		t.Fatalf("connecting to PVE host: %v", err)
	}

	runID := TestRunID()
	topo := &Topology{
		Config:    cfg,
		PVE:       pve,
		RunID:     runID,
		LANBridge: fmt.Sprintf("wt%s", runID), // e.g., wt1a2b3c
	}

	t.Cleanup(func() {
		topo.Teardown(t)
		pve.Close()
	})

	return topo
}

// AllocVMID returns the next VMID for this test run.
func (topo *Topology) AllocVMID() int {
	id := topo.Config.VMIDBase + len(topo.Containers)
	topo.Containers = append(topo.Containers, id)
	return id
}

// Setup provisions the test topology:
// 1. Create internal LAN bridge
// 2. Create router CT (with LAN interface on internal bridge)
// 3. Create client CT (on internal bridge)
func (topo *Topology) Setup(t *testing.T) {
	t.Helper()

	// Read SSH public key for container access
	keyPath := topo.Config.SSHKeyPath + ".pub"
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("reading SSH public key %s: %v", keyPath, err)
	}
	sshPubKey := strings.TrimSpace(string(keyData))

	// 1. Create internal LAN bridge
	t.Logf("Creating LAN bridge %s", topo.LANBridge)
	if err := topo.PVE.CreateBridge(topo.LANBridge, ""); err != nil {
		t.Fatalf("creating LAN bridge: %v", err)
	}

	// 2. Create router CT
	routerVMID := topo.AllocVMID()
	topo.RouterLANIP = "10.99.0.1/24"
	t.Logf("Creating router CT %d", routerVMID)
	err = topo.PVE.CreateCT(CTSpec{
		VMID:     routerVMID,
		Hostname: fmt.Sprintf("wt-router-%s", topo.RunID),
		Template: topo.Config.Template,
		Storage:  topo.Config.StoragePool,
		Cores:    1,
		MemoryMB: 512,
		DiskGB:   4,
		NICs: []NICSpec{
			{Bridge: topo.LANBridge, IP: topo.RouterLANIP},
		},
		SSHPublicKey: sshPubKey,
		Nesting:      true,
	})
	if err != nil {
		t.Fatalf("creating router CT: %v", err)
	}

	if err := topo.PVE.StartCT(routerVMID); err != nil {
		t.Fatalf("starting router CT: %v", err)
	}

	// 3. Create client CT
	clientVMID := topo.AllocVMID()
	topo.ClientLANIP = "10.99.0.2/24"
	t.Logf("Creating client CT %d", clientVMID)
	err = topo.PVE.CreateCT(CTSpec{
		VMID:     clientVMID,
		Hostname: fmt.Sprintf("wt-client-%s", topo.RunID),
		Template: topo.Config.Template,
		Storage:  topo.Config.StoragePool,
		Cores:    1,
		MemoryMB: 256,
		DiskGB:   2,
		NICs: []NICSpec{
			{Bridge: topo.LANBridge, IP: topo.ClientLANIP, Gateway: "10.99.0.1"},
		},
		SSHPublicKey: sshPubKey,
	})
	if err != nil {
		t.Fatalf("creating client CT: %v", err)
	}

	if err := topo.PVE.StartCT(clientVMID); err != nil {
		t.Fatalf("starting client CT: %v", err)
	}

	// 4. Wait for containers to be ready
	t.Log("Waiting for containers to be ready...")
	for _, vmid := range topo.Containers {
		if err := topo.PVE.WaitForCT(vmid, 30*time.Second); err != nil {
			t.Fatalf("waiting for CT %d: %v", vmid, err)
		}
	}
	t.Log("All containers ready")
}

// RouterVMID returns the VMID of the router container.
func (topo *Topology) RouterVMID() int {
	if len(topo.Containers) < 1 {
		return 0
	}
	return topo.Containers[0]
}

// ClientVMID returns the VMID of the client container.
func (topo *Topology) ClientVMID() int {
	if len(topo.Containers) < 2 {
		return 0
	}
	return topo.Containers[1]
}

// Teardown destroys all test resources.
func (topo *Topology) Teardown(t *testing.T) {
	t.Helper()

	// Destroy containers in reverse order
	for i := len(topo.Containers) - 1; i >= 0; i-- {
		vmid := topo.Containers[i]
		t.Logf("Destroying CT %d", vmid)
		topo.PVE.DestroyCT(vmid)
	}

	// Destroy LAN bridge
	if topo.LANBridge != "" {
		t.Logf("Destroying bridge %s", topo.LANBridge)
		topo.PVE.DestroyBridge(topo.LANBridge)
	}

	// Clean up temp files
	topo.PVE.Run("rm -rf /tmp/warp-test 2>/dev/null; true")
}
