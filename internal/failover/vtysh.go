package failover

import (
	"fmt"
	"os/exec"
	"sync"
)

// VtyshRouteManager implements RouteManager by configuring FRR via vtysh.
// This avoids conflicts with FRR's zebra route management (see AD-013).
// Instead of manipulating kernel routes directly via netlink, it adds/removes
// static routes through FRR's own CLI so that zebra remains the single owner
// of the kernel routing table.
type VtyshRouteManager struct {
	mu        sync.Mutex
	installed map[string]bool // "gateway device" → true

	// RunCmd allows injecting a custom command runner for testing.
	// If nil, uses real vtysh.
	RunCmd func(args ...string) ([]byte, error)
}

// NewVtyshRouteManager creates a route manager that drives FRR via vtysh.
// initialRoutes are nexthops already present in frr.conf (from warp apply);
// they are tracked as installed so the first ReplaceECMPRoute is a no-op.
func NewVtyshRouteManager(initialRoutes []Nexthop) *VtyshRouteManager {
	m := &VtyshRouteManager{
		installed: make(map[string]bool),
	}
	for _, nh := range initialRoutes {
		m.installed[nhKey(nh)] = true
	}
	return m
}

// ReplaceECMPRoute updates FRR's static routes to match the desired nexthop set.
// Routes no longer desired are removed; new routes are added.
func (m *VtyshRouteManager) ReplaceECMPRoute(nexthops []Nexthop) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	desired := make(map[string]Nexthop)
	for _, nh := range nexthops {
		desired[nhKey(nh)] = nh
	}

	// Remove routes no longer desired
	for key := range m.installed {
		if _, ok := desired[key]; !ok {
			if err := m.vtyshConfigure("no ip route 0.0.0.0/0 " + key); err != nil {
				return fmt.Errorf("removing route %s: %w", key, err)
			}
			delete(m.installed, key)
		}
	}

	// Add new routes
	for key := range desired {
		if !m.installed[key] {
			if err := m.vtyshConfigure("ip route 0.0.0.0/0 " + key); err != nil {
				return fmt.Errorf("adding route %s: %w", key, err)
			}
			m.installed[key] = true
		}
	}

	return nil
}

// AddPBRRule is a no-op — probe-based PBR failover is not yet supported.
// PBR maps are statically configured in frr.conf via warp apply.
// When a WAN fails probe checks but the link remains up, PBR rules still
// steer traffic toward the dead WAN. This is a known v1 limitation.
// FRR's NHT (nexthop tracking) handles carrier-loss failover natively.
func (m *VtyshRouteManager) AddPBRRule(rule PBRRule) error {
	return nil
}

// DelPBRRule is a no-op — probe-based PBR failover is not yet supported.
// See AddPBRRule for details on this v1 limitation.
func (m *VtyshRouteManager) DelPBRRule(rule PBRRule) error {
	return nil
}

// InstalledRoutes returns the set of routes currently tracked as installed (for testing).
func (m *VtyshRouteManager) InstalledRoutes() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]bool, len(m.installed))
	for k, v := range m.installed {
		cp[k] = v
	}
	return cp
}

func (m *VtyshRouteManager) vtyshConfigure(configCmd string) error {
	run := m.RunCmd
	if run == nil {
		run = func(args ...string) ([]byte, error) {
			return exec.Command("vtysh", args...).CombinedOutput()
		}
	}
	out, err := run("-c", "configure terminal", "-c", configCmd)
	if err != nil {
		return fmt.Errorf("vtysh %q: %w\noutput: %s", configCmd, err, out)
	}
	return nil
}

func nhKey(nh Nexthop) string {
	return nh.Gateway.String() + " " + nh.Device
}
