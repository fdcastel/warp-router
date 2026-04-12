package kea

import (
	"encoding/json"
	"fmt"

	"github.com/fdcastel/warp-router/internal/config"
)

// KeaConfig represents the top-level Kea DHCPv4 configuration.
type KeaConfig struct {
	Dhcp4 Dhcp4Config `json:"Dhcp4"`
}

// Dhcp4Config represents the Dhcp4 section.
type Dhcp4Config struct {
	InterfacesConfig InterfacesConfig `json:"interfaces-config"`
	ControlSocket    ControlSocket    `json:"control-socket"`
	LeaseDatabase    LeaseDatabase    `json:"lease-database"`
	ValidLifetime    int              `json:"valid-lifetime"`
	RenewTimer       int              `json:"renew-timer"`
	RebindTimer      int              `json:"rebind-timer"`
	Subnet4          []Subnet4        `json:"subnet4"`
	Loggers          []Logger         `json:"loggers"`
}

// InterfacesConfig specifies which interfaces Kea listens on.
type InterfacesConfig struct {
	Interfaces []string `json:"interfaces"`
}

// ControlSocket configures the Kea control socket.
type ControlSocket struct {
	SocketType string `json:"socket-type"`
	SocketName string `json:"socket-name"`
}

// LeaseDatabase configures the lease storage.
type LeaseDatabase struct {
	Type        string `json:"type"`
	Persist     bool   `json:"persist"`
	Name        string `json:"name"`
	LFCInterval int    `json:"lfc-interval"`
}

// Subnet4 represents a DHCPv4 subnet definition.
type Subnet4 struct {
	ID            int           `json:"id"`
	Subnet        string        `json:"subnet"`
	Pools         []Pool        `json:"pools"`
	OptionData    []OptionData  `json:"option-data,omitempty"`
	ValidLifetime int           `json:"valid-lifetime,omitempty"`
	Interface     string        `json:"interface,omitempty"`
}

// Pool defines an IP address pool range.
type Pool struct {
	Pool string `json:"pool"`
}

// OptionData represents a DHCP option.
type OptionData struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

// Logger configures Kea logging.
type Logger struct {
	Name          string         `json:"name"`
	OutputOptions []OutputOption `json:"output-options"`
	Severity      string         `json:"severity"`
}

// OutputOption configures log output destination.
type OutputOption struct {
	Output string `json:"output"`
}

// Render generates kea-dhcp4.conf JSON from the site config.
func Render(cfg *config.SiteConfig) (string, error) {
	if cfg.DHCP == nil || !cfg.DHCP.Enabled {
		return renderDefault()
	}

	// Build interface name → device mapping
	ifaceDevice := make(map[string]string)
	for _, iface := range cfg.Interfaces {
		ifaceDevice[iface.Name] = iface.Device
	}

	// Collect LAN interfaces for Kea to listen on
	var listenInterfaces []string
	for _, sub := range cfg.DHCP.Subnets {
		if dev, ok := ifaceDevice[sub.Interface]; ok {
			listenInterfaces = append(listenInterfaces, dev)
		}
	}

	defaultLeaseTime := 3600
	keaCfg := KeaConfig{
		Dhcp4: Dhcp4Config{
			InterfacesConfig: InterfacesConfig{
				Interfaces: listenInterfaces,
			},
			ControlSocket: ControlSocket{
				SocketType: "unix",
				SocketName: "/run/kea/kea4-ctrl-socket",
			},
			LeaseDatabase: LeaseDatabase{
				Type:        "memfile",
				Persist:     true,
				Name:        "/var/lib/kea/kea-leases4.csv",
				LFCInterval: 3600,
			},
			ValidLifetime: defaultLeaseTime,
			RenewTimer:    defaultLeaseTime / 2,
			RebindTimer:   defaultLeaseTime * 7 / 8,
			Loggers: []Logger{
				{
					Name:          "kea-dhcp4",
					OutputOptions: []OutputOption{{Output: "syslog"}},
					Severity:      "INFO",
				},
			},
		},
	}

	for i, sub := range cfg.DHCP.Subnets {
		subnet := Subnet4{
			ID:     i + 1,
			Subnet: sub.Subnet,
			Pools: []Pool{
				{Pool: fmt.Sprintf("%s - %s", sub.PoolStart, sub.PoolEnd)},
			},
		}

		if dev, ok := ifaceDevice[sub.Interface]; ok {
			subnet.Interface = dev
		}

		if sub.LeaseTime > 0 {
			subnet.ValidLifetime = sub.LeaseTime
		}

		// Option: routers (gateway)
		if sub.Gateway != "" {
			subnet.OptionData = append(subnet.OptionData, OptionData{
				Name: "routers",
				Data: sub.Gateway,
			})
		}

		// Option: DNS servers
		if len(sub.DNSServers) > 0 {
			dnsStr := ""
			for j, dns := range sub.DNSServers {
				if j > 0 {
					dnsStr += ", "
				}
				dnsStr += dns
			}
			subnet.OptionData = append(subnet.OptionData, OptionData{
				Name: "domain-name-servers",
				Data: dnsStr,
			})
		}

		keaCfg.Dhcp4.Subnet4 = append(keaCfg.Dhcp4.Subnet4, subnet)
	}

	data, err := json.MarshalIndent(keaCfg, "", "    ")
	if err != nil {
		return "", fmt.Errorf("marshaling Kea config: %w", err)
	}

	return string(data) + "\n", nil
}

func renderDefault() (string, error) {
	keaCfg := KeaConfig{
		Dhcp4: Dhcp4Config{
			InterfacesConfig: InterfacesConfig{
				Interfaces: []string{},
			},
			ControlSocket: ControlSocket{
				SocketType: "unix",
				SocketName: "/run/kea/kea4-ctrl-socket",
			},
			LeaseDatabase: LeaseDatabase{
				Type:        "memfile",
				Persist:     true,
				Name:        "/var/lib/kea/kea-leases4.csv",
				LFCInterval: 3600,
			},
			ValidLifetime: 3600,
			RenewTimer:    1800,
			RebindTimer:   3150,
			Subnet4:       []Subnet4{},
			Loggers: []Logger{
				{
					Name:          "kea-dhcp4",
					OutputOptions: []OutputOption{{Output: "syslog"}},
					Severity:      "INFO",
				},
			},
		},
	}

	data, err := json.MarshalIndent(keaCfg, "", "    ")
	if err != nil {
		return "", fmt.Errorf("marshaling default Kea config: %w", err)
	}

	return string(data) + "\n", nil
}
