package config

import (
	"strings"
	"testing"
)

func TestValidateMinimalValid(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "router01",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
	}
	errs := cfg.Validate()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateMissingHostname(t *testing.T) {
	cfg := &SiteConfig{
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "hostname is required")
}

func TestValidateNoInterfaces(t *testing.T) {
	cfg := &SiteConfig{Hostname: "r1"}
	errs := cfg.Validate()
	assertContainsError(t, errs, "at least one interface is required")
}

func TestValidateMissingWAN(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "lan1", Role: "lan", Device: "eth0", Address: "192.168.1.1/24"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "at least one WAN interface is required")
}

func TestValidateMissingLAN(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "at least one LAN interface is required")
}

func TestValidateDuplicateInterfaceName(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "wan1", Role: "lan", Device: "eth1", Address: "10.0.0.1/24"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "duplicate interface name")
}

func TestValidateDuplicateDevice(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth0", Address: "10.0.0.1/24"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "duplicate device")
}

func TestValidateInvalidRole(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "dmz1", Role: "dmz", Device: "eth1", Address: "10.0.0.1/24"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "role must be")
}

func TestValidateInvalidCIDR(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "not-a-cidr"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "invalid CIDR")
}

func TestValidateInvalidGateway(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp", Gateway: "not-an-ip"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "10.0.0.1/24"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "invalid gateway IP")
}

func TestValidateInvalidVLAN(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "10.0.0.1/24", VLAN: 5000},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "VLAN ID must be")
}

func TestValidateInvalidMTU(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "10.0.0.1/24", MTU: 100},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "MTU must be")
}

func TestValidateOverlappingSubnets(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
			{Name: "lan2", Role: "lan", Device: "eth2", Address: "192.168.1.100/24"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "overlapping subnets")
}

func TestValidateNonOverlappingSubnets(t *testing.T) {
	cfg := &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
			{Name: "lan2", Role: "lan", Device: "eth2", Address: "10.0.0.1/24"},
		},
	}
	errs := cfg.Validate()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateDHCPInvalidSubnet(t *testing.T) {
	cfg := validBase()
	cfg.DHCP = &DHCPConfig{
		Enabled: true,
		Subnets: []DHCPSubnet{
			{Subnet: "bad", Interface: "lan1", PoolStart: "192.168.1.100", PoolEnd: "192.168.1.200", Gateway: "192.168.1.1"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "invalid subnet CIDR")
}

func TestValidateDHCPUndefinedInterface(t *testing.T) {
	cfg := validBase()
	cfg.DHCP = &DHCPConfig{
		Enabled: true,
		Subnets: []DHCPSubnet{
			{Subnet: "192.168.1.0/24", Interface: "nonexistent", PoolStart: "192.168.1.100", PoolEnd: "192.168.1.200", Gateway: "192.168.1.1"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "interface \"nonexistent\" not defined")
}

func TestValidateDHCPMissingPoolStart(t *testing.T) {
	cfg := validBase()
	cfg.DHCP = &DHCPConfig{
		Enabled: true,
		Subnets: []DHCPSubnet{
			{Subnet: "192.168.1.0/24", Interface: "lan1", PoolEnd: "192.168.1.200", Gateway: "192.168.1.1"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "pool_start is required")
}

func TestValidateDNSInvalidForwarder(t *testing.T) {
	cfg := validBase()
	cfg.DNS = &DNSConfig{
		Enabled:    true,
		Forwarders: []string{"not-an-ip"},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "invalid IP")
}

func TestValidateDNSInvalidAllowFrom(t *testing.T) {
	cfg := validBase()
	cfg.DNS = &DNSConfig{
		Enabled:   true,
		AllowFrom: []string{"bad-cidr"},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "invalid CIDR")
}

func TestValidateFirewallUndefinedZoneInterface(t *testing.T) {
	cfg := validBase()
	cfg.Firewall = &Firewall{
		Zones: []FirewallZone{
			{Name: "wan", Interfaces: []string{"nonexistent"}},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "interface \"nonexistent\" not defined")
}

func TestValidateFirewallUndefinedForwardZone(t *testing.T) {
	cfg := validBase()
	cfg.Firewall = &Firewall{
		Zones: []FirewallZone{
			{Name: "lan", Interfaces: []string{"lan1"}},
		},
		ForwardRules: []ForwardRule{
			{From: "lan", To: "wan", Action: "accept"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "to zone \"wan\" not defined")
}

func TestValidateFirewallInvalidAction(t *testing.T) {
	cfg := validBase()
	cfg.Firewall = &Firewall{
		Zones: []FirewallZone{
			{Name: "lan", Interfaces: []string{"lan1"}},
			{Name: "wan", Interfaces: []string{"wan1"}},
		},
		ForwardRules: []ForwardRule{
			{From: "lan", To: "wan", Action: "reject"},
		},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "action must be")
}

func TestValidatePBRUndefinedInterface(t *testing.T) {
	cfg := validBase()
	cfg.PBR = []PBRRule{
		{Name: "r1", Priority: 100, Source: "10.0.0.0/24", Interface: "nonexistent"},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "interface \"nonexistent\" not defined")
}

func TestValidatePBRInvalidSource(t *testing.T) {
	cfg := validBase()
	cfg.PBR = []PBRRule{
		{Name: "r1", Priority: 100, Source: "bad-cidr", Interface: "wan1"},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "invalid source CIDR")
}

func TestValidatePBRDuplicatePriority(t *testing.T) {
	cfg := validBase()
	cfg.PBR = []PBRRule{
		{Name: "r1", Priority: 100, Source: "10.0.0.0/24", Interface: "wan1"},
		{Name: "r2", Priority: 100, Source: "10.1.0.0/24", Interface: "wan1"},
	}
	errs := cfg.Validate()
	assertContainsError(t, errs, "duplicate priority")
}

// --- Helpers ---

func validBase() *SiteConfig {
	return &SiteConfig{
		Hostname: "r1",
		Interfaces: []Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
	}
}

func assertContainsError(t *testing.T, errs []error, substring string) {
	t.Helper()
	for _, err := range errs {
		if strings.Contains(err.Error(), substring) {
			return
		}
	}
	t.Errorf("expected error containing %q, got: %v", substring, errs)
}
