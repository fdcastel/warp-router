package testenv

// VMSpec defines the configuration for provisioning a test QEMU VM.
type VMSpec struct {
	VMID       int
	Hostname   string
	DiskImage  string // path to uploaded QCOW2
	Cores      int
	MemoryMB   int
	NICs       []NICSpec
	CloudInit  *CloudInitSpec
}

// CloudInitSpec holds cloud-init settings for a VM.
type CloudInitSpec struct {
	UserData string // path to user-data YAML
	SSHKeys  string // authorized SSH public keys
}

// VMProvisioner manages QEMU VMs on Proxmox.
type VMProvisioner struct {
	Config *Config
}

// NewVMProvisioner creates a provisioner for QEMU VMs.
func NewVMProvisioner(cfg *Config) *VMProvisioner {
	return &VMProvisioner{Config: cfg}
}

// TODO: Implement when Proxmox access is available:
// Create(spec VMSpec) error
//   - Upload QCOW2 to storage
//   - Create VM via API
//   - Attach disk, NICs, cloud-init drive
//   - Start VM
//   - Wait for SSH
//
// Destroy(vmid int) error
//   - Stop VM
//   - Destroy VM
//   - Clean up disk images
