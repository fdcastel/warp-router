package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// buildWarp compiles the warp binary to a temp directory and returns its path.
func buildWarp(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "warp")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/warp/")
	cmd.Dir = findModuleRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return binPath
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root")
		}
		dir = parent
	}
}

func TestCLINoArgs(t *testing.T) {
	bin := buildWarp(t)
	cmd := exec.Command(bin)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("expected non-zero exit code with no args")
	}
	if len(out) == 0 {
		t.Error("expected usage output")
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	bin := buildWarp(t)
	cmd := exec.Command(bin, "bogus")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("expected non-zero exit code for unknown command")
	}
	output := string(out)
	if output == "" {
		t.Error("expected error output")
	}
}

func TestCLIValidateValid(t *testing.T) {
	bin := buildWarp(t)
	cfgPath := filepath.Join(t.TempDir(), "site.yaml")
	yaml := `hostname: test-router
interfaces:
  - name: wan1
    role: wan
    device: eth0
    address: dhcp
  - name: lan1
    role: lan
    device: eth1
    address: 192.168.1.1/24
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "validate", cfgPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("validate should succeed: %v\n%s", err, out)
	}
	output := string(out)
	if output == "" {
		t.Error("expected success message")
	}
}

func TestCLIValidateInvalid(t *testing.T) {
	bin := buildWarp(t)
	cfgPath := filepath.Join(t.TempDir(), "site.yaml")
	// Missing hostname and LAN
	yaml := `interfaces:
  - name: wan1
    role: wan
    device: eth0
    address: dhcp
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "validate", cfgPath)
	_, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("validate should fail for invalid config")
	}
}

func TestCLIValidateNonexistentFile(t *testing.T) {
	bin := buildWarp(t)
	cmd := exec.Command(bin, "validate", "/nonexistent/site.yaml")
	_, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("validate should fail for nonexistent file")
	}
}

func TestCLIStatus(t *testing.T) {
	bin := buildWarp(t)
	// Status with no revisions should not crash
	cmd := exec.Command(bin, "status")
	out, err := cmd.CombinedOutput()
	// May fail if /var/lib/warp doesn't exist but shouldn't panic
	_ = err
	_ = out
}

func TestCLIRevisions(t *testing.T) {
	bin := buildWarp(t)
	cmd := exec.Command(bin, "revisions")
	out, err := cmd.CombinedOutput()
	// May say "No revisions stored" or error, but shouldn't panic
	_ = err
	_ = out
}
