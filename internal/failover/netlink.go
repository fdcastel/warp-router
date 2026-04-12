package failover

import (
	"fmt"
	"net"
	"sync"

	"github.com/vishvananda/netlink"
)

// NetlinkRouteManager implements RouteManager using vishvananda/netlink.
type NetlinkRouteManager struct {
	mu sync.Mutex
}

// NewNetlinkRouteManager creates a route manager backed by the kernel routing table.
func NewNetlinkRouteManager() *NetlinkRouteManager {
	return &NetlinkRouteManager{}
}

// ReplaceECMPRoute replaces the default route with ECMP nexthops.
func (m *NetlinkRouteManager) ReplaceECMPRoute(nexthops []Nexthop) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, defaultDst, _ := net.ParseCIDR("0.0.0.0/0")

	if len(nexthops) == 0 {
		// Remove default route
		route := &netlink.Route{Dst: defaultDst}
		return netlink.RouteDel(route)
	}

	var nlNexthops []*netlink.NexthopInfo
	for _, nh := range nexthops {
		linkIdx := 0
		if nh.Device != "" {
			link, err := netlink.LinkByName(nh.Device)
			if err != nil {
				return fmt.Errorf("link %s: %w", nh.Device, err)
			}
			linkIdx = link.Attrs().Index
		}
		nlNexthops = append(nlNexthops, &netlink.NexthopInfo{
			LinkIndex: linkIdx,
			Hops:      nh.Weight - 1, // netlink uses 0-based weights
			Gw:        nh.Gateway,
		})
	}

	route := &netlink.Route{
		Dst:       defaultDst,
		MultiPath: nlNexthops,
	}

	return netlink.RouteReplace(route)
}

// AddPBRRule adds an ip rule for policy-based routing.
func (m *NetlinkRouteManager) AddPBRRule(rule PBRRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	nlRule := netlink.NewRule()
	nlRule.Priority = rule.Priority
	nlRule.Src = &rule.Source
	nlRule.Table = 100 + rule.Priority // Use priority-derived table

	// Also need to add a route in the corresponding table
	return netlink.RuleAdd(nlRule)
}

// DelPBRRule removes an ip rule.
func (m *NetlinkRouteManager) DelPBRRule(rule PBRRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	nlRule := netlink.NewRule()
	nlRule.Priority = rule.Priority
	nlRule.Src = &rule.Source
	nlRule.Table = 100 + rule.Priority

	return netlink.RuleDel(nlRule)
}
