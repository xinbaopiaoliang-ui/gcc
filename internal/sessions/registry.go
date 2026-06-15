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
}

type Session struct {
	ID         string
	RemoteAddr string
	CreatedAt  time.Time

	authenticated   atomic.Bool
	userID          atomic.Value
	deviceID        atomic.Value
	clientID        atomic.Value
	clientVersion   atomic.Value
	clientPlatform  atomic.Value
	protocolVersion atomic.Int64
	maxConns        atomic.Int64
	rateLimitMbps   atomic.Int64
	allowTCP        atomic.Bool
	allowUDP        atomic.Bool
	lastSeen        atomic.Int64
	lastPingAt      atomic.Int64

	mu    sync.RWMutex
	flows map[uint32]*Flow
}

type Flow struct {
	ID        uint32
	Network   string
	Target    string
	CreatedAt time.Time

	lastSeen            atomic.Int64
	clientToTargetBytes atomic.Int64
	targetToClientBytes atomic.Int64
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
	CreatedAt                time.Time      `json:"created_at"`
	LastSeen                 time.Time      `json:"last_seen"`
	LastPingAt               *time.Time     `json:"last_ping_at,omitempty"`
	ConnectedDurationSeconds int64          `json:"connected_duration_seconds"`
	UDPFlows                 int            `json:"udp_flows"`
	TCPFlows                 int            `json:"tcp_flows"`
	Flows                    []FlowSnapshot `json:"flows"`
}

type FlowSnapshot struct {
	ID                  uint32    `json:"id"`
	Network             string    `json:"network"`
	Target              string    `json:"target"`
	CreatedAt           time.Time `json:"created_at"`
	LastSeen            time.Time `json:"last_seen"`
	ClientToTargetBytes int64     `json:"client_to_target_bytes"`
	TargetToClientBytes int64     `json:"target_to_client_bytes"`
}

func NewRegistry() *Registry {
	return &Registry{
		sessions: make(map[string]*Session),
	}
}

func (r *Registry) Register(id, remoteAddr string) *Session {
	session := &Session{
		ID:         id,
		RemoteAddr: remoteAddr,
		CreatedAt:  time.Now(),
		flows:      make(map[uint32]*Flow),
	}
	session.Touch()

	r.mu.Lock()
	r.sessions[id] = session
	r.mu.Unlock()

	return session
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()
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
	s.SetPrincipal(userID, "", 0, 0, true, true)
}

func (s *Session) SetPrincipal(userID, deviceID string, maxConns, rateLimitMbps int, allowTCP, allowUDP bool) {
	s.userID.Store(userID)
	s.deviceID.Store(deviceID)
	s.maxConns.Store(int64(maxConns))
	s.rateLimitMbps.Store(int64(rateLimitMbps))
	s.allowTCP.Store(allowTCP)
	s.allowUDP.Store(allowUDP)
	s.authenticated.Store(true)
	s.Touch()
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

func (s *Session) Touch() {
	s.lastSeen.Store(time.Now().UnixNano())
}

func (s *Session) MarkPing() {
	now := time.Now().UnixNano()
	s.lastPingAt.Store(now)
	s.lastSeen.Store(now)
}

func (s *Session) AddFlow(id uint32, network, target string) *Flow {
	flow := &Flow{
		ID:        id,
		Network:   network,
		Target:    target,
		CreatedAt: time.Now(),
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
		CreatedAt:                s.CreatedAt,
		LastSeen:                 time.Unix(0, s.lastSeen.Load()),
		LastPingAt:               lastPingAt,
		ConnectedDurationSeconds: int64(time.Since(s.CreatedAt).Seconds()),
		UDPFlows:                 udpFlows,
		TCPFlows:                 tcpFlows,
		Flows:                    flows,
	}
}

func (f *Flow) AddClientToTarget(n int64) {
	if n <= 0 {
		return
	}
	f.clientToTargetBytes.Add(n)
	f.Touch()
}

func (f *Flow) AddTargetToClient(n int64) {
	if n <= 0 {
		return
	}
	f.targetToClientBytes.Add(n)
	f.Touch()
}

func (f *Flow) Touch() {
	f.lastSeen.Store(time.Now().UnixNano())
}

func (f *Flow) Snapshot() FlowSnapshot {
	return FlowSnapshot{
		ID:                  f.ID,
		Network:             f.Network,
		Target:              f.Target,
		CreatedAt:           f.CreatedAt,
		LastSeen:            time.Unix(0, f.lastSeen.Load()),
		ClientToTargetBytes: f.clientToTargetBytes.Load(),
		TargetToClientBytes: f.targetToClientBytes.Load(),
	}
}
