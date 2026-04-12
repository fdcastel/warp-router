package failover

import (
	"net"
	"sort"
	"testing"

	"github.com/fdcastel/warp-router/internal/config"
	"github.com/fdcastel/warp-router/internal/health"
)

// fakeRouteManager records route operations for testing.
type fakeRouteManager struct {
	ecmpCalls  [][]Nexthop
	pbrAdded   []PBRRule
	pbrRemoved []PBRRule
}

func (f *fakeRouteManager) ReplaceECMPRoute(nexthops []Nexthop) error {
	f.ecmpCalls = append(f.ecmpCalls, nexthops)
	return nil
}

func (f *fakeRouteManager) AddPBRRule(rule PBRRule) error {
	f.pbrAdded = append(f.pbrAdded, rule)
	return nil
}

func (f *fakeRouteManager) DelPBRRule(rule PBRRule) error {
	f.pbrRemoved = append(f.pbrRemoved, rule)
	return nil
}

func dualWANCfg() *config.SiteConfig {
	return &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp", Gateway: "10.0.0.1", Weight: 1},
			{Name: "wan2", Role: "wan", Device: "eth1", Address: "203.0.113.2/30", Gateway: "203.0.113.1", Weight: 1},
			{Name: "lan1", Role: "lan", Device: "eth2", Address: "192.168.1.1/24"},
		},
	}
}

func TestControllerInitialECMP(t *testing.T) {
	routes := &fakeRouteManager{}
	prober := health.NewProber()
	cfg := dualWANCfg()

	ctrl := NewController(cfg, routes, prober)
	if err := ctrl.InstallInitialRoutes(); err != nil {
		t.Fatalf("InstallInitialRoutes: %v", err)
	}

	if len(routes.ecmpCalls) != 1 {
		t.Fatalf("expected 1 ECMP call, got %d", len(routes.ecmpCalls))
	}

	nexthops := routes.ecmpCalls[0]
	if len(nexthops) != 2 {
		t.Fatalf("expected 2 nexthops, got %d", len(nexthops))
	}

	// Sort by device for deterministic comparison
	sort.Slice(nexthops, func(i, j int) bool {
		return nexthops[i].Device < nexthops[j].Device
	})

	if nexthops[0].Device != "eth0" || nexthops[0].Gateway.String() != "10.0.0.1" {
		t.Errorf("nexthop[0] = %s/%s, want eth0/10.0.0.1", nexthops[0].Device, nexthops[0].Gateway)
	}
	if nexthops[1].Device != "eth1" || nexthops[1].Gateway.String() != "203.0.113.1" {
		t.Errorf("nexthop[1] = %s/%s, want eth1/203.0.113.1", nexthops[1].Device, nexthops[1].Gateway)
	}
}

func TestControllerUplinkDown(t *testing.T) {
	routes := &fakeRouteManager{}
	prober := health.NewProber()
	cfg := dualWANCfg()

	ctrl := NewController(cfg, routes, prober)
	ctrl.InstallInitialRoutes()

	// Simulate wan1 going down
	ctrl.HandleStateChange("wan1", health.StatusHealthy, health.StatusDown)

	// Last ECMP call should only have wan2
	lastCall := routes.ecmpCalls[len(routes.ecmpCalls)-1]
	if len(lastCall) != 1 {
		t.Fatalf("expected 1 nexthop after wan1 down, got %d", len(lastCall))
	}
	if lastCall[0].Device != "eth1" {
		t.Errorf("remaining nexthop device = %s, want eth1", lastCall[0].Device)
	}

	// Check active uplinks
	active := ctrl.ActiveUplinks()
	if len(active) != 1 {
		t.Errorf("active uplinks = %v, want [wan2]", active)
	}
}

func TestControllerUplinkRecovery(t *testing.T) {
	routes := &fakeRouteManager{}
	prober := health.NewProber()
	cfg := dualWANCfg()

	ctrl := NewController(cfg, routes, prober)
	ctrl.InstallInitialRoutes()

	// Down then recover
	ctrl.HandleStateChange("wan1", health.StatusHealthy, health.StatusDown)
	ctrl.HandleStateChange("wan1", health.StatusDown, health.StatusHealthy)

	// Last ECMP call should have both back
	lastCall := routes.ecmpCalls[len(routes.ecmpCalls)-1]
	if len(lastCall) != 2 {
		t.Fatalf("expected 2 nexthops after recovery, got %d", len(lastCall))
	}
}

