package config

import (
	"fmt"
	"net"
	"strings"
)

// Validate checks the SiteConfig for logical errors and returns all found issues.
func (c *SiteConfig) Validate() []error {
	var errs []error

	errs = append(errs, c.validateRequired()...)
	errs = append(errs, c.validateInterfaces()...)
	errs = append(errs, c.validateSubnetOverlaps()...)
	errs = append(errs, c.validateDHCP()...)
	errs = append(errs, c.validateDNS()...)
	errs = append(errs, c.validateFirewall()...)
	errs = append(errs, c.validatePBR()...)

	return errs
}

func (c *SiteConfig) validateRequired() []error {
	var errs []error
	if c.Hostname == "" {
		errs = append(errs, fmt.Errorf("hostname is required"))
	}
	if len(c.Interfaces) == 0 {
		errs = append(errs, fmt.Errorf("at least one interface is required"))
	}
	return errs
}

func (c *SiteConfig) validateInterfaces() []error {
	var errs []error
	names := make(map[string]bool)
	devices := make(map[string]bool)
	hasWAN := false
	hasLAN := false

	for i, iface := range c.Interfaces {
		prefix := fmt.Sprintf("interfaces[%d]", i)

		if iface.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required", prefix))
		} else if names[iface.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate interface name %q", prefix, iface.Name))
		} else {
			names[iface.Name] = true
		}

		if iface.Device == "" {
			errs = append(errs, fmt.Errorf("%s (%s): device is required", prefix, iface.Name))
		} else if devices[iface.Device] {
			errs = append(errs, fmt.Errorf("%s (%s): duplicate device %q", prefix, iface.Name, iface.Device))
		} else {
			devices[iface.Device] = true
		}

		switch iface.Role {
		case "wan":
			hasWAN = true
		case "lan":
			hasLAN = true
		default:
			errs = append(errs, fmt.Errorf("%s (%s): role must be \"wan\" or \"lan\", got %q", prefix, iface.Name, iface.Role))
		}

		if iface.Address == "" {
			errs = append(errs, fmt.Errorf("%s (%s): address is required", prefix, iface.Name))
		} else if iface.Address != "dhcp" {
			if _, _, err := net.ParseCIDR(iface.Address); err != nil {
				errs = append(errs, fmt.Errorf("%s (%s): invalid CIDR address %q: %v", prefix, iface.Name, iface.Address, err))
			}
		}

		if iface.Gateway != "" {
			if net.ParseIP(iface.Gateway) == nil {
				errs = append(errs, fmt.Errorf("%s (%s): invalid gateway IP %q", prefix, iface.Name, iface.Gateway))
			}
		}

		if iface.VLAN < 0 || iface.VLAN > 4094 {
			errs = append(errs, fmt.Errorf("%s (%s): VLAN ID must be 0-4094, got %d", prefix, iface.Name, iface.VLAN))
		}

		if iface.VLAN > 0 && !strings.Contains(iface.Device, ".") {
			errs = append(errs, fmt.Errorf("%s (%s): VLAN interfaces must use dotted device name (e.g., eth0.%d)", prefix, iface.Name, iface.VLAN))
		}

		if iface.MTU != 0 && (iface.MTU < 576 || iface.MTU > 9000) {
			errs = append(errs, fmt.Errorf("%s (%s): MTU must be 576-9000, got %d", prefix, iface.Name, iface.MTU))
		}
	}

	if !hasWAN {
		errs = append(errs, fmt.Errorf("at least one WAN interface is required"))
	}
	if !hasLAN {
		errs = append(errs, fmt.Errorf("at least one LAN interface is required"))
	}

	return errs
}

func (c *SiteConfig) validateSubnetOverlaps() []error {
	var errs []error
	type namedNet struct {
		name string
		net  *net.IPNet
	}
	var nets []namedNet

	for _, iface := range c.Interfaces {
		if iface.Address != "" && iface.Address != "dhcp" {
			_, ipnet, err := net.ParseCIDR(iface.Address)
			if err == nil {
				nets = append(nets, namedNet{name: iface.Name, net: ipnet})
			}
		}
	}

	for i := 0; i < len(nets); i++ {
		for j := i + 1; j < len(nets); j++ {
			if nets[i].net.Contains(nets[j].net.IP) || nets[j].net.Contains(nets[i].net.IP) {
				errs = append(errs, fmt.Errorf("overlapping subnets: %s (%s) and %s (%s)",
					nets[i].name, nets[i].net, nets[j].name, nets[j].net))
			}
		}
	}

	return errs
}

