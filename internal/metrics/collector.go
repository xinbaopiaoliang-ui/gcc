package metrics

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
)

type Collector struct {
	activeQUICConnections atomic.Int64
	activeUDPFlows        atomic.Int64
	activeTCPFlows        atomic.Int64
	udpClientToTarget     atomic.Int64
	udpTargetToClient     atomic.Int64
	tcpClientToTarget     atomic.Int64
	tcpTargetToClient     atomic.Int64

	mu    sync.RWMutex
	users map[string]*UserStats

	flowMu     sync.RWMutex
	flowEvents map[flowEventKey]*atomic.Int64
}

var ErrUserConnectionLimitExceeded = errors.New("user connection limit exceeded")

type UserStats struct {
	userID            string
	activeConnections atomic.Int64
	udpClientToTarget atomic.Int64
	udpTargetToClient atomic.Int64
	tcpClientToTarget atomic.Int64
	tcpTargetToClient atomic.Int64
}

type flowEventKey struct {
	Network  string
	Event    string
	Reason   string
	GameID   string
	PolicyID string
}

type Snapshot struct {
	ActiveQUICConnections int64               `json:"active_quic_connections"`
	ActiveUDPFlows        int64               `json:"active_udp_flows"`
	ActiveTCPFlows        int64               `json:"active_tcp_flows"`
	UDPClientToTarget     int64               `json:"udp_client_to_target_bytes"`
	UDPTargetToClient     int64               `json:"udp_target_to_client_bytes"`
	TCPClientToTarget     int64               `json:"tcp_client_to_target_bytes"`
	TCPTargetToClient     int64               `json:"tcp_target_to_client_bytes"`
	Users                 []UserSnapshot      `json:"users"`
	FlowEvents            []FlowEventSnapshot `json:"flow_events"`
}

type UserSnapshot struct {
	UserID            string `json:"user_id"`
	ActiveConnections int64  `json:"active_connections"`
	UDPClientToTarget int64  `json:"udp_client_to_target_bytes"`
	UDPTargetToClient int64  `json:"udp_target_to_client_bytes"`
	TCPClientToTarget int64  `json:"tcp_client_to_target_bytes"`
	TCPTargetToClient int64  `json:"tcp_target_to_client_bytes"`
}

