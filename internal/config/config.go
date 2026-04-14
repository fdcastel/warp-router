package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SiteConfig is the top-level configuration for a Warp Router deployment.
type SiteConfig struct {
	Hostname   string      `yaml:"hostname"`
	Interfaces []Interface `yaml:"interfaces"`
	DHCP       *DHCPConfig `yaml:"dhcp,omitempty"`
	DNS        *DNSConfig  `yaml:"dns,omitempty"`
	Firewall   *Firewall   `yaml:"firewall,omitempty"`
	ECMP       *ECMPConfig `yaml:"ecmp,omitempty"`
	PBR        []PBRRule   `yaml:"pbr,omitempty"`
	Sysctl     *Sysctl     `yaml:"sysctl,omitempty"`
}

// Interface represents a network interface configuration.
type Interface struct {
	Name    string `yaml:"name"`
	Role    string `yaml:"role"`    // "wan" or "lan"
	Device  string `yaml:"device"`  // Linux device name (e.g. "eth0")
	Address string `yaml:"address"` // CIDR notation (e.g. "192.168.1.1/24") or "dhcp"
	Gateway string `yaml:"gateway,omitempty"`
	VLAN    int    `yaml:"vlan,omitempty"` // 802.1Q VLAN ID (0 = untagged)

	// WAN health check
	HealthCheck *HealthCheck `yaml:"health_check,omitempty"`
}

// HealthCheck configures WAN uplink health monitoring.
type HealthCheck struct {
	Target   string `yaml:"target"`             // IP to probe (default: gateway)
	Interval int    `yaml:"interval,omitempty"` // Probe interval in seconds (default: 1)
	Timeout  int    `yaml:"timeout,omitempty"`  // Probe timeout in seconds (default: 2)
	Failures int    `yaml:"failures,omitempty"` // Consecutive failures before marking down (default: 3)
}

// DHCPConfig configures the Kea DHCPv4 server.
type DHCPConfig struct {
	Enabled bool         `yaml:"enabled"`
	Subnets []DHCPSubnet `yaml:"subnets"`
}

// DHCPSubnet defines a DHCP scope for a LAN segment.
type DHCPSubnet struct {
	Subnet     string            `yaml:"subnet"`     // CIDR (e.g. "192.168.1.0/24")
	Interface  string            `yaml:"interface"`  // Interface name from interfaces list
	PoolStart  string            `yaml:"pool_start"` // First IP in pool
	PoolEnd    string            `yaml:"pool_end"`   // Last IP in pool
	Gateway    string            `yaml:"gateway"`    // Default gateway for clients
	DNSServers []string          `yaml:"dns_servers,omitempty"`
	LeaseTime  int               `yaml:"lease_time,omitempty"` // Seconds (default: 3600)
	Options    map[string]string `yaml:"options,omitempty"`
}

// DNSConfig configures the Unbound DNS resolver.
type DNSConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Listen     []string `yaml:"listen,omitempty"`     // Addresses to listen on (default: LAN IPs + 127.0.0.1)
	Forwarders []string `yaml:"forwarders,omitempty"` // Upstream DNS servers (empty = full recursion)
	AllowFrom  []string `yaml:"allow_from,omitempty"` // CIDRs allowed to query (default: LAN subnets)
}

// Firewall configures nftables zones and rules.
type Firewall struct {
	Zones        []FirewallZone `yaml:"zones,omitempty"`
	ForwardRules []ForwardRule  `yaml:"forward_rules,omitempty"`
	InputRules   []InputRule    `yaml:"input_rules,omitempty"`
}

// FirewallZone groups interfaces into a named zone.
type FirewallZone struct {
	Name       string   `yaml:"name"`
	Interfaces []string `yaml:"interfaces"`
}

// ForwardRule defines an inter-zone forwarding rule.
type ForwardRule struct {
	From     string `yaml:"from"`
	To       string `yaml:"to"`
	Action   string `yaml:"action"`             // "accept" or "drop"
	Protocol string `yaml:"protocol,omitempty"` // "tcp", "udp", "icmp", or empty for all
	Port     string `yaml:"port,omitempty"`     // Port or range (e.g. "80", "8000-9000")
	Source   string `yaml:"source,omitempty"`   // Source CIDR
	Dest     string `yaml:"dest,omitempty"`     // Destination CIDR
}

// InputRule defines a rule for traffic destined to the router itself.
type InputRule struct {
	Zone     string `yaml:"zone"`
	Action   string `yaml:"action"` // "accept" or "drop"
	Protocol string `yaml:"protocol,omitempty"`
	Port     string `yaml:"port,omitempty"`
	Source   string `yaml:"source,omitempty"`
}

// ECMPConfig configures equal-cost multi-path routing.
type ECMPConfig struct {
	Enabled bool `yaml:"enabled"` // Enable ECMP across WAN interfaces
}

// PBRRule defines a policy-based routing rule.
type PBRRule struct {
	Name      string `yaml:"name"`
	Priority  int    `yaml:"priority"`            // ip rule priority (lower = higher precedence)
	Source    string `yaml:"source,omitempty"`    // Source CIDR to match
	Interface string `yaml:"interface,omitempty"` // Target WAN interface name
}

// Sysctl allows overriding default sysctl settings.
type Sysctl struct {
	ConntrackMax int `yaml:"conntrack_max,omitempty"` // nf_conntrack_max (default: 262144)
}

// LoadFile reads and parses a YAML site config file.
func LoadFile(path string) (*SiteConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return Parse(data)
}

// Parse parses YAML bytes into a SiteConfig.
func Parse(data []byte) (*SiteConfig, error) {
	var cfg SiteConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	return &cfg, nil
}
