package apply

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/fdcastel/warp-router/internal/config"
	"github.com/fdcastel/warp-router/internal/services/frr"
	"github.com/fdcastel/warp-router/internal/services/kea"
	"github.com/fdcastel/warp-router/internal/services/nftables"
	"github.com/fdcastel/warp-router/internal/services/sysctl"
	"github.com/fdcastel/warp-router/internal/services/unbound"
)

// Target file paths for rendered configs.
const (
	FRRConfPath     = "/etc/frr/frr.conf"
	NFTConfPath     = "/etc/nftables.conf"
	KeaConfPath     = "/etc/kea/kea-dhcp4.conf"
	UnboundConfPath = "/etc/unbound/unbound.conf.d/warp-router.conf"
	SysctlConfPath  = "/etc/sysctl.d/90-warp-router.conf"
	LockFilePath    = "/run/warp-apply.lock"
)

// ServiceReloader defines how to reload a service after config change.
type ServiceReloader interface {
	Reload(service string) error
}

// SystemdReloader reloads services via systemctl.
type SystemdReloader struct{}

func (s *SystemdReloader) Reload(service string) error {
	cmd := exec.Command("systemctl", "reload-or-restart", service)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Step represents a single unit of work in the apply pipeline.
type Step struct {
	Name       string
	ConfigPath string
	Render     func(*config.SiteConfig) (string, error)
	Service    string // systemd unit to reload (empty = no reload)
}

// Pipeline orchestrates rendering configs, writing files, and reloading services.
type Pipeline struct {
	Steps    []Step
	Reloader ServiceReloader
}

// NewPipeline creates the default apply pipeline.
func NewPipeline(reloader ServiceReloader) *Pipeline {
	return &Pipeline{
		Reloader: reloader,
		Steps: []Step{
			{Name: "sysctl", ConfigPath: SysctlConfPath, Render: sysctl.Render, Service: ""},
			{Name: "frr", ConfigPath: FRRConfPath, Render: frr.Render, Service: "frr"},
			{Name: "nftables", ConfigPath: NFTConfPath, Render: nftables.Render, Service: "nftables"},
			{Name: "kea", ConfigPath: KeaConfPath, Render: kea.Render, Service: "kea-dhcp4-server"},
			{Name: "unbound", ConfigPath: UnboundConfPath, Render: unbound.Render, Service: "unbound"},
		},
	}
}

// Result captures the outcome of an apply run.
type Result struct {
	Completed []string // Steps that completed successfully
	Failed    string   // Step that failed (empty if all succeeded)
	Err       error    // Error from the failed step
}

// Execute runs the full apply pipeline: render → write → reload for each step.
// On failure, it attempts to restore previously backed-up config files.
func (p *Pipeline) Execute(cfg *config.SiteConfig) Result {
	var result Result

	// Provision VLAN subinterfaces before config rendering
	if err := ProvisionVLANs(cfg); err != nil {
		result.Failed = "vlan"
		result.Err = err
		return result
	}

	// Back up existing config files before overwriting
	backups := make(map[string]string) // configPath → backupPath
	for _, step := range p.Steps {
		backupPath, err := backupFile(step.ConfigPath)
		if err == nil && backupPath != "" {
			backups[step.ConfigPath] = backupPath
		}
	}

	for _, step := range p.Steps {
		// Render config
		content, err := step.Render(cfg)
		if err != nil {
			result.Failed = step.Name
			result.Err = fmt.Errorf("render %s: %w", step.Name, err)
			restoreBackups(backups)
			return result
		}

		// Write atomically (write to temp, rename)
		if err := atomicWrite(step.ConfigPath, content); err != nil {
			result.Failed = step.Name
			result.Err = fmt.Errorf("write %s to %s: %w", step.Name, step.ConfigPath, err)
			restoreBackups(backups)
			return result
		}

		// Apply sysctl directly (no service reload)
		if step.Name == "sysctl" {
			if err := applySysctl(step.ConfigPath); err != nil {
				result.Failed = step.Name
				result.Err = fmt.Errorf("apply sysctl: %w", err)
				restoreBackups(backups)
				return result
			}
		}

		// Reload service
		if step.Service != "" && p.Reloader != nil {
			if err := p.Reloader.Reload(step.Service); err != nil {
				result.Failed = step.Name
				result.Err = fmt.Errorf("reload %s: %w", step.Service, err)
				restoreBackups(backups)
				return result
			}
		}

		result.Completed = append(result.Completed, step.Name)
	}

	// Clean up backups on success
	for _, backupPath := range backups {
		os.Remove(backupPath)
	}

	return result
}

// backupFile creates a backup copy of a config file. Returns "" if the source doesn't exist.
func backupFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	backupPath := path + ".warp-backup"
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", err
	}
	return backupPath, nil
}

