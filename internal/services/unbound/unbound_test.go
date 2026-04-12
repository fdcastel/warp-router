package unbound

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/internal/config"
)

func TestRenderBasic(t *testing.T) {
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

	// Should listen on localhost + LAN IP
	if !strings.Contains(got, "interface: 127.0.0.1") {
		t.Error("missing localhost listen")
	}
	if !strings.Contains(got, "interface: 192.168.1.1") {
		t.Error("missing LAN IP listen")
	}

	// Should allow LAN subnet
	if !strings.Contains(got, "access-control: 192.168.1.0/24 allow") {
		t.Error("missing LAN subnet access-control")
	}

	// No forwarders = full recursion (no forward-zone)
	if strings.Contains(got, "forward-zone") {
		t.Error("should not have forward-zone without forwarders")
	}
}

func TestRenderWithForwarders(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
		DNS: &config.DNSConfig{
			Enabled:    true,
			Forwarders: []string{"1.1.1.1", "8.8.8.8"},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(got, "forward-zone:") {
		t.Error("missing forward-zone")
	}
	if !strings.Contains(got, "forward-addr: 1.1.1.1") {
		t.Error("missing forwarder 1.1.1.1")
	}
	if !strings.Contains(got, "forward-addr: 8.8.8.8") {
		t.Error("missing forwarder 8.8.8.8")
	}
}

func TestRenderCustomListenAndAllowFrom(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
		DNS: &config.DNSConfig{
			Enabled:   true,
			Listen:    []string{"0.0.0.0"},
			AllowFrom: []string{"10.0.0.0/8"},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(got, "interface: 0.0.0.0") {
		t.Error("missing custom listen address")
	}
	if !strings.Contains(got, "access-control: 10.0.0.0/8 allow") {
		t.Error("missing custom allow_from")
	}
	// Should not have default LAN listen when custom is provided
	if strings.Contains(got, "interface: 192.168.1.1") {
		t.Error("should not have LAN IP when custom listen is set")
	}
}

func TestRenderMultipleLANs(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
			{Name: "lan2", Role: "lan", Device: "eth2", Address: "10.10.0.1/24"},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(got, "interface: 192.168.1.1") {
		t.Error("missing lan1 IP listen")
	}
	if !strings.Contains(got, "interface: 10.10.0.1") {
		t.Error("missing lan2 IP listen")
	}
	if !strings.Contains(got, "access-control: 192.168.1.0/24 allow") {
		t.Error("missing lan1 subnet access-control")
	}
	if !strings.Contains(got, "access-control: 10.10.0.0/24 allow") {
		t.Error("missing lan2 subnet access-control")
	}
}

func TestRenderContainsSecuritySettings(t *testing.T) {
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

	securitySettings := []string{
		"hide-identity: yes",
		"hide-version: yes",
		"harden-glue: yes",
		"harden-dnssec-stripped: yes",
		"prefetch: yes",
	}
	for _, s := range securitySettings {
		if !strings.Contains(got, s) {
			t.Errorf("missing security setting: %q", s)
		}
	}
}
