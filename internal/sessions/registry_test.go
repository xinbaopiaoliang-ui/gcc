package sessions

import "testing"

func TestSessionSnapshotIncludesPrincipalLimits(t *testing.T) {
	registry := NewRegistry()
	session := registry.Register("1", "127.0.0.1:12345")
	session.SetClientInfo("client-1", "0.3.0", "windows/amd64", 1)
	session.MarkPing()
	session.SetPrincipal("user-1", "device-1", 2, 50, true, false, []string{"steam"}, []string{"steam-web-v1"}, "r1")
	flow := session.AddFlow(1, "tcp", "store.steampowered.com:443", FlowMetadata{
		GameID:               "steam",
		PolicyID:             "steam-web-v1",
		RuleID:               "steam-store-tcp-443",
		ProcessName:          "steam.exe",
		ClientConfigRevision: "r1",
	})
	flow.AddClientToTarget(10)

	snapshots := registry.Snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("len(snapshots) = %d, want 1", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.UserID != "user-1" {
		t.Fatalf("UserID = %q, want user-1", snapshot.UserID)
	}
	if snapshot.DeviceID != "device-1" {
		t.Fatalf("DeviceID = %q, want device-1", snapshot.DeviceID)
	}
	if snapshot.ClientID != "client-1" {
		t.Fatalf("ClientID = %q, want client-1", snapshot.ClientID)
	}
	if snapshot.ClientVersion != "0.3.0" {
		t.Fatalf("ClientVersion = %q, want 0.3.0", snapshot.ClientVersion)
	}
	if snapshot.ClientPlatform != "windows/amd64" {
		t.Fatalf("ClientPlatform = %q, want windows/amd64", snapshot.ClientPlatform)
	}
	if snapshot.ProtocolVersion != 1 {
		t.Fatalf("ProtocolVersion = %d, want 1", snapshot.ProtocolVersion)
	}
	if snapshot.LastPingAt == nil {
		t.Fatal("LastPingAt = nil, want timestamp")
	}
	if snapshot.MaxConns != 2 {
		t.Fatalf("MaxConns = %d, want 2", snapshot.MaxConns)
	}
	if snapshot.RateLimitMbps != 50 {
		t.Fatalf("RateLimitMbps = %d, want 50", snapshot.RateLimitMbps)
	}
	if !snapshot.AllowTCP {
		t.Fatal("AllowTCP = false, want true")
	}
	if snapshot.AllowUDP {
		t.Fatal("AllowUDP = true, want false")
	}
	if len(snapshot.GameIDs) != 1 || snapshot.GameIDs[0] != "steam" {
		t.Fatalf("GameIDs = %#v, want [steam]", snapshot.GameIDs)
	}
	if len(snapshot.PolicyIDs) != 1 || snapshot.PolicyIDs[0] != "steam-web-v1" {
		t.Fatalf("PolicyIDs = %#v, want [steam-web-v1]", snapshot.PolicyIDs)
	}
	if snapshot.ConfigRevision != "r1" {
		t.Fatalf("ConfigRevision = %q, want r1", snapshot.ConfigRevision)
	}
	if len(snapshot.Flows) != 1 {
		t.Fatalf("len(Flows) = %d, want 1", len(snapshot.Flows))
	}
	if snapshot.Flows[0].GameID != "steam" || snapshot.Flows[0].PolicyID != "steam-web-v1" || snapshot.Flows[0].RuleID != "steam-store-tcp-443" {
		t.Fatalf("flow metadata = %#v", snapshot.Flows[0])
	}
}
