package health

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Status represents the current health of a WAN uplink.
type Status int

const (
	StatusUnknown Status = iota
	StatusHealthy
	StatusDegraded // Some probes failing, not yet declared down
	StatusDown
)

func (s Status) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusDegraded:
		return "degraded"
	case StatusDown:
		return "down"
	default:
		return "unknown"
	}
}

// UplinkState holds the health state for a single WAN uplink.
type UplinkState struct {
	Name             string
	Target           string
	Status           Status
	ConsecutiveFails int
	LastProbeTime    time.Time
	LastSuccessTime  time.Time
	LastLatency      time.Duration
	TotalProbes      int64
	TotalFailures    int64
}

// ProbeConfig configures the health probe for a WAN uplink.
type ProbeConfig struct {
	Name     string
	Target   string // IP to probe
	Interval time.Duration
	Timeout  time.Duration
	Failures int // Consecutive failures before marking down
}

// Prober manages health probing for one or more WAN uplinks.
type Prober struct {
	mu     sync.RWMutex
	states map[string]*UplinkState
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// PingFunc allows injecting a custom ping implementation for testing.
	// If nil, uses real ICMP ping.
	PingFunc func(target string, timeout time.Duration) (time.Duration, error)

	// OnStateChange is called when an uplink's health status changes.
	OnStateChange func(name string, oldStatus, newStatus Status)
}

// NewProber creates a new health prober.
func NewProber() *Prober {
	return &Prober{
		states: make(map[string]*UplinkState),
	}
}

// Start begins health probing for the given configs.
func (p *Prober) Start(ctx context.Context, configs []ProbeConfig) {
	ctx, p.cancel = context.WithCancel(ctx)

	for _, cfg := range configs {
		state := &UplinkState{
			Name:   cfg.Name,
			Target: cfg.Target,
			Status: StatusUnknown,
		}
		p.mu.Lock()
		p.states[cfg.Name] = state
		p.mu.Unlock()

		p.wg.Add(1)
		go p.probeLoop(ctx, cfg)
	}
}

// Stop halts all probing goroutines.
func (p *Prober) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

// GetState returns the current state of a named uplink.
func (p *Prober) GetState(name string) *UplinkState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	state, ok := p.states[name]
	if !ok {
		return nil
	}
	// Return a copy
	s := *state
	return &s
}

// GetAllStates returns a copy of all uplink states.
func (p *Prober) GetAllStates() []UplinkState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	states := make([]UplinkState, 0, len(p.states))
	for _, s := range p.states {
		states = append(states, *s)
	}
	return states
}

// HealthyUplinks returns the names of uplinks currently in healthy status.
func (p *Prober) HealthyUplinks() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var names []string
	for name, state := range p.states {
		if state.Status == StatusHealthy {
			names = append(names, name)
		}
	}
	return names
}

func (p *Prober) probeLoop(ctx context.Context, cfg ProbeConfig) {
	defer p.wg.Done()

	// Set defaults
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Failures <= 0 {
		cfg.Failures = 3
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.doPing(cfg)
		}
	}
}

func (p *Prober) doPing(cfg ProbeConfig) {
	var latency time.Duration
	var err error

	if p.PingFunc != nil {
		latency, err = p.PingFunc(cfg.Target, cfg.Timeout)
	} else {
		latency, err = icmpPing(cfg.Target, cfg.Timeout)
	}

	var nameForCallback string
	var oldStatusForCallback, newStatusForCallback Status
	var shouldCallback bool

	p.mu.Lock()

	state := p.states[cfg.Name]
	state.LastProbeTime = time.Now()
	state.TotalProbes++

	oldStatus := state.Status

	if err != nil {
		state.TotalFailures++
		state.ConsecutiveFails++

		if state.ConsecutiveFails >= cfg.Failures {
			state.Status = StatusDown
		} else if state.ConsecutiveFails >= 1 {
			state.Status = StatusDegraded
		}
	} else {
		state.ConsecutiveFails = 0
		state.LastSuccessTime = time.Now()
		state.LastLatency = latency
		state.Status = StatusHealthy
	}

	if oldStatus != state.Status && p.OnStateChange != nil {
		shouldCallback = true
		nameForCallback = cfg.Name
		oldStatusForCallback = oldStatus
		newStatusForCallback = state.Status
	}

	p.mu.Unlock()

	if shouldCallback {
		p.OnStateChange(nameForCallback, oldStatusForCallback, newStatusForCallback)
	}
}

// icmpPing sends a single ICMP echo request and waits for a reply.
func icmpPing(target string, timeout time.Duration) (time.Duration, error) {
	conn, err := icmp.ListenPacket("udp4", "")
	if err != nil {
		return 0, fmt.Errorf("listen: %w", err)
	}
	defer conn.Close()

	dst := &net.UDPAddr{IP: net.ParseIP(target)}
	if dst.IP == nil {
		return 0, fmt.Errorf("invalid target IP: %s", target)
	}

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("warp-health"),
		},
	}
	wb, err := msg.Marshal(nil)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}

	start := time.Now()
	if err := conn.SetDeadline(start.Add(timeout)); err != nil {
		return 0, fmt.Errorf("set deadline: %w", err)
	}

	if _, err := conn.WriteTo(wb, dst); err != nil {
		return 0, fmt.Errorf("write: %w", err)
	}

	rb := make([]byte, 1500)
	n, _, err := conn.ReadFrom(rb)
	if err != nil {
		return 0, fmt.Errorf("read: %w", err)
	}

	latency := time.Since(start)

	rm, err := icmp.ParseMessage(1, rb[:n]) // protocol 1 = ICMP
	if err != nil {
		return 0, fmt.Errorf("parse reply: %w", err)
	}

	if rm.Type != ipv4.ICMPTypeEchoReply {
		return 0, fmt.Errorf("unexpected ICMP type: %v", rm.Type)
	}

	return latency, nil
}
