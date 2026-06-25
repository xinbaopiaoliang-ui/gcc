package sessions

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	events   []Event
	eventSeq uint64
	eventCap int
}

type Session struct {
	ID         string
	RemoteAddr string
	CreatedAt  time.Time

	authenticated          atomic.Bool
	authenticatedAt        atomic.Int64
	userID                 atomic.Value
	deviceID               atomic.Value
	clientID               atomic.Value
	clientVersion          atomic.Value
	clientPlatform         atomic.Value
	protocolVersion        atomic.Int64
	maxConns               atomic.Int64
	rateLimitMbps          atomic.Int64
	allowTCP               atomic.Bool
	allowUDP               atomic.Bool
	gameIDs                atomic.Value
	policyIDs              atomic.Value
	configRevision         atomic.Value
	lastSeen               atomic.Int64
	lastPingAt             atomic.Int64
	udpClientToTargetBytes atomic.Int64
	udpTargetToClientBytes atomic.Int64
	tcpClientToTargetBytes atomic.Int64
	tcpTargetToClientBytes atomic.Int64
	emit                   func(Event)

	mu    sync.RWMutex
	flows map[uint32]*Flow
}

type Flow struct {
	ID        uint32
	Network   string
	Target    string
	CreatedAt time.Time
	Metadata  FlowMetadata

	lastSeen            atomic.Int64
	clientToTargetBytes atomic.Int64
	targetToClientBytes atomic.Int64
	session             *Session
}

type Event struct {
	Sequence               uint64     `json:"sequence"`
	Type                   string     `json:"type"`
	SessionID              string     `json:"session_id"`
	RemoteAddr             string     `json:"remote_addr"`
	UserID                 string     `json:"user_id,omitempty"`
	DeviceID               string     `json:"device_id,omitempty"`
	ClientID               string     `json:"client_id,omitempty"`
	ClientVersion          string     `json:"client_version,omitempty"`
	ClientPlatform         string     `json:"client_platform,omitempty"`
	ProtocolVersion        int        `json:"protocol_version,omitempty"`
	Status                 string     `json:"status"`
	CloseReason            string     `json:"close_reason,omitempty"`
	CloseSource            string     `json:"close_source,omitempty"`
	GameIDs                []string   `json:"game_ids,omitempty"`
	PolicyIDs              []string   `json:"policy_ids,omitempty"`
	ConfigRevision         string     `json:"config_revision,omitempty"`
	ConnectedAt            time.Time  `json:"connected_at"`
	AuthenticatedAt        *time.Time `json:"authenticated_at,omitempty"`
	LastSeenAt             time.Time  `json:"last_seen_at"`
	LastPingAt             *time.Time `json:"last_ping_at,omitempty"`
	EndedAt                *time.Time `json:"ended_at,omitempty"`
	DurationSeconds        int64      `json:"duration_seconds"`
	UDPFlows               int        `json:"udp_flows"`
	TCPFlows               int        `json:"tcp_flows"`
	UDPClientToTargetBytes int64      `json:"udp_client_to_target_bytes"`
	UDPTargetToClientBytes int64      `json:"udp_target_to_client_bytes"`
	TCPClientToTargetBytes int64      `json:"tcp_client_to_target_bytes"`
	TCPTargetToClientBytes int64      `json:"tcp_target_to_client_bytes"`
}

