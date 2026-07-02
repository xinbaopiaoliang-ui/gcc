package router

import (
	"context"
	"errors"
	"testing"

	"gaccel-node/internal/config"
)

func TestResolveTargetAllowsPublicAddressAndAllowedPort(t *testing.T) {
	r := newTestRouter(t)

	target, err := r.ResolveTarget(context.Background(), "udp", "8.8.8.8", 1600)
	if err != nil {
		t.Fatalf("ResolveTarget returned error: %v", err)
	}
	if target != "8.8.8.8:1600" {
		t.Fatalf("target = %q, want %q", target, "8.8.8.8:1600")
	}
}

func TestResolveTargetRejectsBlockedPort(t *testing.T) {
	r := newTestRouter(t)

	_, err := r.ResolveTarget(context.Background(), "tcp", "8.8.8.8", 1500)
	if !errors.Is(err, ErrTargetDenied) {
		t.Fatalf("error = %v, want ErrTargetDenied", err)
	}
}

func TestResolveTargetRejectsPrivateAndMetadataAddresses(t *testing.T) {
	r := newTestRouter(t)

	for _, host := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "224.0.0.1"} {
		_, err := r.ResolveTarget(context.Background(), "udp", host, 1600)
		if !errors.Is(err, ErrTargetDenied) {
			t.Fatalf("host %s error = %v, want ErrTargetDenied", host, err)
		}
	}
}

func newTestRouter(t *testing.T) *Router {
	t.Helper()
	r, err := New(config.SecurityConfig{
		DenyPrivateIP:     true,
		DenyLoopback:      true,
		DenyLinkLocal:     true,
		DenyMulticast:     true,
		DenyCloudMetadata: true,
		AllowedUDPPorts:   []string{"1000-2000"},
		AllowedTCPPorts:   []string{"1000-2000"},
		BlockedTCPPorts:   []string{"1500"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return r
}

func TestDefaultSecurityAllowsGameTCPPorts(t *testing.T) {
	route, err := New(config.Default().Security)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	for _, port := range []int{8080, 8085, 10012} {
		if !route.allowPort("tcp", port) {
			t.Fatalf("default security should allow tcp port %d", port)
		}
	}
}

func TestDefaultSecurityStillBlocksDangerousTCPPorts(t *testing.T) {
	route, err := New(config.Default().Security)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	for _, port := range []int{22, 25, 3306, 5432, 6379} {
		if route.allowPort("tcp", port) {
			t.Fatalf("default security should block tcp port %d", port)
		}
	}
}

func TestDefaultSecurityStillBlocksPrivateTargets(t *testing.T) {
	route, err := New(config.Default().Security)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if _, err := route.ResolveTarget(context.Background(), "tcp", "127.0.0.1", 8080); err == nil {
		t.Fatal("ResolveTarget allowed loopback target")
	}
	if _, err := route.ResolveTarget(context.Background(), "tcp", "10.0.0.1", 8080); err == nil {
		t.Fatal("ResolveTarget allowed private target")
	}
}
