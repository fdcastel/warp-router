package testenv

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// PVE provides SSH-based access to Proxmox VE host for running pct/qm commands.
type PVE struct {
	client *ssh.Client
	host   string
}

// ConnectPVE establishes an SSH connection to the Proxmox VE host.
func ConnectPVE(host, user, keyPath string) (*PVE, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading SSH key %s: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), config)
	if err != nil {
		return nil, fmt.Errorf("SSH dial %s: %w", host, err)
	}

	return &PVE{client: client, host: host}, nil
}

// Run executes a command on the PVE host and returns stdout.
// Returns an error if the command fails (non-zero exit).
func (p *PVE) Run(cmd string) (string, error) {
	session, err := p.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(cmd); err != nil {
		return stdout.String(), fmt.Errorf("command %q failed: %w\nstderr: %s", cmd, err, stderr.String())
	}

	return stdout.String(), nil
}

// Close closes the SSH connection.
func (p *PVE) Close() error {
	return p.client.Close()
}

// --- Container operations ---

// CreateCT creates an LXC container on the PVE host.
func (p *PVE) CreateCT(spec CTSpec) error {
	args := []string{
		"pct", "create", fmt.Sprintf("%d", spec.VMID),
		spec.Template,
		"--hostname", spec.Hostname,
		"--cores", fmt.Sprintf("%d", spec.Cores),
		"--memory", fmt.Sprintf("%d", spec.MemoryMB),
		"--rootfs", fmt.Sprintf("%s:%d", spec.Storage, spec.DiskGB),
		"--unprivileged", "1",
	}

	if spec.SSHPublicKey != "" {
		// Write the key to a temp file on the host, use --ssh-public-keys
		_, err := p.Run(fmt.Sprintf("mkdir -p /tmp/warp-test && echo %q > /tmp/warp-test/ct-%d.pub",
			spec.SSHPublicKey, spec.VMID))
		if err != nil {
			return fmt.Errorf("writing SSH key: %w", err)
		}
		args = append(args, "--ssh-public-keys", fmt.Sprintf("/tmp/warp-test/ct-%d.pub", spec.VMID))
	}

	if spec.Password != "" {
		args = append(args, "--password", spec.Password)
	}

	// Add network interfaces
	for i, nic := range spec.NICs {
		netStr := fmt.Sprintf("name=eth%d,bridge=%s", i, nic.Bridge)
		if nic.IP != "" {
			netStr += fmt.Sprintf(",ip=%s", nic.IP)
		}
		if nic.Gateway != "" {
			netStr += fmt.Sprintf(",gw=%s", nic.Gateway)
		}
		if nic.Firewall {
			netStr += ",firewall=1"
		}
		if nic.MAC != "" {
			netStr += fmt.Sprintf(",hwaddr=%s", nic.MAC)
		}
		args = append(args, fmt.Sprintf("--net%d", i), netStr)
	}

	// Extra features for nesting (needed for sysctl etc.)
	if spec.Nesting {
		args = append(args, "--features", "nesting=1")
	}

	cmd := strings.Join(args, " ")
	_, err := p.Run(cmd)
	return err
}

// StartCT starts a container.
func (p *PVE) StartCT(vmid int) error {
	_, err := p.Run(fmt.Sprintf("pct start %d", vmid))
	return err
}

// StopCT stops a container (force).
func (p *PVE) StopCT(vmid int) error {
	_, _ = p.Run(fmt.Sprintf("pct stop %d 2>/dev/null; while pct status %d 2>/dev/null | grep -q running; do sleep 1; done; true", vmid, vmid))
	return nil
}

// DestroyCT destroys a container (stops first if running).
func (p *PVE) DestroyCT(vmid int) error {
	p.StopCT(vmid)
	// Wait briefly for stop to complete
	time.Sleep(2 * time.Second)
	_, err := p.Run(fmt.Sprintf("pct destroy %d --force --purge 2>/dev/null; true", vmid))
	return err
}

// ExecCT runs a command inside a container via pct exec.
func (p *PVE) ExecCT(vmid int, cmd string) (string, error) {
	return p.Run(fmt.Sprintf("pct exec %d -- bash -c %q", vmid, cmd))
}

// WaitForCT waits until a container is running and has network connectivity.
func (p *PVE) WaitForCT(vmid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := p.Run(fmt.Sprintf("pct status %d", vmid))
		if err == nil && strings.Contains(out, "running") {
			// Check if networking is up
			_, err := p.ExecCT(vmid, "ip -4 addr show dev eth0 | grep inet")
			if err == nil {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("container %d not ready within %v", vmid, timeout)
}

// GetCTIP returns the first IPv4 address of a container's eth0.
func (p *PVE) GetCTIP(vmid int) (string, error) {
	out, err := p.ExecCT(vmid, "ip -4 addr show dev eth0 | grep -oP 'inet \\K[0-9.]+'")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// --- Bridge operations ---

// CreateBridge creates a Linux bridge on the PVE host (internal, no physical port).
func (p *PVE) CreateBridge(name, cidr string) error {
	// Check if bridge already exists
	_, err := p.Run(fmt.Sprintf("ip link show %s 2>/dev/null", name))
	if err == nil {
		return nil // already exists
	}

	cmds := fmt.Sprintf(
		"ip link add name %s type bridge && ip link set %s up",
		name, name)
	if cidr != "" {
		cmds += fmt.Sprintf(" && ip addr add %s dev %s", cidr, name)
	}
	_, err = p.Run(cmds)
	return err
}

// DestroyBridge removes a Linux bridge from the PVE host.
func (p *PVE) DestroyBridge(name string) error {
	_, _ = p.Run(fmt.Sprintf("ip link set %s down 2>/dev/null; ip link del %s 2>/dev/null; true", name, name))
	return nil
}

// --- Helpers ---

// UploadFile writes content to a file on the PVE host.
func (p *PVE) UploadFile(path, content string) error {
	// Ensure parent directory exists
	dir := path[:strings.LastIndex(path, "/")]
	p.Run(fmt.Sprintf("mkdir -p %s", dir))
	_, err := p.Run(fmt.Sprintf("cat > %s << 'WARP_EOF'\n%s\nWARP_EOF", path, content))
	return err
}

// UploadFileToCT writes content to a file inside a container.
// It writes to a temp file on the PVE host first, then pushes to the container.
func (p *PVE) UploadFileToCT(vmid int, path, content string) error {
	tmpPath := fmt.Sprintf("/tmp/warp-test/ct-%d-upload", vmid)
	if err := p.UploadFile(tmpPath, content); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	_, err := p.Run(fmt.Sprintf("pct push %d %s %s", vmid, tmpPath, path))
	if err != nil {
		return fmt.Errorf("pushing file to CT: %w", err)
	}
	return nil
}

// CTSpec defines the configuration for a test LXC container.
type CTSpec struct {
	VMID         int
	Hostname     string
	Template     string
	Storage      string
	Cores        int
	MemoryMB     int
	DiskGB       int
	NICs         []NICSpec
	SSHPublicKey string
	Password     string
	Nesting      bool
}

// NICSpec defines a NIC attachment for a container.
type NICSpec struct {
	Bridge   string
	IP       string // CIDR or "dhcp"
	Gateway  string
	Firewall bool
	MAC      string
}