type Snapshot struct {
	ID                       string         `json:"id"`
	RemoteAddr               string         `json:"remote_addr"`
	UserID                   string         `json:"user_id,omitempty"`
	DeviceID                 string         `json:"device_id,omitempty"`
	ClientID                 string         `json:"client_id,omitempty"`
	ClientVersion            string         `json:"client_version,omitempty"`
	ClientPlatform           string         `json:"client_platform,omitempty"`
	ProtocolVersion          int            `json:"protocol_version,omitempty"`
	Authenticated            bool           `json:"authenticated"`
	MaxConns                 int            `json:"max_connections,omitempty"`
	RateLimitMbps            int            `json:"rate_limit_mbps,omitempty"`
	AllowTCP                 bool           `json:"allow_tcp"`
	AllowUDP                 bool           `json:"allow_udp"`
	GameIDs                  []string       `json:"game_ids,omitempty"`
	PolicyIDs                []string       `json:"policy_ids,omitempty"`
	ConfigRevision           string         `json:"config_revision,omitempty"`
	CreatedAt                time.Time      `json:"created_at"`
	AuthenticatedAt          *time.Time     `json:"authenticated_at,omitempty"`
	LastSeen                 time.Time      `json:"last_seen"`
	LastPingAt               *time.Time     `json:"last_ping_at,omitempty"`
	ConnectedDurationSeconds int64          `json:"connected_duration_seconds"`
	UDPFlows                 int            `json:"udp_flows"`
	TCPFlows                 int            `json:"tcp_flows"`
	UDPClientToTargetBytes   int64          `json:"udp_client_to_target_bytes"`
	UDPTargetToClientBytes   int64          `json:"udp_target_to_client_bytes"`
	TCPClientToTargetBytes   int64          `json:"tcp_client_to_target_bytes"`
	TCPTargetToClientBytes   int64          `json:"tcp_target_to_client_bytes"`
	Flows                    []FlowSnapshot `json:"flows"`
}

