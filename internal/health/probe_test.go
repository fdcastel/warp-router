package health

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestProberHealthyOnSuccess(t *testing.T) {
	prober := NewProber()
	prober.PingFunc = func(target string, timeout time.Duration) (time.Duration, error) {
		return 5 * time.Millisecond, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prober.Start(ctx, []ProbeConfig{
		{Name: "wan1", Target: "10.0.0.1", Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond, Failures: 3},
	})

	// Wait for a few probes
	time.Sleep(100 * time.Millisecond)
	prober.Stop()

	state := prober.GetState("wan1")
	if state == nil {
		t.Fatal("state is nil")
	}
	if state.Status != StatusHealthy {
		t.Errorf("status = %v, want %v", state.Status, StatusHealthy)
	}
	if state.TotalProbes == 0 {
		t.Error("no probes were sent")
	}
	if state.ConsecutiveFails != 0 {
		t.Errorf("consecutive_fails = %d, want 0", state.ConsecutiveFails)
	}
}

func TestProberDownAfterConsecutiveFailures(t *testing.T) {
	prober := NewProber()
	prober.PingFunc = func(target string, timeout time.Duration) (time.Duration, error) {
		return 0, fmt.Errorf("timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prober.Start(ctx, []ProbeConfig{
		{Name: "wan1", Target: "10.0.0.1", Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond, Failures: 3},
	})

	// Wait for enough probes to trigger down (3 failures + some buffer)
	time.Sleep(150 * time.Millisecond)
	prober.Stop()

	state := prober.GetState("wan1")
	if state == nil {
		t.Fatal("state is nil")
	}
	if state.Status != StatusDown {
		t.Errorf("status = %v, want %v", state.Status, StatusDown)
	}
	if state.ConsecutiveFails < 3 {
		t.Errorf("consecutive_fails = %d, want >= 3", state.ConsecutiveFails)
	}
}

func TestProberRecovery(t *testing.T) {
	var mu sync.Mutex
	failCount := 0
	prober := NewProber()
	prober.PingFunc = func(target string, timeout time.Duration) (time.Duration, error) {
		mu.Lock()
		defer mu.Unlock()
		failCount++
		if failCount <= 4 {
			return 0, fmt.Errorf("timeout")
		}
		return 5 * time.Millisecond, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prober.Start(ctx, []ProbeConfig{
		{Name: "wan1", Target: "10.0.0.1", Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond, Failures: 3},
	})

	// Wait for failures then recovery
	time.Sleep(250 * time.Millisecond)
	prober.Stop()

	state := prober.GetState("wan1")
	if state == nil {
		t.Fatal("state is nil")
	}
	if state.Status != StatusHealthy {
		t.Errorf("status = %v, want %v (should have recovered)", state.Status, StatusHealthy)
	}
	if state.ConsecutiveFails != 0 {
		t.Errorf("consecutive_fails = %d, want 0 after recovery", state.ConsecutiveFails)
	}
}

func TestProberStateChangeCallback(t *testing.T) {
	var mu sync.Mutex
	var transitions []string

	prober := NewProber()
	prober.PingFunc = func(target string, timeout time.Duration) (time.Duration, error) {
		return 0, fmt.Errorf("timeout")
	}
	prober.OnStateChange = func(name string, oldStatus, newStatus Status) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, fmt.Sprintf("%s:%s->%s", name, oldStatus, newStatus))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prober.Start(ctx, []ProbeConfig{
		{Name: "wan1", Target: "10.0.0.1", Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond, Failures: 3},
	})

	time.Sleep(150 * time.Millisecond)
	prober.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(transitions) == 0 {
		t.Error("no state transitions recorded")
	}
	// Should have at least: unknown→degraded and degraded→down
	hasDegraded := false
	hasDown := false
	for _, tr := range transitions {
		if tr == "wan1:unknown->degraded" {
			hasDegraded = true
		}
		if tr == "wan1:degraded->down" {
			hasDown = true
		}
	}
	if !hasDegraded {
		t.Errorf("missing unknown->degraded transition, got: %v", transitions)
	}
	if !hasDown {
		t.Errorf("missing degraded->down transition, got: %v", transitions)
	}
}

func TestProberMultipleUplinks(t *testing.T) {
	prober := NewProber()
	prober.PingFunc = func(target string, timeout time.Duration) (time.Duration, error) {
		if target == "10.0.0.1" {
			return 5 * time.Millisecond, nil
		}
		return 0, fmt.Errorf("timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prober.Start(ctx, []ProbeConfig{
		{Name: "wan1", Target: "10.0.0.1", Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond, Failures: 3},
		{Name: "wan2", Target: "10.0.1.1", Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond, Failures: 3},
	})

	time.Sleep(150 * time.Millisecond)
	prober.Stop()

	wan1 := prober.GetState("wan1")
	wan2 := prober.GetState("wan2")

	if wan1.Status != StatusHealthy {
		t.Errorf("wan1 status = %v, want healthy", wan1.Status)
	}
	if wan2.Status != StatusDown {
		t.Errorf("wan2 status = %v, want down", wan2.Status)
	}

	healthy := prober.HealthyUplinks()
	if len(healthy) != 1 || healthy[0] != "wan1" {
		t.Errorf("HealthyUplinks = %v, want [wan1]", healthy)
	}
}

func TestProberGetAllStates(t *testing.T) {
	prober := NewProber()
	prober.PingFunc = func(target string, timeout time.Duration) (time.Duration, error) {
		return 1 * time.Millisecond, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prober.Start(ctx, []ProbeConfig{
		{Name: "wan1", Target: "10.0.0.1", Interval: 20 * time.Millisecond},
		{Name: "wan2", Target: "10.0.1.1", Interval: 20 * time.Millisecond},
	})

	time.Sleep(60 * time.Millisecond)
	prober.Stop()

	states := prober.GetAllStates()
	if len(states) != 2 {
		t.Errorf("got %d states, want 2", len(states))
	}
}

func TestProberGetStateNonexistent(t *testing.T) {
	prober := NewProber()
	state := prober.GetState("nonexistent")
	if state != nil {
		t.Error("expected nil for nonexistent uplink")
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{StatusUnknown, "unknown"},
		{StatusHealthy, "healthy"},
		{StatusDegraded, "degraded"},
		{StatusDown, "down"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}
