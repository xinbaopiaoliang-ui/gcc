package sessions

import "testing"

func TestSessionSnapshotIncludesPrincipalLimits(t *testing.T) {
	registry := NewRegistry()
	session := registry.Register("1", "127.0.0.1:12345")
	session.SetPrincipal("user-1", "device-1", 2, 50, true, false)

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
}