type FlowSnapshot struct {
	ID                   uint32    `json:"id"`
	Network              string    `json:"network"`
	Target               string    `json:"target"`
	GameID               string    `json:"game_id,omitempty"`
	PolicyID             string    `json:"policy_id,omitempty"`
	RuleID               string    `json:"rule_id,omitempty"`
	ProcessName          string    `json:"process_name,omitempty"`
	ClientConfigRevision string    `json:"client_config_revision,omitempty"`
	CaptureMode          string    `json:"capture_mode,omitempty"`
	TraceID              string    `json:"trace_id,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	LastSeen             time.Time `json:"last_seen"`
	ClientToTargetBytes  int64     `json:"client_to_target_bytes"`
	TargetToClientBytes  int64     `json:"target_to_client_bytes"`
}

type FlowMetadata struct {
	GameID               string
	PolicyID             string
	RuleID               string
	ProcessName          string
	ClientConfigRevision string
	CaptureMode          string
	TraceID              string
}

func NewRegistry() *Registry {
	return &Registry{
		sessions: make(map[string]*Session),
		eventCap: 5000,
	}
}

func (r *Registry) Register(id, remoteAddr string) *Session {
	session := &Session{
		ID:         id,
		RemoteAddr: remoteAddr,
		CreatedAt:  time.Now(),
		flows:      make(map[uint32]*Flow),
	}
	session.emit = r.appendEvent
	session.Touch()

	r.mu.Lock()
	r.sessions[id] = session
	r.mu.Unlock()

	session.emitLifecycle("session_started", "", "", nil)
	return session
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()
}

func (r *Registry) End(id string, reason string, source string, endedAt time.Time) {
	r.mu.RLock()
	session := r.sessions[id]
	r.mu.RUnlock()
	if session == nil {
		return
	}
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	session.emitLifecycle("session_ended", normalizeEventValue(reason, "unknown"), normalizeEventValue(source, "node"), &endedAt)
}

func (r *Registry) DrainEvents(limit int) []Event {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return []Event{}
	}
	if len(r.events) <= limit {
		events := append([]Event(nil), r.events...)
		r.events = r.events[:0]
		return events
	}
	start := len(r.events) - limit
	events := append([]Event(nil), r.events[start:]...)
	r.events = r.events[:0]
	return events
}

func (r *Registry) PendingEvents(limit int) []Event {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.events) == 0 {
		return []Event{}
	}
	start := 0
	if len(r.events) > limit {
		start = len(r.events) - limit
	}
	return append([]Event(nil), r.events[start:]...)
}

func (r *Registry) AckEvents(maxSequence uint64) {
	if maxSequence == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	keep := r.events[:0]
	for _, event := range r.events {
		if event.Sequence > maxSequence {
			keep = append(keep, event)
		}
	}
	r.events = keep
}

func (r *Registry) appendEvent(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventSeq++
	event.Sequence = r.eventSeq
	r.events = append(r.events, event)
	if r.eventCap <= 0 {
		r.eventCap = 5000
	}
	if len(r.events) > r.eventCap {
		r.events = append([]Event(nil), r.events[len(r.events)-r.eventCap:]...)
	}
}

func (r *Registry) Snapshot() []Snapshot {
	r.mu.RLock()
	items := make([]*Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		items = append(items, session)
	}
	r.mu.RUnlock()

	snapshots := make([]Snapshot, 0, len(items))
	for _, item := range items {
		snapshots = append(snapshots, item.Snapshot())
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt.Before(snapshots[j].CreatedAt)
	})
	return snapshots
}

func (s *Session) SetUser(userID string) {
	s.SetPrincipal(userID, "", 0, 0, true, true, nil, nil, "")
}

func (s *Session) SetPrincipal(userID, deviceID string, maxConns, rateLimitMbps int, allowTCP, allowUDP bool, gameIDs, policyIDs []string, configRevision string) {
	wasAuthenticated := s.authenticated.Load()
	s.userID.Store(userID)
	s.deviceID.Store(deviceID)
	s.maxConns.Store(int64(maxConns))
	s.rateLimitMbps.Store(int64(rateLimitMbps))
	s.allowTCP.Store(allowTCP)
	s.allowUDP.Store(allowUDP)
	s.gameIDs.Store(cloneStrings(gameIDs))
	s.policyIDs.Store(cloneStrings(policyIDs))
	s.configRevision.Store(configRevision)
	s.authenticated.Store(true)
	if s.authenticatedAt.Load() == 0 {
		s.authenticatedAt.Store(time.Now().UnixNano())
	}
	s.Touch()
	if !wasAuthenticated {
		s.emitLifecycle("session_authenticated", "", "", nil)
	}
}

func (s *Session) SetClientInfo(clientID, clientVersion, clientPlatform string, protocolVersion int) {
	if clientID != "" {
		s.clientID.Store(clientID)
	}
	if clientVersion != "" {
		s.clientVersion.Store(clientVersion)
	}
	if clientPlatform != "" {
		s.clientPlatform.Store(clientPlatform)
	}
	if protocolVersion > 0 {
		s.protocolVersion.Store(int64(protocolVersion))
	}
	s.Touch()
}

func (s *Session) UserID() string {
	value := s.userID.Load()
	if value == nil {
		return ""
	}
	userID, _ := value.(string)
	return userID
}

func (s *Session) DeviceID() string {
	value := s.deviceID.Load()
	if value == nil {
		return ""
	}
	deviceID, _ := value.(string)
	return deviceID
}

func (s *Session) ClientID() string {
	value := s.clientID.Load()
	if value == nil {
		return ""
	}
	clientID, _ := value.(string)
	return clientID
}

func (s *Session) ClientVersion() string {
	value := s.clientVersion.Load()
	if value == nil {
		return ""
	}
	clientVersion, _ := value.(string)
	return clientVersion
}

func (s *Session) ClientPlatform() string {
	value := s.clientPlatform.Load()
	if value == nil {
		return ""
	}
	clientPlatform, _ := value.(string)
	return clientPlatform
}

func (s *Session) GameIDs() []string {
	value := s.gameIDs.Load()
	if value == nil {
		return nil
	}
	items, _ := value.([]string)
	return cloneStrings(items)
}

func (s *Session) PolicyIDs() []string {
	value := s.policyIDs.Load()
	if value == nil {
		return nil
	}
	items, _ := value.([]string)
	return cloneStrings(items)
}

func (s *Session) ConfigRevision() string {
	value := s.configRevision.Load()
	if value == nil {
		return ""
	}
	configRevision, _ := value.(string)
	return configRevision
}

func (s *Session) Touch() {
	s.lastSeen.Store(time.Now().UnixNano())
}

func (s *Session) LastSeenTime() time.Time {
	value := s.lastSeen.Load()
	if value == 0 {
		return s.CreatedAt
	}
	return time.Unix(0, value)
}

func (s *Session) MarkPing() {
	now := time.Now().UnixNano()
	s.lastPingAt.Store(now)
	s.lastSeen.Store(now)
}

func (s *Session) AddFlow(id uint32, network, target string, metadata FlowMetadata) *Flow {
	flow := &Flow{
		ID:        id,
		Network:   network,
		Target:    target,
		CreatedAt: time.Now(),
		Metadata:  metadata,
		session:   s,
	}
	flow.Touch()

	s.mu.Lock()
	s.flows[id] = flow
	s.mu.Unlock()
	s.Touch()

	return flow
}

func (s *Session) RemoveFlow(id uint32) {
	s.mu.Lock()
	delete(s.flows, id)
	s.mu.Unlock()
	s.Touch()
}

func (s *Session) Snapshot() Snapshot {
	s.mu.RLock()
	flows := make([]FlowSnapshot, 0, len(s.flows))
	udpFlows := 0
	tcpFlows := 0
	for _, flow := range s.flows {
		snapshot := flow.Snapshot()
		flows = append(flows, snapshot)
		switch snapshot.Network {
		case "udp":
			udpFlows++
		case "tcp":
			tcpFlows++
		}
	}
	s.mu.RUnlock()

	sort.Slice(flows, func(i, j int) bool {
		return flows[i].CreatedAt.Before(flows[j].CreatedAt)
	})

	var lastPingAt *time.Time
	if value := s.lastPingAt.Load(); value > 0 {
		t := time.Unix(0, value)
		lastPingAt = &t
	}
	var authenticatedAt *time.Time
	if value := s.authenticatedAt.Load(); value > 0 {
		t := time.Unix(0, value)
		authenticatedAt = &t
	}

	return Snapshot{
		ID:                       s.ID,
		RemoteAddr:               s.RemoteAddr,
		UserID:                   s.UserID(),
		DeviceID:                 s.DeviceID(),
		ClientID:                 s.ClientID(),
		ClientVersion:            s.ClientVersion(),
		ClientPlatform:           s.ClientPlatform(),
		ProtocolVersion:          int(s.protocolVersion.Load()),
		Authenticated:            s.authenticated.Load(),
		MaxConns:                 int(s.maxConns.Load()),
		RateLimitMbps:            int(s.rateLimitMbps.Load()),
		AllowTCP:                 s.allowTCP.Load(),
		AllowUDP:                 s.allowUDP.Load(),
		GameIDs:                  s.GameIDs(),
		PolicyIDs:                s.PolicyIDs(),
		ConfigRevision:           s.ConfigRevision(),
		CreatedAt:                s.CreatedAt,
		AuthenticatedAt:          authenticatedAt,
		LastSeen:                 time.Unix(0, s.lastSeen.Load()),
		LastPingAt:               lastPingAt,
		ConnectedDurationSeconds: int64(time.Since(s.CreatedAt).Seconds()),
		UDPFlows:                 udpFlows,
		TCPFlows:                 tcpFlows,
		UDPClientToTargetBytes:   s.udpClientToTargetBytes.Load(),
		UDPTargetToClientBytes:   s.udpTargetToClientBytes.Load(),
		TCPClientToTargetBytes:   s.tcpClientToTargetBytes.Load(),
		TCPTargetToClientBytes:   s.tcpTargetToClientBytes.Load(),
		Flows:                    flows,
	}
}

func (f *Flow) AddClientToTarget(n int64) {
	if n <= 0 {
		return
	}
	f.clientToTargetBytes.Add(n)
	if f.session != nil {
		f.session.addFlowBytes(f.Network, true, n)
	}
	f.Touch()
}

func (f *Flow) AddTargetToClient(n int64) {
	if n <= 0 {
		return
	}
	f.targetToClientBytes.Add(n)
	if f.session != nil {
		f.session.addFlowBytes(f.Network, false, n)
	}
	f.Touch()
}

func (f *Flow) Touch() {
	f.lastSeen.Store(time.Now().UnixNano())
}

func (f *Flow) Snapshot() FlowSnapshot {
	return FlowSnapshot{
		ID:                   f.ID,
		Network:              f.Network,
		Target:               f.Target,
		GameID:               f.Metadata.GameID,
		PolicyID:             f.Metadata.PolicyID,
		RuleID:               f.Metadata.RuleID,
		ProcessName:          f.Metadata.ProcessName,
		ClientConfigRevision: f.Metadata.ClientConfigRevision,
		CaptureMode:          f.Metadata.CaptureMode,
		TraceID:              f.Metadata.TraceID,
		CreatedAt:            f.CreatedAt,
		LastSeen:             time.Unix(0, f.lastSeen.Load()),
		ClientToTargetBytes:  f.clientToTargetBytes.Load(),
		TargetToClientBytes:  f.targetToClientBytes.Load(),
	}
}

func (s *Session) addFlowBytes(network string, clientToTarget bool, n int64) {
	if n <= 0 {
		return
	}
	switch network {
	case "udp":
		if clientToTarget {
			s.udpClientToTargetBytes.Add(n)
		} else {
			s.udpTargetToClientBytes.Add(n)
		}
	case "tcp":
		if clientToTarget {
			s.tcpClientToTargetBytes.Add(n)
		} else {
			s.tcpTargetToClientBytes.Add(n)
		}
	}
}

func (s *Session) emitLifecycle(eventType string, reason string, source string, endedAt *time.Time) {
	if s.emit == nil {
		return
	}
	snapshot := s.Snapshot()
	status := "online"
	if endedAt != nil {
		status = "closed"
	}
	event := Event{
		Type:                   eventType,
		SessionID:              snapshot.ID,
		RemoteAddr:             snapshot.RemoteAddr,
		UserID:                 snapshot.UserID,
		DeviceID:               snapshot.DeviceID,
		ClientID:               snapshot.ClientID,
		ClientVersion:          snapshot.ClientVersion,
		ClientPlatform:         snapshot.ClientPlatform,
		ProtocolVersion:        snapshot.ProtocolVersion,
		Status:                 status,
		CloseReason:            reason,
		CloseSource:            source,
		GameIDs:                snapshot.GameIDs,
		PolicyIDs:              snapshot.PolicyIDs,
		ConfigRevision:         snapshot.ConfigRevision,
		ConnectedAt:            snapshot.CreatedAt,
		AuthenticatedAt:        snapshot.AuthenticatedAt,
		LastSeenAt:             snapshot.LastSeen,
		LastPingAt:             snapshot.LastPingAt,
		EndedAt:                endedAt,
		DurationSeconds:        snapshot.ConnectedDurationSeconds,
		UDPFlows:               snapshot.UDPFlows,
		TCPFlows:               snapshot.TCPFlows,
		UDPClientToTargetBytes: snapshot.UDPClientToTargetBytes,
		UDPTargetToClientBytes: snapshot.UDPTargetToClientBytes,
		TCPClientToTargetBytes: snapshot.TCPClientToTargetBytes,
		TCPTargetToClientBytes: snapshot.TCPTargetToClientBytes,
	}
	if endedAt != nil {
		event.DurationSeconds = int64(endedAt.Sub(snapshot.CreatedAt).Seconds())
		if event.DurationSeconds < 0 {
			event.DurationSeconds = 0
		}
	}
	s.emit(event)
}

func normalizeEventValue(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