type FlowEventSnapshot struct {
	Network  string `json:"network"`
	Event    string `json:"event"`
	Reason   string `json:"reason"`
	GameID   string `json:"game_id,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	Count    int64  `json:"count"`
}

func NewCollector() *Collector {
	return &Collector{
		users:      make(map[string]*UserStats),
		flowEvents: make(map[flowEventKey]*atomic.Int64),
	}
}

func (c *Collector) ConnOpened() {
	c.activeQUICConnections.Add(1)
}

func (c *Collector) ConnClosed() {
	c.activeQUICConnections.Add(-1)
}

func (c *Collector) UDPFlowOpened() {
	c.UDPFlowOpenedWithPolicy("", "")
}

func (c *Collector) UDPFlowOpenedWithPolicy(gameID, policyID string) {
	c.activeUDPFlows.Add(1)
	c.AddFlowEventWithPolicy("udp", "open", "success", gameID, policyID)
}

func (c *Collector) UDPFlowClosed(reason string) {
	c.activeUDPFlows.Add(-1)
	c.AddFlowEvent("udp", "close", normalizeReason(reason))
}

func (c *Collector) TCPFlowOpened() {
	c.TCPFlowOpenedWithPolicy("", "")
}

func (c *Collector) TCPFlowOpenedWithPolicy(gameID, policyID string) {
	c.activeTCPFlows.Add(1)
	c.AddFlowEventWithPolicy("tcp", "open", "success", gameID, policyID)
}

func (c *Collector) TCPFlowClosed(reason string) {
	c.activeTCPFlows.Add(-1)
	c.AddFlowEvent("tcp", "close", normalizeReason(reason))
}

func (c *Collector) FlowOpenFailed(network, reason string) {
	c.AddFlowEvent(network, "open", normalizeReason(reason))
}

func (c *Collector) AddFlowEvent(network, event, reason string) {
	c.AddFlowEventWithPolicy(network, event, reason, "", "")
}

func (c *Collector) AddFlowEventWithPolicy(network, event, reason, gameID, policyID string) {
	key := flowEventKey{
		Network:  normalizeReason(network),
		Event:    normalizeReason(event),
		Reason:   normalizeReason(reason),
		GameID:   normalizeReasonEmpty(gameID),
		PolicyID: normalizeReasonEmpty(policyID),
	}
	c.flowEventCounter(key).Add(1)
}

func (c *Collector) UserConnOpened(userID string, maxConnections int) (func(), error) {
	user := c.user(userID)
	for {
		current := user.activeConnections.Load()
		if maxConnections > 0 && current >= int64(maxConnections) {
			return nil, ErrUserConnectionLimitExceeded
		}
		if user.activeConnections.CompareAndSwap(current, current+1) {
			break
		}
	}
	var released atomic.Bool
	return func() {
		if released.CompareAndSwap(false, true) {
			user.activeConnections.Add(-1)
		}
	}, nil
}

func (c *Collector) AddUDPClientToTarget(userID string, n int64) {
	c.udpClientToTarget.Add(n)
	c.user(userID).udpClientToTarget.Add(n)
}

func (c *Collector) AddUDPTargetToClient(userID string, n int64) {
	c.udpTargetToClient.Add(n)
	c.user(userID).udpTargetToClient.Add(n)
}

func (c *Collector) AddTCPClientToTarget(userID string, n int64) {
	c.tcpClientToTarget.Add(n)
	c.user(userID).tcpClientToTarget.Add(n)
}

func (c *Collector) AddTCPTargetToClient(userID string, n int64) {
	c.tcpTargetToClient.Add(n)
	c.user(userID).tcpTargetToClient.Add(n)
}

func (c *Collector) Snapshot() Snapshot {
	return Snapshot{
		ActiveQUICConnections: c.activeQUICConnections.Load(),
		ActiveUDPFlows:        c.activeUDPFlows.Load(),
		ActiveTCPFlows:        c.activeTCPFlows.Load(),
		UDPClientToTarget:     c.udpClientToTarget.Load(),
		UDPTargetToClient:     c.udpTargetToClient.Load(),
		TCPClientToTarget:     c.tcpClientToTarget.Load(),
		TCPTargetToClient:     c.tcpTargetToClient.Load(),
		Users:                 c.UserSnapshots(),
		FlowEvents:            c.FlowEventSnapshots(),
	}
}

func (c *Collector) UserSnapshots() []UserSnapshot {
	c.mu.RLock()
	users := make([]*UserStats, 0, len(c.users))
	for _, user := range c.users {
		users = append(users, user)
	}
	c.mu.RUnlock()

	snapshots := make([]UserSnapshot, 0, len(users))
	for _, user := range users {
		snapshots = append(snapshots, UserSnapshot{
			UserID:            user.userID,
			ActiveConnections: user.activeConnections.Load(),
			UDPClientToTarget: user.udpClientToTarget.Load(),
			UDPTargetToClient: user.udpTargetToClient.Load(),
			TCPClientToTarget: user.tcpClientToTarget.Load(),
			TCPTargetToClient: user.tcpTargetToClient.Load(),
		})
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].UserID < snapshots[j].UserID
	})
	return snapshots
}

func (c *Collector) FlowEventSnapshots() []FlowEventSnapshot {
	c.flowMu.RLock()
	items := make([]FlowEventSnapshot, 0, len(c.flowEvents))
	for key, counter := range c.flowEvents {
		items = append(items, FlowEventSnapshot{
			Network:  key.Network,
			Event:    key.Event,
			Reason:   key.Reason,
			GameID:   key.GameID,
			PolicyID: key.PolicyID,
			Count:    counter.Load(),
		})
	}
	c.flowMu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].Network != items[j].Network {
			return items[i].Network < items[j].Network
		}
		if items[i].Event != items[j].Event {
			return items[i].Event < items[j].Event
		}
		if items[i].Reason != items[j].Reason {
			return items[i].Reason < items[j].Reason
		}
		if items[i].GameID != items[j].GameID {
			return items[i].GameID < items[j].GameID
		}
		if items[i].PolicyID != items[j].PolicyID {
			return items[i].PolicyID < items[j].PolicyID
		}
		return items[i].Count < items[j].Count
	})
	return items
}

func (c *Collector) flowEventCounter(key flowEventKey) *atomic.Int64 {
	c.flowMu.RLock()
	counter := c.flowEvents[key]
	c.flowMu.RUnlock()
	if counter != nil {
		return counter
	}

	c.flowMu.Lock()
	defer c.flowMu.Unlock()
	counter = c.flowEvents[key]
	if counter == nil {
		counter = &atomic.Int64{}
		c.flowEvents[key] = counter
	}
	return counter
}

func (c *Collector) user(userID string) *UserStats {
	if userID == "" {
		userID = "anonymous"
	}
	c.mu.RLock()
	user := c.users[userID]
	c.mu.RUnlock()
	if user != nil {
		return user
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	user = c.users[userID]
	if user == nil {
		user = &UserStats{userID: userID}
		c.users[userID] = user
	}
	return user
}

func (s Snapshot) WritePrometheus(w io.Writer) {
	writeGauge(w, "gaccel_active_quic_connections", "Active QUIC connections.", s.ActiveQUICConnections)
	writeGauge(w, "gaccel_active_udp_flows", "Active UDP flows.", s.ActiveUDPFlows)
	writeGauge(w, "gaccel_active_tcp_flows", "Active TCP flows.", s.ActiveTCPFlows)
	writeCounter(w, "gaccel_udp_client_to_target_bytes_total", "UDP bytes from client to target.", s.UDPClientToTarget)
	writeCounter(w, "gaccel_udp_target_to_client_bytes_total", "UDP bytes from target to client.", s.UDPTargetToClient)
	writeCounter(w, "gaccel_tcp_client_to_target_bytes_total", "TCP bytes from client to target.", s.TCPClientToTarget)
	writeCounter(w, "gaccel_tcp_target_to_client_bytes_total", "TCP bytes from target to client.", s.TCPTargetToClient)
	for _, user := range s.Users {
		fmt.Fprintf(w, "gaccel_user_active_connections{user_id=%q} %d\n", user.UserID, user.ActiveConnections)
		fmt.Fprintf(w, "gaccel_user_udp_client_to_target_bytes_total{user_id=%q} %d\n", user.UserID, user.UDPClientToTarget)
		fmt.Fprintf(w, "gaccel_user_udp_target_to_client_bytes_total{user_id=%q} %d\n", user.UserID, user.UDPTargetToClient)
		fmt.Fprintf(w, "gaccel_user_tcp_client_to_target_bytes_total{user_id=%q} %d\n", user.UserID, user.TCPClientToTarget)
		fmt.Fprintf(w, "gaccel_user_tcp_target_to_client_bytes_total{user_id=%q} %d\n", user.UserID, user.TCPTargetToClient)
	}
	for _, event := range s.FlowEvents {
		fmt.Fprintf(w, "gaccel_flow_events_total{network=%q,event=%q,reason=%q,game_id=%q,policy_id=%q} %d\n", event.Network, event.Event, event.Reason, event.GameID, event.PolicyID, event.Count)
	}
}

func writeGauge(w io.Writer, name, help string, value int64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	fmt.Fprintf(w, "%s %d\n", name, value)
}

func writeCounter(w io.Writer, name, help string, value int64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	fmt.Fprintf(w, "%s %d\n", name, value)
}

func normalizeReason(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func normalizeReasonEmpty(value string) string {
	return value
}
