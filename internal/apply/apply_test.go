package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/fdcastel/warp-router/internal/config"
)

// mockReloader records reload calls for testing.
type mockReloader struct {
	reloaded []string
	failOn   string // service name to fail on (empty = never fail)
}

func (m *mockReloader) Reload(service string) error {
	if m.failOn == service {
		return fmt.Errorf("simulated reload failure for %s", service)
	}
	m.reloaded = append(m.reloaded, service)
	return nil
}

func testConfig() *config.SiteConfig {
	return &config.SiteConfig{
		Hostname: "test-router",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp", Gateway: "10.0.0.1"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
		DHCP: &config.DHCPConfig{
			Enabled: true,
			Subnets: []config.DHCPSubnet{
				{
					Subnet:    "192.168.1.0/24",
					Interface: "lan1",
					PoolStart: "192.168.1.100",
					PoolEnd:   "192.168.1.250",
					Gateway:   "192.168.1.1",
				},
			},
		},
		DNS: &config.DNSConfig{
			Enabled:    true,
			Forwarders: []string{"1.1.1.1"},
		},
	}
}

func TestPipelineRendersAllConfigs(t *testing.T) {
	tmpDir := t.TempDir()
	reloader := &mockReloader{}

	pipeline := &Pipeline{
		Reloader: reloader,
		Steps: []Step{
			{
				Name:       "sysctl",
				ConfigPath: filepath.Join(tmpDir, "sysctl.conf"),
				Render: func(cfg *config.SiteConfig) (string, error) {
					return "net.ipv4.ip_forward = 1\n", nil
				},
			},
			{
				Name:       "frr",
				ConfigPath: filepath.Join(tmpDir, "frr.conf"),
				Render: func(cfg *config.SiteConfig) (string, error) {
					return "! frr config\n", nil
				},
				Service: "frr",
			},
			{
				Name:       "nftables",
				ConfigPath: filepath.Join(tmpDir, "nftables.conf"),
				Render: func(cfg *config.SiteConfig) (string, error) {
					return "flush ruleset\n", nil
				},
				Service: "nftables",
			},
		},
	}

	cfg := testConfig()
	result := pipeline.Execute(cfg)

	if result.Failed != "" {
		t.Fatalf("pipeline failed at step %q: %v", result.Failed, result.Err)
	}

	if len(result.Completed) != 3 {
		t.Errorf("completed %d steps, want 3", len(result.Completed))
	}

	// Verify files were written
	for _, step := range pipeline.Steps {
		data, err := os.ReadFile(step.ConfigPath)
		if err != nil {
			t.Errorf("config %s not written: %v", step.Name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("config %s is empty", step.Name)
		}
	}

	// Verify reloads
	if len(reloader.reloaded) != 2 {
		t.Errorf("reloaded %d services, want 2 (frr, nftables)", len(reloader.reloaded))
	}
}

func TestPipelineStopsOnRenderError(t *testing.T) {
	tmpDir := t.TempDir()
	reloader := &mockReloader{}

	pipeline := &Pipeline{
		Reloader: reloader,
		Steps: []Step{
			{
				Name:       "good",
				ConfigPath: filepath.Join(tmpDir, "good.conf"),
				Render: func(cfg *config.SiteConfig) (string, error) {
					return "ok\n", nil
				},
			},
			{
				Name:       "broken",
				ConfigPath: filepath.Join(tmpDir, "broken.conf"),
				Render: func(cfg *config.SiteConfig) (string, error) {
					return "", fmt.Errorf("render failure")
				},
			},
			{
				Name:       "unreachable",
				ConfigPath: filepath.Join(tmpDir, "unreachable.conf"),
				Render: func(cfg *config.SiteConfig) (string, error) {
					return "should not be called\n", nil
				},
			},
		},
	}

	cfg := testConfig()
	result := pipeline.Execute(cfg)

	if result.Failed != "broken" {
		t.Errorf("failed = %q, want %q", result.Failed, "broken")
	}
	if len(result.Completed) != 1 {
		t.Errorf("completed %d steps, want 1", len(result.Completed))
	}

	// Unreachable config should not exist
	if _, err := os.Stat(filepath.Join(tmpDir, "unreachable.conf")); err == nil {
		t.Error("unreachable.conf should not have been written")
	}
}

func TestPipelineStopsOnReloadError(t *testing.T) {
	tmpDir := t.TempDir()
	reloader := &mockReloader{failOn: "bad-service"}

	pipeline := &Pipeline{
		Reloader: reloader,
		Steps: []Step{
			{
				Name:       "step1",
				ConfigPath: filepath.Join(tmpDir, "step1.conf"),
				Render: func(cfg *config.SiteConfig) (string, error) {
					return "ok\n", nil
				},
				Service: "bad-service",
			},
		},
	}

	cfg := testConfig()
	result := pipeline.Execute(cfg)

	if result.Failed != "step1" {
		t.Errorf("failed = %q, want %q", result.Failed, "step1")
	}
	if result.Err == nil {
		t.Error("expected error")
	}
}

func TestAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sub", "dir", "test.conf")

	content := "test content\n"
	if err := atomicWrite(path, content); err != nil {
		t.Fatalf("atomicWrite error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", string(data), content)
	}

	// Check permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat error: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("permissions = %o, want 644", info.Mode().Perm())
	}
}

func TestAtomicWriteOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.conf")

	// Write initial content
	if err := atomicWrite(path, "old content\n"); err != nil {
		t.Fatalf("first write error: %v", err)
	}

	// Overwrite
	if err := atomicWrite(path, "new content\n"); err != nil {
		t.Fatalf("second write error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != "new content\n" {
		t.Errorf("content = %q, want %q", string(data), "new content\n")
	}
}

func TestNewPipelineHasAllSteps(t *testing.T) {
	pipeline := NewPipeline(&mockReloader{})

	expectedSteps := []string{"sysctl", "frr", "nftables", "kea", "unbound"}
	if len(pipeline.Steps) != len(expectedSteps) {
		t.Fatalf("pipeline has %d steps, want %d", len(pipeline.Steps), len(expectedSteps))
	}
	for i, expected := range expectedSteps {
		if pipeline.Steps[i].Name != expected {
			t.Errorf("step[%d].Name = %q, want %q", i, pipeline.Steps[i].Name, expected)
		}
	}
}

func TestParentDevice(t *testing.T) {
	tests := []struct {
		device string
		want   string
	}{
		{"eth0.100", "eth0"},
		{"bond0.200", "bond0"},
		{"ens3.50", "ens3"},
		{"dummy0.10", "dummy0"},
		{"eth0", ""},
		{"", ""},
		{".100", ""},
	}

	for _, tt := range tests {
		got := ParentDevice(tt.device)
		if got != tt.want {
			t.Errorf("ParentDevice(%q) = %q, want %q", tt.device, got, tt.want)
		}
	}
}
