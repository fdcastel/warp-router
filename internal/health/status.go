package health

import (
	"encoding/json"
	"os"
	"time"
)

const StatusFilePath = "/run/warp/health.json"

// StatusReport is written to disk by the daemon for the CLI to read.
type StatusReport struct {
	Timestamp time.Time         `json:"timestamp"`
	Uplinks   []UplinkStatus    `json:"uplinks"`
}

// UplinkStatus is a serializable view of UplinkState.
type UplinkStatus struct {
	Name             string        `json:"name"`
	Target           string        `json:"target"`
	Status           string        `json:"status"`
	ConsecutiveFails int           `json:"consecutive_fails"`
	LastProbeTime    time.Time     `json:"last_probe_time"`
	LastSuccessTime  time.Time     `json:"last_success_time"`
	LastLatencyMs    float64       `json:"last_latency_ms"`
	TotalProbes      int64         `json:"total_probes"`
	TotalFailures    int64         `json:"total_failures"`
}

// WriteStatusFile writes the current health state to disk.
func (p *Prober) WriteStatusFile(path string) error {
	states := p.GetAllStates()

	report := StatusReport{
		Timestamp: time.Now().UTC(),
	}

	for _, s := range states {
		report.Uplinks = append(report.Uplinks, UplinkStatus{
			Name:             s.Name,
			Target:           s.Target,
			Status:           s.Status.String(),
			ConsecutiveFails: s.ConsecutiveFails,
			LastProbeTime:    s.LastProbeTime,
			LastSuccessTime:  s.LastSuccessTime,
			LastLatencyMs:    float64(s.LastLatency) / float64(time.Millisecond),
			TotalProbes:      s.TotalProbes,
			TotalFailures:    s.TotalFailures,
		})
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// ReadStatusFile reads the health status from disk (for CLI use).
func ReadStatusFile(path string) (*StatusReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report StatusReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}