func TestControllerBothDown(t *testing.T) {
	routes := &fakeRouteManager{}
	prober := health.NewProber()
	cfg := dualWANCfg()

	ctrl := NewController(cfg, routes, prober)
	ctrl.InstallInitialRoutes()

	ctrl.HandleStateChange("wan1", health.StatusHealthy, health.StatusDown)
	ctrl.HandleStateChange("wan2", health.StatusHealthy, health.StatusDown)

	lastCall := routes.ecmpCalls[len(routes.ecmpCalls)-1]
	if len(lastCall) != 0 {
		t.Errorf("expected 0 nexthops when both down, got %d", len(lastCall))
	}

	active := ctrl.ActiveUplinks()
	if len(active) != 0 {
		t.Errorf("active uplinks = %v, want empty", active)
	}
}

func TestControllerPBRFailover(t *testing.T) {
	routes := &fakeRouteManager{}
	prober := health.NewProber()
	cfg := dualWANCfg()
	cfg.PBR = []config.PBRRule{
		{Name: "force-wan2", Priority: 100, Source: "192.168.1.0/24", Interface: "wan2"},
	}

	ctrl := NewController(cfg, routes, prober)
	if err := ctrl.InstallInitialRoutes(); err != nil {
		t.Fatalf("InstallInitialRoutes: %v", err)
	}

	// PBR rule should have been added
	if len(routes.pbrAdded) != 1 {
		t.Fatalf("expected 1 PBR rule added, got %d", len(routes.pbrAdded))
	}
	if routes.pbrAdded[0].Name != "force-wan2" {
		t.Errorf("PBR rule name = %q, want %q", routes.pbrAdded[0].Name, "force-wan2")
	}

	// wan2 goes down → PBR rule should be removed
	ctrl.HandleStateChange("wan2", health.StatusHealthy, health.StatusDown)
	if len(routes.pbrRemoved) != 1 {
		t.Fatalf("expected 1 PBR rule removed, got %d", len(routes.pbrRemoved))
	}
	if routes.pbrRemoved[0].Name != "force-wan2" {
		t.Errorf("removed PBR = %q, want %q", routes.pbrRemoved[0].Name, "force-wan2")
	}

	// wan2 recovers → PBR rule should be re-added
	oldAdded := len(routes.pbrAdded)
	ctrl.HandleStateChange("wan2", health.StatusDown, health.StatusHealthy)
	if len(routes.pbrAdded) != oldAdded+1 {
		t.Errorf("expected PBR rule re-added on recovery")
	}
}

func TestControllerSingleWAN(t *testing.T) {
	routes := &fakeRouteManager{}
	prober := health.NewProber()
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Interfaces: []config.Interface{
			{Name: "wan1", Role: "wan", Device: "eth0", Address: "dhcp", Gateway: "10.0.0.1"},
			{Name: "lan1", Role: "lan", Device: "eth1", Address: "192.168.1.1/24"},
		},
	}

	ctrl := NewController(cfg, routes, prober)
	if err := ctrl.InstallInitialRoutes(); err != nil {
		t.Fatalf("InstallInitialRoutes: %v", err)
	}

	nexthops := routes.ecmpCalls[0]
	if len(nexthops) != 1 {
		t.Fatalf("expected 1 nexthop for single WAN, got %d", len(nexthops))
	}
	if nexthops[0].Gateway.String() != "10.0.0.1" {
		t.Errorf("gateway = %s, want 10.0.0.1", nexthops[0].Gateway)
	}
}

func TestControllerDuplicateDown(t *testing.T) {
	routes := &fakeRouteManager{}
	prober := health.NewProber()
	cfg := dualWANCfg()

	ctrl := NewController(cfg, routes, prober)
	ctrl.InstallInitialRoutes()

	initialCalls := len(routes.ecmpCalls)
	ctrl.HandleStateChange("wan1", health.StatusHealthy, health.StatusDown)
	afterFirst := len(routes.ecmpCalls)
	ctrl.HandleStateChange("wan1", health.StatusDown, health.StatusDown)
	afterSecond := len(routes.ecmpCalls)

	// Second down should be a no-op
	if afterSecond != afterFirst {
		t.Errorf("duplicate down caused extra ECMP call: before=%d, after=%d", afterFirst, afterSecond)
	}
	_ = initialCalls
}

func TestNexthopEquality(t *testing.T) {
	nh := Nexthop{
		Gateway: net.ParseIP("10.0.0.1"),
		Device:  "eth0",
		Weight:  1,
	}
	if nh.Gateway.String() != "10.0.0.1" {
		t.Errorf("gateway = %s, want 10.0.0.1", nh.Gateway)
	}
}
