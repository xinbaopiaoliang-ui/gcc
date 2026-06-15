package router

import (
	"fmt"
	"strconv"
	"strings"
)

type portRange struct {
	start int
	end   int
}

type portMatcher struct {
	allowed []portRange
	blocked []portRange
}

func newPortMatcher(allowed, blocked []string) (*portMatcher, error) {
	allowedRanges, err := parsePortRanges(allowed)
	if err != nil {
		return nil, err
	}
	blockedRanges, err := parsePortRanges(blocked)
	if err != nil {
		return nil, err
	}
	return &portMatcher{
		allowed: allowedRanges,
		blocked: blockedRanges,
	}, nil
}

func (m *portMatcher) Allow(port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	for _, r := range m.blocked {
		if r.contains(port) {
			return false
		}
	}
	if len(m.allowed) == 0 {
		return true
	}
	for _, r := range m.allowed {
		if r.contains(port) {
			return true
		}
	}
	return false
}

func (r portRange) contains(port int) bool {
	return port >= r.start && port <= r.end
}

func parsePortRanges(values []string) ([]portRange, error) {
	ranges := make([]portRange, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, "-") {
			parts := strings.SplitN(value, "-", 2)
			start, err := parsePort(parts[0])
			if err != nil {
				return nil, fmt.Errorf("parse port range %q: %w", value, err)
			}
			end, err := parsePort(parts[1])
			if err != nil {
				return nil, fmt.Errorf("parse port range %q: %w", value, err)
			}
			if start > end {
				return nil, fmt.Errorf("invalid port range %q", value)
			}
			ranges = append(ranges, portRange{start: start, end: end})
			continue
		}
		port, err := parsePort(value)
		if err != nil {
			return nil, fmt.Errorf("parse port %q: %w", value, err)
		}
		ranges = append(ranges, portRange{start: port, end: port})
	}
	return ranges, nil
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port out of range: %d", port)
	}
	return port, nil
}
