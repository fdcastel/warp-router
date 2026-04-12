package frr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/internal/config"
)

func TestRenderSingleWAN(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "router01",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp", Gateway: "10.0.0.1"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	assertGolden(t, "single-wan.golden", got)
}

func TestRenderDualWANECMP(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "gw-dual",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp", Gateway: "10.0.0.1", Weight: 1},
			{Name: "wan2", Role: "wan", Device: "eth1", Address: "203.0.113.2/30", Gateway: "203.0.113.1", Weight: 1},
			{Name: "lan1", Role: "lan", Device: "eth2", Address: "192.168.1.1/24"},
		},
		ECMP: &config.ECMPConfig{Enabled: true},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	assertGolden(t, "dual-wan-ecmp.golden", got)
}

func TestRenderContainsPBR(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "gw-pbr",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp", Gateway: "10.0.0.1"},
			{Name: "wan2", Role: "wan", Device: "eth1", Address: "203.0.113.2/30", Gateway: "203.0.113.1"},
			{Name: "lan1", Role: "lan", Device: "eth2", Address: "192.168.1.1/24"},
		},
		PBR: []config.PBRRule{
			{Name: "lan1-via-wan2", Priority: 100, Source: "192.168.1.0/24", Interface: "wan2"},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(got, "pbr-map lan1-via-wan2") {
		t.Error("output does not contain PBR map")
	}
	if !strings.Contains(got, "match src-ip 192.168.1.0/24") {
		t.Error("output does not contain PBR match")
	}
	if !strings.Contains(got, "set nexthop 203.0.113.1") {
		t.Error("output does not contain PBR nexthop")
	}
	if !strings.Contains(got, "pbr-policy lan1-via-wan2") {
		t.Error("output does not contain PBR policy attachment")
	}
}

func TestRenderNoGatewaySkipsBFD(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "gw-nogate",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if strings.Contains(got, "bfd") {
		t.Error("output should not contain BFD when no gateway is set")
	}
}

func assertGolden(t *testing.T, name string, got string) {
	t.Helper()
	goldenPath := filepath.Join("testdata", name)
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file %s: %v", goldenPath, err)
	}
	if got != string(expected) {
		t.Errorf("output does not match golden file %s.\n--- GOT ---\n%s\n--- EXPECTED ---\n%s", name, got, string(expected))
	}
}
