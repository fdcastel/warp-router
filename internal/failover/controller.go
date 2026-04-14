package failover

import (
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/fdcastel/warp-router/internal/config"
	"github.com/fdcastel/warp-router/internal/health"
)

// RouteManager defines the interface for managing kernel routes.
type RouteManager interface {
	// ReplaceECMPRoute replaces the default route with the given nexthops.
	// If nexthops is empty, removes the default route.
	ReplaceECMPRoute(nexthops []Nexthop) error

	// AddPBRRule adds a policy-based routing rule.
	AddPBRRule(rule PBRRule) error

	// DelPBRRule removes a policy-based routing rule.
	DelPBRRule(rule PBRRule) error
}

// Nexthop represents an ECMP nexthop.
type Nexthop struct {
	Gateway net.IP
	Device  string
	Weight  int
}

// PBRRule represents a policy-based routing rule.
type PBRRule struct {
	Name     string
	Priority int
	Source   net.IPNet
	Gateway  net.IP
	Device   string
}

// Controller monitors health state changes and adjusts routes accordingly.
type Controller struct {
	mu       sync.Mutex
	cfg      *config.SiteConfig
	routes   RouteManager
	prober   *health.Prober
	uplinks  map[string]uplinkInfo // uplink name → info
	pbrRules map[string]PBRRule    // PBR rule name → rule
	active   map[string]bool       // uplink name → currently active in ECMP
}

type uplinkInfo struct {
	Name    string
	Gateway net.IP
	Device  string
}

// NewController creates a failover controller.
func NewController(cfg *config.SiteConfig, routes RouteManager, prober *health.Prober) *Controller {
	c := &Controller{
		cfg:      cfg,
		routes:   routes,
		prober:   prober,
		uplinks:  make(map[string]uplinkInfo),
		pbrRules: make(map[string]PBRRule),
		active:   make(map[string]bool),
	}

	// Index WAN uplinks
	for _, iface := range cfg.Interfaces {
		if iface.Role == "wan" && iface.Gateway != "" {
			c.uplinks[iface.Name] = uplinkInfo{
				Name:    iface.Name,
				Gateway: net.ParseIP(iface.Gateway),
				Device:  iface.Device,
			}
			c.active[iface.Name] = true // start as active
		}
	}

	// Build PBR rules from config
	ifaceByName := make(map[string]config.Interface)
	for _, iface := range cfg.Interfaces {
		ifaceByName[iface.Name] = iface
	}
	for _, rule := range cfg.PBR {
		targetIface, ok := ifaceByName[rule.Interface]
		if !ok || targetIface.Gateway == "" {
			continue
		}
		_, srcNet, err := net.ParseCIDR(rule.Source)
		if err != nil {
			continue
		}
		c.pbrRules[rule.Name] = PBRRule{
			Name:     rule.Name,
			Priority: rule.Priority,
			Source:   *srcNet,
			Gateway:  net.ParseIP(targetIface.Gateway),
			Device:   targetIface.Device,
		}
	}

	return c
}

// HandleStateChange is called by the prober when a WAN uplink changes state.
func (c *Controller) HandleStateChange(name string, oldStatus, newStatus health.Status) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch newStatus {
	case health.StatusDown:
		c.deactivateUplink(name)
	case health.StatusHealthy:
		c.activateUplink(name)
	}
}

// InstallInitialRoutes sets up the initial ECMP and PBR routes.
func (c *Controller) InstallInitialRoutes() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.updateECMP(); err != nil {
		return fmt.Errorf("initial ECMP: %w", err)
	}

	for _, rule := range c.pbrRules {
		if err := c.routes.AddPBRRule(rule); err != nil {
			return fmt.Errorf("initial PBR rule %s: %w", rule.Name, err)
		}
	}

	return nil
}

func (c *Controller) deactivateUplink(name string) {
	if !c.active[name] {
		return
	}
	c.active[name] = false
	if err := c.updateECMP(); err != nil {
		log.Printf("failover: error updating ECMP after deactivating %s: %v", name, err)
		c.active[name] = true // revert in-memory state
		return
	}

	// Remove PBR rules targeting this uplink
	info, ok := c.uplinks[name]
	if !ok {
		return
	}
	for _, rule := range c.pbrRules {
		if rule.Device == info.Device {
			if err := c.routes.DelPBRRule(rule); err != nil {
				log.Printf("failover: error removing PBR rule %s: %v", rule.Name, err)
			}
		}
	}
}

func (c *Controller) activateUplink(name string) {
	if c.active[name] {
		return
	}
	c.active[name] = true
	if err := c.updateECMP(); err != nil {
		log.Printf("failover: error updating ECMP after activating %s: %v", name, err)
		c.active[name] = false // revert in-memory state
		return
	}

	// Restore PBR rules targeting this uplink
	info, ok := c.uplinks[name]
	if !ok {
		return
	}
	for _, rule := range c.pbrRules {
		if rule.Device == info.Device {
			if err := c.routes.AddPBRRule(rule); err != nil {
				log.Printf("failover: error adding PBR rule %s: %v", rule.Name, err)
			}
		}
	}
}

func (c *Controller) updateECMP() error {
	var nexthops []Nexthop
	for name, info := range c.uplinks {
		if c.active[name] {
			nexthops = append(nexthops, Nexthop{
				Gateway: info.Gateway,
				Device:  info.Device,
				Weight:  1,
			})
		}
	}
	return c.routes.ReplaceECMPRoute(nexthops)
}

// ActiveUplinks returns the names of currently active (in ECMP) uplinks.
func (c *Controller) ActiveUplinks() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var names []string
	for name, active := range c.active {
		if active {
			names = append(names, name)
		}
	}
	return names
}