// restoreBackups restores config files from backups (best-effort).
func restoreBackups(backups map[string]string) {
	for configPath, backupPath := range backups {
		data, err := os.ReadFile(backupPath)
		if err != nil {
			continue
		}
		os.WriteFile(configPath, data, 0644)
		os.Remove(backupPath)
	}
}

// atomicWrite writes content to a file atomically via temp file + rename.
func atomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".warp-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("syncing temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming %s → %s: %w", tmpPath, path, err)
	}

	return nil
}

// applySysctl loads sysctl settings from a file.
// Uses -e to continue on errors (e.g., conntrack_max in unprivileged LXC).
func applySysctl(path string) error {
	cmd := exec.Command("sysctl", "-e", "-p", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ProvisionVLANs creates VLAN subinterfaces for interfaces with VLAN > 0.
// It reconciles IP addresses on existing interfaces, creates missing VLANs,
// and removes stale VLAN interfaces not in the config.
func ProvisionVLANs(cfg *config.SiteConfig) error {
	// Build set of desired VLAN devices
	desiredVLANs := make(map[string]config.Interface)
	for _, iface := range cfg.Interfaces {
		if iface.VLAN > 0 {
			desiredVLANs[iface.Device] = iface
		}
	}

	// Collect all parent devices referenced by config VLANs
	parentDevices := make(map[string]bool)
	for _, iface := range desiredVLANs {
		parent := ParentDevice(iface.Device)
		if parent != "" {
			parentDevices[parent] = true
		}
	}

	// Remove stale VLAN interfaces: any VLAN sub-interface on a known parent
	// that is not in the desired set
	for parent := range parentDevices {
		existingVLANs, err := listVLANSubinterfaces(parent)
		if err != nil {
			continue // best effort
		}
		for _, existing := range existingVLANs {
			if _, wanted := desiredVLANs[existing]; !wanted {
				exec.Command("ip", "link", "delete", existing).Run()
			}
		}
	}

	// Create or reconcile desired VLANs
	for _, iface := range cfg.Interfaces {
		if iface.VLAN <= 0 {
			continue
		}

		parent := ParentDevice(iface.Device)
		if parent == "" {
			return fmt.Errorf("VLAN interface %s: cannot derive parent from device %q (expected format: parent.vid)",
				iface.Name, iface.Device)
		}

		exists := exec.Command("ip", "link", "show", iface.Device).Run() == nil

		if !exists {
			// Create VLAN subinterface
			cmd := exec.Command("ip", "link", "add", "link", parent,
				"name", iface.Device, "type", "vlan", "id", fmt.Sprintf("%d", iface.VLAN))
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("creating VLAN %d on %s: %w\n%s", iface.VLAN, parent, err, out)
			}

			// Bring it up
			if out, err := exec.Command("ip", "link", "set", iface.Device, "up").CombinedOutput(); err != nil {
				return fmt.Errorf("bringing up %s: %w\n%s", iface.Device, err, out)
			}
		}

		// Reconcile IP address (flush existing, assign desired)
		if iface.Address != "" && iface.Address != "dhcp" {
			if exists {
				exec.Command("ip", "addr", "flush", "dev", iface.Device).Run()
			}
			out, err := exec.Command("ip", "addr", "add", iface.Address, "dev", iface.Device).CombinedOutput()
			if err != nil {
				if !strings.Contains(string(out), "File exists") {
					return fmt.Errorf("assigning %s to %s: %w\n%s", iface.Address, iface.Device, err, out)
				}
			}
		}
	}
	return nil
}

// listVLANSubinterfaces returns VLAN sub-interface names for a given parent device.
func listVLANSubinterfaces(parent string) ([]string, error) {
	out, err := exec.Command("ip", "-o", "link", "show", "type", "vlan").CombinedOutput()
	if err != nil {
		return nil, err
	}
	var result []string
	for _, line := range strings.Split(string(out), "\n") {
		// Format: "N: dev@parent: ..."
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		devPart := strings.TrimSuffix(fields[1], ":")
		parts := strings.SplitN(devPart, "@", 2)
		if len(parts) == 2 && parts[1] == parent {
			result = append(result, parts[0])
		}
	}
	return result, nil
}

// ParentDevice extracts the parent device name from a dotted VLAN device name.
// For example, "eth0.100" returns "eth0". Returns "" if no dot is found.
func ParentDevice(device string) string {
	if idx := strings.LastIndex(device, "."); idx > 0 {
		return device[:idx]
	}
	return ""
}

// AcquireLock acquires an exclusive file lock for the apply pipeline.
// Returns a cleanup function that must be called to release the lock.
func AcquireLock() (func(), error) {
	f, err := os.OpenFile(LockFilePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another warp apply is already running")
	}

	cleanup := func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		os.Remove(LockFilePath)
	}

	return cleanup, nil
}
