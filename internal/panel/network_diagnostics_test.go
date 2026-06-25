package panel

import (
	"testing"
	"time"
)

func TestParseNetworkDiagnosticsOutput(t *testing.T) {
	values := parseNetworkDiagnosticsOutput(`
sysctl.net.core.rmem_max=67108864
sysctl.net.core.wmem_max=67108864
kernel.loadavg=0.31 0.28 0.22
ignored-line
=ignored
`)
	if values["sysctl.net.core.rmem_max"] != "67108864" {
		t.Fatalf("rmem_max = %q", values["sysctl.net.core.rmem_max"])
	}
	if values["kernel.loadavg"] != "0.31 0.28 0.22" {
		t.Fatalf("loadavg = %q", values["kernel.loadavg"])
	}
	if _, ok := values[""]; ok {
		t.Fatalf("empty key should be ignored")
	}
}

func TestBuildNodeNetworkDiagnosticsLowRisk(t *testing.T) {
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	resp := BuildNodeNetworkDiagnostics("node-hk-01", map[string]string{
		"sysctl.net.core.rmem_max":           "134217728",
		"sysctl.net.core.wmem_max":           "134217728",
		"sysctl.net.core.rmem_default":       "8388608",
		"sysctl.net.core.wmem_default":       "8388608",
		"sysctl.net.core.netdev_max_backlog": "250000",
		"udp.socket_count":                   "4",
		"udp.recv_q_total":                   "0",
		"udp.send_q_total":                   "0",
		"netdev.rx_dropped":                  "0",
		"netdev.tx_dropped":                  "0",
		"netdev.rx_errors":                   "0",
		"netdev.tx_errors":                   "0",
		"journal.udp_buffer_warnings":        "0",
		"journal.warn_or_error":              "0",
		"kernel.cpu_count":                   "4",
		"kernel.loadavg":                     "0.31 0.28 0.22",
	}, now)
	if resp.RiskLevel != "low" {
		t.Fatalf("risk level = %q, want low, score=%d", resp.RiskLevel, resp.RiskScore)
	}
	if resp.Status != DiagnosticStatusOK {
		t.Fatalf("status = %q, want ok", resp.Status)
	}
	if resp.Summary.Warning != 0 || resp.Summary.Error != 0 {
		t.Fatalf("summary = %#v", resp.Summary)
	}
}

func TestBuildNodeNetworkDiagnosticsHighRisk(t *testing.T) {
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	resp := BuildNodeNetworkDiagnostics("node-hk-01", map[string]string{
		"sysctl.net.core.rmem_max":           "212992",
		"sysctl.net.core.wmem_max":           "212992",
		"sysctl.net.core.rmem_default":       "212992",
		"sysctl.net.core.wmem_default":       "212992",
		"sysctl.net.core.netdev_max_backlog": "1000",
		"udp.socket_count":                   "20",
		"udp.recv_q_total":                   "128",
		"udp.send_q_total":                   "64",
		"netdev.rx_dropped":                  "2300",
		"netdev.tx_dropped":                  "12",
		"netdev.rx_errors":                   "1",
		"netdev.tx_errors":                   "0",
		"journal.udp_buffer_warnings":        "3",
		"journal.warn_or_error":              "5",
		"kernel.cpu_count":                   "2",
		"kernel.loadavg":                     "2.40 1.80 1.10",
	}, now)
	if resp.RiskLevel != "high" {
		t.Fatalf("risk level = %q, want high, score=%d", resp.RiskLevel, resp.RiskScore)
	}
	if resp.Summary.Warning == 0 {
		t.Fatalf("expected warnings: %#v", resp.Summary)
	}
	if len(resp.Recommendations) == 0 {
		t.Fatalf("expected recommendations")
	}
}
