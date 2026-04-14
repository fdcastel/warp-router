package apply

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
func (p *Pipeline) Execute(cfg *config.SiteConfig) Result {
	var result Result

	for _, step := range p.Steps {
		// Render config
		content, err := step.Render(cfg)
		if err != nil {
			result.Failed = step.Name
			result.Err = fmt.Errorf("render %s: %w", step.Name, err)
			return result
		}

		// Write atomically (write to temp, rename)
		if err := atomicWrite(step.ConfigPath, content); err != nil {
			result.Failed = step.Name
			result.Err = fmt.Errorf("write %s to %s: %w", step.Name, step.ConfigPath, err)
			return result
		}

		// Apply sysctl directly (no service reload)
		if step.Name == "sysctl" {
			if err := applySysctl(step.ConfigPath); err != nil {
				result.Failed = step.Name
				result.Err = fmt.Errorf("apply sysctl: %w", err)
				return result
			}
		}

		// Reload service
		if step.Service != "" && p.Reloader != nil {
			if err := p.Reloader.Reload(step.Service); err != nil {
				result.Failed = step.Name
				result.Err = fmt.Errorf("reload %s: %w", step.Service, err)
				return result
			}
		}

		result.Completed = append(result.Completed, step.Name)
	}

	return result
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