func (c *SiteConfig) validateDHCP() []error {
	if c.DHCP == nil || !c.DHCP.Enabled {
		return nil
	}
	var errs []error
	ifaceNames := c.interfaceNames()

	for i, sub := range c.DHCP.Subnets {
		prefix := fmt.Sprintf("dhcp.subnets[%d]", i)

		if _, _, err := net.ParseCIDR(sub.Subnet); err != nil {
			errs = append(errs, fmt.Errorf("%s: invalid subnet CIDR %q: %v", prefix, sub.Subnet, err))
		}

		if sub.Interface != "" && !ifaceNames[sub.Interface] {
			errs = append(errs, fmt.Errorf("%s: interface %q not defined", prefix, sub.Interface))
		}

		if sub.PoolStart == "" {
			errs = append(errs, fmt.Errorf("%s: pool_start is required", prefix))
		} else if net.ParseIP(sub.PoolStart) == nil {
			errs = append(errs, fmt.Errorf("%s: invalid pool_start IP %q", prefix, sub.PoolStart))
		}

		if sub.PoolEnd == "" {
			errs = append(errs, fmt.Errorf("%s: pool_end is required", prefix))
		} else if net.ParseIP(sub.PoolEnd) == nil {
			errs = append(errs, fmt.Errorf("%s: invalid pool_end IP %q", prefix, sub.PoolEnd))
		}

		if sub.Gateway == "" {
			errs = append(errs, fmt.Errorf("%s: gateway is required", prefix))
		} else if net.ParseIP(sub.Gateway) == nil {
			errs = append(errs, fmt.Errorf("%s: invalid gateway IP %q", prefix, sub.Gateway))
		}

		for j, dns := range sub.DNSServers {
			if net.ParseIP(dns) == nil {
				errs = append(errs, fmt.Errorf("%s: dns_servers[%d] invalid IP %q", prefix, j, dns))
			}
		}
	}
	return errs
}

func (c *SiteConfig) validateDNS() []error {
	if c.DNS == nil || !c.DNS.Enabled {
		return nil
	}
	var errs []error
	for i, fwd := range c.DNS.Forwarders {
		if net.ParseIP(fwd) == nil {
			errs = append(errs, fmt.Errorf("dns.forwarders[%d]: invalid IP %q", i, fwd))
		}
	}
	for i, cidr := range c.DNS.AllowFrom {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			errs = append(errs, fmt.Errorf("dns.allow_from[%d]: invalid CIDR %q: %v", i, cidr, err))
		}
	}
	return errs
}

func (c *SiteConfig) validateFirewall() []error {
	if c.Firewall == nil {
		return nil
	}
	var errs []error
	ifaceNames := c.interfaceNames()
	zoneNames := make(map[string]bool)

	for i, zone := range c.Firewall.Zones {
		prefix := fmt.Sprintf("firewall.zones[%d]", i)
		if zone.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required", prefix))
		} else {
			zoneNames[zone.Name] = true
		}
		for j, ifName := range zone.Interfaces {
			if !ifaceNames[ifName] {
				errs = append(errs, fmt.Errorf("%s.interfaces[%d]: interface %q not defined", prefix, j, ifName))
			}
		}
	}

	for i, rule := range c.Firewall.ForwardRules {
		prefix := fmt.Sprintf("firewall.forward_rules[%d]", i)
		if !zoneNames[rule.From] {
			errs = append(errs, fmt.Errorf("%s: from zone %q not defined", prefix, rule.From))
		}
		if !zoneNames[rule.To] {
			errs = append(errs, fmt.Errorf("%s: to zone %q not defined", prefix, rule.To))
		}
		if err := validateAction(rule.Action); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", prefix, err))
		}
	}

	for i, rule := range c.Firewall.InputRules {
		prefix := fmt.Sprintf("firewall.input_rules[%d]", i)
		if !zoneNames[rule.Zone] {
			errs = append(errs, fmt.Errorf("%s: zone %q not defined", prefix, rule.Zone))
		}
		if err := validateAction(rule.Action); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", prefix, err))
		}
	}

	return errs
}

func (c *SiteConfig) validatePBR() []error {
	var errs []error
	ifaceNames := c.interfaceNames()
	priorities := make(map[int]string)

	for i, rule := range c.PBR {
		prefix := fmt.Sprintf("pbr[%d]", i)
		if rule.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required", prefix))
		}
		if rule.Interface != "" && !ifaceNames[rule.Interface] {
			errs = append(errs, fmt.Errorf("%s (%s): interface %q not defined", prefix, rule.Name, rule.Interface))
		}
		if rule.Source != "" {
			if _, _, err := net.ParseCIDR(rule.Source); err != nil {
				errs = append(errs, fmt.Errorf("%s (%s): invalid source CIDR %q: %v", prefix, rule.Name, rule.Source, err))
			}
		}
		if existing, ok := priorities[rule.Priority]; ok {
			errs = append(errs, fmt.Errorf("%s (%s): duplicate priority %d (also used by %q)", prefix, rule.Name, rule.Priority, existing))
		} else {
			priorities[rule.Priority] = rule.Name
		}
	}
	return errs
}

func (c *SiteConfig) interfaceNames() map[string]bool {
	names := make(map[string]bool)
	for _, iface := range c.Interfaces {
		names[iface.Name] = true
	}
	return names
}

func validateAction(action string) error {
	action = strings.ToLower(action)
	if action != "accept" && action != "drop" {
		return fmt.Errorf("action must be \"accept\" or \"drop\", got %q", action)
	}
	return nil
}
