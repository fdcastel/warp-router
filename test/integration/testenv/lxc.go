package testenv

// LXCSpec defines the configuration for provisioning a test LXC container.
type LXCSpec struct {
	VMID     int
	Hostname string
	Template string // path to uploaded template
	Cores    int
	MemoryMB int
	NICs     []NICSpec
}

// NICSpec defines a NIC attachment for a container or VM.
type NICSpec struct {
	Bridge  string // Proxmox bridge or vnet name
	IP      string // Static IP in CIDR notation, or "dhcp"
	Gateway string // Default gateway (optional)
}

// LXCProvisioner manages LXC containers on Proxmox.
type LXCProvisioner struct {
	Config *Config
}

// NewLXCProvisioner creates a provisioner for LXC containers.
func NewLXCProvisioner(cfg *Config) *LXCProvisioner {
	return &LXCProvisioner{Config: cfg}
}

// TODO: Implement when Proxmox access is available:
// Create(spec LXCSpec) error
//   - Upload template if not present
//   - Create container via API
//   - Attach NICs to specified bridges/vnets
//   - Start container
//   - Wait for SSH (using support.ConnectWithRetry)
//
// Destroy(vmid int) error
//   - Stop container
//   - Destroy container
