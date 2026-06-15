package router

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"gaccel-node/internal/config"
)

var ErrTargetDenied = errors.New("target denied")

type Router struct {
	cfg      config.SecurityConfig
	udpPorts *portMatcher
	tcpPorts *portMatcher
}

func New(cfg config.SecurityConfig) (*Router, error) {
	udpPorts, err := newPortMatcher(cfg.AllowedUDPPorts, cfg.BlockedUDPPorts)
	if err != nil {
		return nil, err
	}
	tcpPorts, err := newPortMatcher(cfg.AllowedTCPPorts, cfg.BlockedTCPPorts)
	if err != nil {
		return nil, err
	}
	return &Router{
		cfg:      cfg,
		udpPorts: udpPorts,
		tcpPorts: tcpPorts,
	}, nil
}

func (r *Router) ResolveTarget(ctx context.Context, network, host string, port int) (string, error) {
	host = strings.TrimSpace(host)
	network = strings.ToLower(strings.TrimSpace(network))
	if host == "" {
		return "", fmt.Errorf("%w: target host is required", ErrTargetDenied)
	}
	if !r.allowPort(network, port) {
		return "", fmt.Errorf("%w: %s port %d is not allowed", ErrTargetDenied, network, port)
	}

	addr, err := r.resolveAllowedAddr(ctx, host)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(addr.String(), strconv.Itoa(port)), nil
}

func (r *Router) allowPort(network string, port int) bool {
	switch network {
	case "udp":
		return r.udpPorts.Allow(port)
	case "tcp":
		return r.tcpPorts.Allow(port)
	default:
		return false
	}
}

func (r *Router) resolveAllowedAddr(ctx context.Context, host string) (netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		if r.allowAddr(addr) {
			return addr, nil
		}
		return netip.Addr{}, fmt.Errorf("%w: address %s is blocked", ErrTargetDenied, addr)
	}

	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, err
	}
	for _, addr := range addrs {
		if r.allowAddr(addr) {
			return addr, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("%w: all resolved addresses for %s are blocked", ErrTargetDenied, host)
}

func (r *Router) allowAddr(addr netip.Addr) bool {
	if !addr.IsValid() || addr.IsUnspecified() {
		return false
	}
	addr = addr.Unmap()
	if r.cfg.DenyLoopback && addr.IsLoopback() {
		return false
	}
	if r.cfg.DenyPrivateIP && addr.IsPrivate() {
		return false
	}
	if r.cfg.DenyLinkLocal && addr.IsLinkLocalUnicast() {
		return false
	}
	if r.cfg.DenyMulticast && addr.IsMulticast() {
		return false
	}
	if r.cfg.DenyCloudMetadata && isCloudMetadataAddr(addr) {
		return false
	}
	return true
}

func isCloudMetadataAddr(addr netip.Addr) bool {
	switch addr.String() {
	case "169.254.169.254", "100.100.100.200":
		return true
	default:
		return false
	}
}
