package testenv

import "testing"

// Topology represents a complete test environment with router, WAN gateways, and LAN clients.
type Topology struct {
	Config *Config
	RunID  string
	SDN    *SDNProvisioner

	// Node IPs assigned during provisioning
	RouterWAN1IP   string
	RouterWAN2IP   string
	RouterLANIP    string
	WANGateway1IP  string
	WANGateway2IP  string
	LANClientIP    string
}

// TopologySpec defines the desired test topology.
type TopologySpec struct {
	RouterType  string // "lxc" or "vm"
	DualWAN     bool   // If true, provision two WAN gateways
	LANClients  int    // Number of LAN client containers (default: 1)
}

// NewTopology creates a topology builder for a test.
func NewTopology(t *testing.T, spec TopologySpec) *Topology {
	t.Helper()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Skipf("Proxmox integration tests skipped: %v", err)
	}

	runID := TestRunID()
	topo := &Topology{
		Config: cfg,
		RunID:  runID,
		SDN:    NewSDNProvisioner(cfg, runID),
	}

	// Register cleanup to run even on test failure
	t.Cleanup(func() {
		topo.Teardown()
	})

	return topo
}

// Setup provisions the full test topology.
func (t *Topology) Setup() error {
	// TODO: Implement when Proxmox access is available:
	// 1. Create SDN zone + vnets
	// 2. Provision WAN gateway LXC(s) with IP forwarding + masquerade
	// 3. Provision LAN client LXC
	// 4. Provision router (LXC or VM)
	// 5. Wait for all nodes to be reachable via SSH
	return nil
}

// Teardown destroys all test resources.
func (t *Topology) Teardown() {
	// TODO: Implement when Proxmox access is available:
	// 1. Stop and destroy all containers/VMs
	// 2. Remove SDN vnets and zone
	// 3. Clean up uploaded images
	// Order: VMs/containers first, then SDN, then images
}
