package kea

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/internal/config"
)

func TestRenderDisabledDHCP(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	// Should produce valid JSON
	var parsed KeaConfig
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(parsed.Dhcp4.Subnet4) != 0 {
		t.Errorf("expected 0 subnets for disabled DHCP, got %d", len(parsed.Dhcp4.Subnet4))
	}
}

func TestRenderSingleSubnet(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
		DHCP: &config.DHCPConfig{
			Enabled: true,
			Subnets: []config.DHCPSubnet{
				{
					Subnet:     "192.168.1.0/24",
					Interface:  "lan1",
					PoolStart:  "192.168.1.100",
					PoolEnd:    "192.168.1.250",
					Gateway:    "192.168.1.1",
					DNSServers: []string{"192.168.1.1"},
					LeaseTime:  7200,
				},
			},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	var parsed KeaConfig
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(parsed.Dhcp4.Subnet4) != 1 {
		t.Fatalf("expected 1 subnet, got %d", len(parsed.Dhcp4.Subnet4))
	}

	sub := parsed.Dhcp4.Subnet4[0]
	if sub.Subnet != "192.168.1.0/24" {
		t.Errorf("subnet = %q, want %q", sub.Subnet, "192.168.1.0/24")
	}
	if sub.Interface != "eth1" {
		t.Errorf("interface = %q, want %q", sub.Interface, "eth1")
	}
	if len(sub.Pools) != 1 || sub.Pools[0].Pool != "192.168.1.100 - 192.168.1.250" {
		t.Errorf("pool = %v, want '192.168.1.100 - 192.168.1.250'", sub.Pools)
	}
	if sub.ValidLifetime != 7200 {
		t.Errorf("valid-lifetime = %d, want 7200", sub.ValidLifetime)
	}

	// Check options
	hasRouter := false
	hasDNS := false
	for _, opt := range sub.OptionData {
		if opt.Name == "routers" && opt.Data == "192.168.1.1" {
			hasRouter = true
		}
		if opt.Name == "domain-name-servers" && opt.Data == "192.168.1.1" {
			hasDNS = true
		}
	}
	if !hasRouter {
		t.Error("missing routers option")
	}
	if !hasDNS {
		t.Error("missing domain-name-servers option")
	}
}

func TestRenderMultipleSubnets(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
			{Name: "lan2", Role: "lan", Device: "eth2", Address: "10.10.0.1/24"},
		},
		DHCP: &config.DHCPConfig{
			Enabled: true,
			Subnets: []config.DHCPSubnet{
				{Subnet: "192.168.1.0/24", Interface: "lan1", PoolStart: "192.168.1.100", PoolEnd: "192.168.1.250", Gateway: "192.168.1.1"},
				{Subnet: "10.10.0.0/24", Interface: "lan2", PoolStart: "10.10.0.100", PoolEnd: "10.10.0.250", Gateway: "10.10.0.1"},
			},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	var parsed KeaConfig
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(parsed.Dhcp4.Subnet4) != 2 {
		t.Fatalf("expected 2 subnets, got %d", len(parsed.Dhcp4.Subnet4))
	}
	if parsed.Dhcp4.Subnet4[0].ID != 1 || parsed.Dhcp4.Subnet4[1].ID != 2 {
		t.Error("subnet IDs should be sequential starting from 1")
	}

	// Check listen interfaces
	ifaces := parsed.Dhcp4.InterfacesConfig.Interfaces
	if len(ifaces) != 2 {
		t.Fatalf("expected 2 listen interfaces, got %d", len(ifaces))
	}
	ifaceStr := strings.Join(ifaces, ",")
	if !strings.Contains(ifaceStr, "eth1") || !strings.Contains(ifaceStr, "eth2") {
		t.Errorf("listen interfaces = %v, want eth1 and eth2", ifaces)
	}
}

func TestRenderOutputIsValidJSON(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
		DHCP: &config.DHCPConfig{
			Enabled: true,
			Subnets: []config.DHCPSubnet{
				{Subnet: "192.168.1.0/24", Interface: "lan1", PoolStart: "192.168.1.100", PoolEnd: "192.168.1.250", Gateway: "192.168.1.1"},
			},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !json.Valid([]byte(got)) {
		t.Error("output is not valid JSON")
	}
}
