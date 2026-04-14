package packaging_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageListExists verifies the package list file exists and contains expected packages.
func TestPackageListExists(t *testing.T) {
	data, err := os.ReadFile("packaging/rootfs/packages.list")
	if err != nil {
		t.Fatalf("failed to read packages.list: %v", err)
	}

	content := string(data)
	required := []string{"frr", "nftables", "kea-dhcp4-server", "unbound", "cloud-init", "openssh-server", "iproute2", "systemd"}
	for _, pkg := range required {
		if !strings.Contains(content, pkg) {
			t.Errorf("packages.list missing required package: %s", pkg)
		}
	}

	if !strings.Contains(content, "unattended-upgrades") {
		t.Error("packages.list missing required package: unattended-upgrades")
	}
}

// TestBuildScriptExists verifies the rootfs build script exists and is executable.
func TestBuildScriptExists(t *testing.T) {
	info, err := os.Stat("packaging/rootfs/build-rootfs.sh")
	if err != nil {
		t.Fatalf("build-rootfs.sh not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("build-rootfs.sh is not executable")
	}
}

// TestCustomizeHookExists verifies the customize hook exists and is executable.
func TestCustomizeHookExists(t *testing.T) {
	matches, err := filepath.Glob("packaging/rootfs/hooks/customize*.sh")
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no customize hooks found in packaging/rootfs/hooks/")
	}
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			t.Errorf("cannot stat %s: %v", m, err)
			continue
		}
		if info.Mode()&0111 == 0 {
			t.Errorf("hook %s is not executable", m)
		}
	}
}

// TestOverlayStructure verifies all required overlay files exist.
func TestOverlayStructure(t *testing.T) {
	requiredFiles := []string{
		"packaging/rootfs/overlay/etc/sysctl.d/90-warp-router.conf",
		"packaging/rootfs/overlay/etc/frr/daemons",
		"packaging/rootfs/overlay/etc/frr/frr.conf",
		"packaging/rootfs/overlay/etc/nftables.conf",
		"packaging/rootfs/overlay/etc/kea/kea-dhcp4.conf",
		"packaging/rootfs/overlay/etc/unbound/unbound.conf.d/warp-router.conf",
		"packaging/rootfs/overlay/etc/ssh/sshd_config.d/99-warp-router.conf",
		"packaging/rootfs/overlay/etc/apt/apt.conf.d/20auto-upgrades",
		"packaging/rootfs/overlay/etc/apt/apt.conf.d/52warp-security-upgrades",
	}

	for _, f := range requiredFiles {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing overlay file: %s", f)
		}
	}
}

// TestSysctlIPForwarding verifies sysctl overlay enables IP forwarding.
func TestSysctlIPForwarding(t *testing.T) {
	data, err := os.ReadFile("packaging/rootfs/overlay/etc/sysctl.d/90-warp-router.conf")
	if err != nil {
		t.Fatalf("failed to read sysctl config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "net.ipv4.ip_forward = 1") {
		t.Error("sysctl config does not enable net.ipv4.ip_forward")
	}
	if !strings.Contains(content, "rp_filter = 2") {
		t.Error("sysctl config does not set rp_filter to loose mode (2)")
	}
}

// TestFRRDaemonsConfig verifies FRR daemons file enables required daemons.
func TestFRRDaemonsConfig(t *testing.T) {
	data, err := os.ReadFile("packaging/rootfs/overlay/etc/frr/daemons")
	if err != nil {
		t.Fatalf("failed to read FRR daemons config: %v", err)
	}

	content := string(data)
	requiredDaemons := map[string]string{
		"bgpd":    "bgpd=yes",
		"staticd": "staticd=yes",
		"bfdd":    "bfdd=yes",
		"pbrd":    "pbrd=yes",
	}
	for name, line := range requiredDaemons {
		if !strings.Contains(content, line) {
			t.Errorf("FRR daemons config does not enable %s (expected %q)", name, line)
		}
	}
}

// TestNftablesBaseRuleset verifies the nftables config has required chains.
func TestNftablesBaseRuleset(t *testing.T) {
	data, err := os.ReadFile("packaging/rootfs/overlay/etc/nftables.conf")
	if err != nil {
		t.Fatalf("failed to read nftables config: %v", err)
	}

	content := string(data)
	requiredElements := []string{
		"table inet filter",
		"chain input",
		"chain forward",
		"chain output",
		"table inet nat",
		"chain postrouting",
		"policy drop",
		"ct state established,related accept",
	}
	for _, elem := range requiredElements {
		if !strings.Contains(content, elem) {
			t.Errorf("nftables config missing: %q", elem)
		}
	}
}

// TestKeaSkeletonConfig verifies the Kea config is valid JSON with correct structure.
func TestKeaSkeletonConfig(t *testing.T) {
	data, err := os.ReadFile("packaging/rootfs/overlay/etc/kea/kea-dhcp4.conf")
	if err != nil {
		t.Fatalf("failed to read Kea config: %v", err)
	}

	var keaConfig map[string]interface{}
	if err := json.Unmarshal(data, &keaConfig); err != nil {
		t.Fatalf("Kea config is not valid JSON: %v", err)
	}

	if _, ok := keaConfig["Dhcp4"]; !ok {
		t.Error("Kea config missing 'Dhcp4' top-level key")
	}
}

// TestSSHHardening verifies SSH is configured for key-only access.
func TestSSHHardening(t *testing.T) {
	data, err := os.ReadFile("packaging/rootfs/overlay/etc/ssh/sshd_config.d/99-warp-router.conf")
	if err != nil {
		t.Fatalf("failed to read SSH config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "PasswordAuthentication no") {
		t.Error("SSH config does not disable password authentication")
	}
	if !strings.Contains(content, "PermitRootLogin prohibit-password") {
		t.Error("SSH config does not restrict root login to key-only")
	}
}

// TestUnattendedUpgradesConfig verifies security unattended upgrades are enabled.
func TestUnattendedUpgradesConfig(t *testing.T) {
	data, err := os.ReadFile("packaging/rootfs/overlay/etc/apt/apt.conf.d/20auto-upgrades")
	if err != nil {
		t.Fatalf("failed to read apt auto-upgrades config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "APT::Periodic::Update-Package-Lists \"1\"") {
		t.Error("20auto-upgrades does not enable package list refresh")
	}
	if !strings.Contains(content, "APT::Periodic::Unattended-Upgrade \"1\"") {
		t.Error("20auto-upgrades does not enable unattended upgrades")
	}

	policyData, err := os.ReadFile("packaging/rootfs/overlay/etc/apt/apt.conf.d/52warp-security-upgrades")
	if err != nil {
		t.Fatalf("failed to read apt security policy config: %v", err)
	}
	policy := string(policyData)
	if !strings.Contains(policy, "Origins-Pattern") {
		t.Error("52warp-security-upgrades missing Origins-Pattern")
	}
	if !strings.Contains(policy, "${distro_codename}-security") {
		t.Error("52warp-security-upgrades missing security pocket selector")
	}
}
