package sysctl

import (
	"strings"
	"testing"

	"github.com/fdcastel/warp-router/internal/config"
)

func TestRenderDefault(t *testing.T) {
	cfg := &config.SiteConfig{Hostname: "r1"}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(got, "net.ipv4.ip_forward = 1") {
		t.Error("missing IP forwarding")
	}
	if !strings.Contains(got, "nf_conntrack_max = 262144") {
		t.Error("missing default conntrack max")
	}
	if !strings.Contains(got, "rp_filter = 2") {
		t.Error("missing loose rp_filter")
	}
}

func TestRenderCustomConntrack(t *testing.T) {
	cfg := &config.SiteConfig{
		Hostname: "r1",
		Sysctl:   &config.Sysctl{ConntrackMax: 524288},
	}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(got, "nf_conntrack_max = 524288") {
		t.Error("custom conntrack max not applied")
	}
}

func TestRenderSecuritySettings(t *testing.T) {
	cfg := &config.SiteConfig{Hostname: "r1"}

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	settings := []string{
		"tcp_syncookies = 1",
		"accept_redirects = 0",
		"send_redirects = 0",
		"icmp_echo_ignore_broadcasts = 1",
		"log_martians = 1",
	}
	for _, s := range settings {
		if !strings.Contains(got, s) {
			t.Errorf("missing security setting: %q", s)
		}
	}
}
