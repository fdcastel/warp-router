package failover

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestVtyshRouteManager_InitialState(t *testing.T) {
	initial := []Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
		{Gateway: net.ParseIP("203.0.113.1"), Device: "eth1", Weight: 1},
	}
	mgr := NewVtyshRouteManager(initial)

	installed := mgr.InstalledRoutes()
	if len(installed) != 2 {
		t.Fatalf("expected 2 installed routes, got %d", len(installed))
	}
	if !installed["10.0.0.1 eth0"] {
		t.Error("missing route 10.0.0.1 eth0")
	}
	if !installed["203.0.113.1 eth1"] {
		t.Error("missing route 203.0.113.1 eth1")
	}
}

func TestVtyshRouteManager_NoOpOnSameSet(t *testing.T) {
	var cmds []string
	initial := []Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
		{Gateway: net.ParseIP("203.0.113.1"), Device: "eth1", Weight: 1},
	}
	mgr := NewVtyshRouteManager(initial)
	mgr.RunCmd = func(args ...string) ([]byte, error) {
		cmds = append(cmds, strings.Join(args, " "))
		return nil, nil
	}

	// Replace with the same set — should be a no-op
	err := mgr.ReplaceECMPRoute(initial)
	if err != nil {
		t.Fatalf("ReplaceECMPRoute: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("expected 0 vtysh commands, got %d: %v", len(cmds), cmds)
	}
}

func TestVtyshRouteManager_RemoveOneNexhop(t *testing.T) {
	var cmds []string
	initial := []Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
		{Gateway: net.ParseIP("203.0.113.1"), Device: "eth1", Weight: 1},
	}
	mgr := NewVtyshRouteManager(initial)
	mgr.RunCmd = func(args ...string) ([]byte, error) {
		cmds = append(cmds, strings.Join(args, " "))
		return nil, nil
	}

	// Remove eth1 nexthop
	err := mgr.ReplaceECMPRoute([]Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceECMPRoute: %v", err)
	}

	if len(cmds) != 1 {
		t.Fatalf("expected 1 vtysh command, got %d: %v", len(cmds), cmds)
	}
	if !strings.Contains(cmds[0], "no ip route 0.0.0.0/0 203.0.113.1 eth1") {
		t.Errorf("expected 'no ip route' command, got: %s", cmds[0])
	}

	installed := mgr.InstalledRoutes()
	if len(installed) != 1 {
		t.Errorf("expected 1 installed route, got %d", len(installed))
	}
}

func TestVtyshRouteManager_AddNexhop(t *testing.T) {
	var cmds []string
	initial := []Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
	}
	mgr := NewVtyshRouteManager(initial)
	mgr.RunCmd = func(args ...string) ([]byte, error) {
		cmds = append(cmds, strings.Join(args, " "))
		return nil, nil
	}

	// Add a second nexthop
	err := mgr.ReplaceECMPRoute([]Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
		{Gateway: net.ParseIP("203.0.113.1"), Device: "eth1", Weight: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceECMPRoute: %v", err)
	}

	if len(cmds) != 1 {
		t.Fatalf("expected 1 vtysh command, got %d: %v", len(cmds), cmds)
	}
	if !strings.Contains(cmds[0], "ip route 0.0.0.0/0 203.0.113.1 eth1") {
		t.Errorf("expected 'ip route' command, got: %s", cmds[0])
	}
	// Should NOT contain "no" (add, not remove)
	if strings.Contains(cmds[0], "no ip route") {
		t.Errorf("should be add, not remove: %s", cmds[0])
	}
}

func TestVtyshRouteManager_RemoveAll(t *testing.T) {
	var cmds []string
	initial := []Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
		{Gateway: net.ParseIP("203.0.113.1"), Device: "eth1", Weight: 1},
	}
	mgr := NewVtyshRouteManager(initial)
	mgr.RunCmd = func(args ...string) ([]byte, error) {
		cmds = append(cmds, strings.Join(args, " "))
		return nil, nil
	}

	err := mgr.ReplaceECMPRoute(nil)
	if err != nil {
		t.Fatalf("ReplaceECMPRoute: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 remove commands, got %d: %v", len(cmds), cmds)
	}
	// Both should be "no ip route" commands
	sort.Strings(cmds)
	for _, cmd := range cmds {
		if !strings.Contains(cmd, "no ip route") {
			t.Errorf("expected 'no ip route' command, got: %s", cmd)
		}
	}

	installed := mgr.InstalledRoutes()
	if len(installed) != 0 {
		t.Errorf("expected 0 installed routes, got %d", len(installed))
	}
}

func TestVtyshRouteManager_ErrorPropagation(t *testing.T) {
	initial := []Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
		{Gateway: net.ParseIP("203.0.113.1"), Device: "eth1", Weight: 1},
	}
	mgr := NewVtyshRouteManager(initial)
	mgr.RunCmd = func(args ...string) ([]byte, error) {
		return []byte("error"), fmt.Errorf("vtysh failed")
	}

	err := mgr.ReplaceECMPRoute(nil) // Try to remove all
	if err == nil {
		t.Fatal("expected error from vtysh failure")
	}
	if !strings.Contains(err.Error(), "vtysh failed") {
		t.Errorf("error should mention vtysh failure: %v", err)
	}
}

func TestVtyshRouteManager_PBRNoOp(t *testing.T) {
	mgr := NewVtyshRouteManager(nil)

	// PBR operations should be no-ops for vtysh manager
	err := mgr.AddPBRRule(PBRRule{Name: "test"})
	if err != nil {
		t.Errorf("AddPBRRule should be no-op: %v", err)
	}
	err = mgr.DelPBRRule(PBRRule{Name: "test"})
	if err != nil {
		t.Errorf("DelPBRRule should be no-op: %v", err)
	}
}

func TestVtyshRouteManager_ConcurrentAccess(t *testing.T) {
	var mu sync.Mutex
	var cmds []string

	initial := []Nexthop{
		{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
		{Gateway: net.ParseIP("203.0.113.1"), Device: "eth1", Weight: 1},
	}
	mgr := NewVtyshRouteManager(initial)
	mgr.RunCmd = func(args ...string) ([]byte, error) {
		mu.Lock()
		cmds = append(cmds, strings.Join(args, " "))
		mu.Unlock()
		return nil, nil
	}

	// Concurrent updates
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				mgr.ReplaceECMPRoute([]Nexthop{
					{Gateway: net.ParseIP("10.0.0.1"), Device: "eth0", Weight: 1},
				})
			} else {
				mgr.ReplaceECMPRoute(initial)
			}
		}(i)
	}
	wg.Wait()

	// Just verify no panics / data races
	installed := mgr.InstalledRoutes()
	if len(installed) < 1 || len(installed) > 2 {
		t.Errorf("unexpected installed routes count: %d", len(installed))
	}
}
