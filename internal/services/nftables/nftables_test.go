package nftables

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/internal/config"
)

func TestRenderBasicNAT(t *testing.T) {
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

	// Should have masquerade on WAN
	if !strings.Contains(got, `oifname "eth0" masquerade`) {
		t.Error("missing masquerade rule for WAN eth0")
	}
	// Should allow forwarding from LAN (no firewall config)
	if !strings.Contains(got, `iifname "eth1" accept`) {
		t.Error("missing LAN forward accept rule")
	}
	// Should have default drop policies
	if !strings.Contains(got, "policy drop") {
		t.Error("missing default drop policy")
	}
}

func TestRenderDualWANMasquerade(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "wan2", Role: "wan", Device: "eth1", Address: "203.0.113.2/30"},
			{Name: "lan1", Role: "lan", Device: "eth2", Address: "192.168.1.1/24"},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(got, `oifname "eth0" masquerade`) {
		t.Error("missing masquerade for eth0")
	}
	if !strings.Contains(got, `oifname "eth1" masquerade`) {
		t.Error("missing masquerade for eth1")
	}
}

func TestRenderWithFirewallZones(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
		Firewall: &config.Firewall{
			Zones: []config.FirewallZone{
				{Name: "wan", Interfaces: []string{"wan1"}},
				{Name: "lan", Interfaces: []string{"lan1"}},
			},
			ForwardRules: []config.ForwardRule{
				{From: "lan", To: "wan", Action: "accept"},
				{From: "wan", To: "lan", Action: "drop"},
			},
			InputRules: []config.InputRule{
				{Zone: "lan", Action: "accept", Protocol: "tcp", Port: "22"},
			},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	// Forward: LAN → WAN accept
	if !strings.Contains(got, `iifname "eth1" oifname "eth0" accept`) {
		t.Error("missing LAN→WAN forward accept rule")
	}
	// Forward: WAN → LAN drop
	if !strings.Contains(got, `iifname "eth0" oifname "eth1" drop`) {
		t.Error("missing WAN→LAN forward drop rule")
	}
	// Input: SSH from LAN
	if !strings.Contains(got, `iifname "eth1" ip protocol tcp tcp dport 22 accept`) {
		t.Error("missing SSH input rule from LAN")
	}
}

func TestRenderStructure(t *testing.T) {
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

	requiredElements := []string{
		"flush ruleset",
		"table inet filter",
		"chain input",
		"chain forward",
		"chain output",
		"table inet nat",
		"chain postrouting",
		"ct state established,related accept",
	}

	for _, elem := range requiredElements {
		if !strings.Contains(got, elem) {
			t.Errorf("missing required element: %q", elem)
		}
	}
}
