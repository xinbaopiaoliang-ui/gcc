package panel

import (
	"errors"
	"testing"
	"time"
)

func TestProbeDNSAcceptsIPAddress(t *testing.T) {
	ips, latency, err := probeDNS(t.Context(), "195.245.242.9")
	if err != nil {
		t.Fatalf("probeDNS returned error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "195.245.242.9" {
		t.Fatalf("ips = %#v, want 195.245.242.9", ips)
	}
	if latency < 0 {
		t.Fatalf("latency = %s, want non-negative", latency)
	}
}

func TestAdminTCPProbeCheckWarnsForLoopback(t *testing.T) {
	check := adminTCPProbeCheck(Node{
		NodeID:    "node-local-admin",
		AdminHost: "127.0.0.1",
		AdminPort: 5557,
	}, 0, errors.New("admin host is loopback from panel side"))
	if check.Status != DiagnosticStatusWarning {
		t.Fatalf("status = %q, want warning", check.Status)
	}
	if check.Key != "connectivity.admin_tcp" {
		t.Fatalf("key = %q", check.Key)
	}
}

func TestQUICAuthProbeWarnsWhenHMACMissing(t *testing.T) {
	check := quicAuthProbeCheck(Node{
		NodeID:       "node-hk-01",
		EndpointHost: "203.0.113.10",
		EndpointPort: 5555,
	}, errors.New("node hmac_secret is not configured"), quicProbeResult{
		HandshakeLatency: 20 * time.Millisecond,
	})
	if check.Status != DiagnosticStatusWarning {
		t.Fatalf("status = %q, want warning", check.Status)
	}
	if check.Detail["reason"] == "" {
		t.Fatalf("expected reason detail, got %#v", check.Detail)
	}
}

func TestConnectivityRecommendationsForHealthyProbe(t *testing.T) {
	items := connectivityRecommendations(Node{
		NodeID:    "node-hk-01",
		AdminHost: "47.83.160.126",
	}, []DiagnosticCheck{
		newDiagnosticCheck("connectivity.dns", "入口 DNS", DiagnosticStatusOK, "ok", nil),
		newDiagnosticCheck("connectivity.quic_handshake", "QUIC 握手", DiagnosticStatusOK, "ok", nil),
	})
	if len(items) != 1 {
		t.Fatalf("recommendations = %#v, want one item", items)
	}
}
