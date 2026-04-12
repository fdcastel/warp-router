package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMinimalConfig(t *testing.T) {
	yaml := `
hostname: router01
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
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.Hostname != "router01" {
		t.Errorf("hostname = %q, want %q", cfg.Hostname, "router01")
	}
	if len(cfg.Interfaces) != 2 {
		t.Fatalf("interfaces count = %d, want 2", len(cfg.Interfaces))
	}
	if cfg.Interfaces[0].Role != "wan" {
		t.Errorf("interfaces[0].role = %q, want %q", cfg.Interfaces[0].Role, "wan")
	}
	if cfg.Interfaces[1].Address != "192.168.1.1/24" {
		t.Errorf("interfaces[1].address = %q, want %q", cfg.Interfaces[1].Address, "192.168.1.1/24")
	}
}

func TestParseFullConfig(t *testing.T) {
	yaml := `
hostname: gw-site-a
interfaces:
  - name: wan1
    role: wan
    device: eth0
    address: dhcp
    gateway: 10.0.0.1
    weight: 1
    health_check:
      target: 8.8.8.8
      interval: 1
      timeout: 2
      failures: 3
  - name: wan2
    role: wan
    device: eth1
    address: 203.0.113.2/30
    gateway: 203.0.113.1
    weight: 1
    health_check:
      target: 1.1.1.1
  - name: lan1
    role: lan
    device: eth2
    address: 192.168.1.1/24
  - name: lan2
    role: lan
    device: eth3.100
    address: 10.10.0.1/24
    vlan: 100
    mtu: 1500
dhcp:
  enabled: true
  subnets:
    - subnet: 192.168.1.0/24
      interface: lan1
      pool_start: 192.168.1.100
      pool_end: 192.168.1.250
      gateway: 192.168.1.1
      dns_servers:
        - 192.168.1.1
      lease_time: 7200
    - subnet: 10.10.0.0/24
      interface: lan2
      pool_start: 10.10.0.100
      pool_end: 10.10.0.250
      gateway: 10.10.0.1
dns:
  enabled: true
  forwarders:
    - 1.1.1.1
    - 8.8.8.8
firewall:
  zones:
    - name: wan
      interfaces: [wan1, wan2]
    - name: lan
      interfaces: [lan1, lan2]
  forward_rules:
    - from: lan
      to: wan
      action: accept
    - from: wan
      to: lan
      action: drop
  input_rules:
    - zone: lan
      action: accept
      protocol: tcp
      port: "22"
ecmp:
  enabled: true
pbr:
  - name: lan2-via-wan2
    priority: 100
    source: 10.10.0.0/24
    interface: wan2
    table: 200
sysctl:
  conntrack_max: 524288
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Interfaces
	if len(cfg.Interfaces) != 4 {
		t.Fatalf("interfaces count = %d, want 4", len(cfg.Interfaces))
	}
	wan1 := cfg.Interfaces[0]
	if wan1.HealthCheck == nil {
		t.Fatal("wan1.health_check is nil")
	}
	if wan1.HealthCheck.Target != "8.8.8.8" {
		t.Errorf("wan1.health_check.target = %q, want %q", wan1.HealthCheck.Target, "8.8.8.8")
	}
	if wan1.HealthCheck.Failures != 3 {
		t.Errorf("wan1.health_check.failures = %d, want 3", wan1.HealthCheck.Failures)
	}

	lan2 := cfg.Interfaces[3]
	if lan2.VLAN != 100 {
		t.Errorf("lan2.vlan = %d, want 100", lan2.VLAN)
	}

	// DHCP
	if cfg.DHCP == nil || !cfg.DHCP.Enabled {
		t.Fatal("dhcp not enabled")
	}
	if len(cfg.DHCP.Subnets) != 2 {
		t.Fatalf("dhcp.subnets count = %d, want 2", len(cfg.DHCP.Subnets))
	}
	if cfg.DHCP.Subnets[0].PoolStart != "192.168.1.100" {
		t.Errorf("dhcp.subnets[0].pool_start = %q, want %q", cfg.DHCP.Subnets[0].PoolStart, "192.168.1.100")
	}

	// DNS
	if cfg.DNS == nil || !cfg.DNS.Enabled {
		t.Fatal("dns not enabled")
	}
	if len(cfg.DNS.Forwarders) != 2 {
		t.Errorf("dns.forwarders count = %d, want 2", len(cfg.DNS.Forwarders))
	}

	// Firewall
	if cfg.Firewall == nil {
		t.Fatal("firewall is nil")
	}
	if len(cfg.Firewall.Zones) != 2 {
		t.Errorf("firewall.zones count = %d, want 2", len(cfg.Firewall.Zones))
	}
	if len(cfg.Firewall.ForwardRules) != 2 {
		t.Errorf("firewall.forward_rules count = %d, want 2", len(cfg.Firewall.ForwardRules))
	}

	// ECMP
	if cfg.ECMP == nil || !cfg.ECMP.Enabled {
		t.Fatal("ecmp not enabled")
	}

	// PBR
	if len(cfg.PBR) != 1 {
		t.Fatalf("pbr count = %d, want 1", len(cfg.PBR))
	}
	if cfg.PBR[0].Source != "10.10.0.0/24" {
		t.Errorf("pbr[0].source = %q, want %q", cfg.PBR[0].Source, "10.10.0.0/24")
	}

	// Sysctl
	if cfg.Sysctl == nil || cfg.Sysctl.ConntrackMax != 524288 {
		t.Errorf("sysctl.conntrack_max = %d, want 524288", cfg.Sysctl.ConntrackMax)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.yaml")
	content := `
hostname: test-router
interfaces:
  - name: wan1
    role: wan
    device: eth0
    address: dhcp
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile error: %v", err)
	}
	if cfg.Hostname != "test-router" {
		t.Errorf("hostname = %q, want %q", cfg.Hostname, "test-router")
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/site.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParseInvalidYAML(t *testing.T) {
	_, err := Parse([]byte("{{{{invalid yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
