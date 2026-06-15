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

	authenticated atomic.Bool
	userID        atomic.Value
	lastSeen      atomic.Int64

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
	ID            string         `json:"id"`
	RemoteAddr    string         `json:"remote_addr"`
	UserID        string         `json:"user_id,omitempty"`
	Authenticated bool           `json:"authenticated"`
	CreatedAt     time.Time      `json:"created_at"`
	LastSeen      time.Time      `json:"last_seen"`
	UDPFlows      int            `json:"udp_flows"`
	TCPFlows      int            `json:"tcp_flows"`
	Flows         []FlowSnapshot `json:"flows"`
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
	s.userID.Store(userID)
	s.authenticated.Store(true)
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

func (s *Session) Touch() {
	s.lastSeen.Store(time.Now().UnixNano())
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

	return Snapshot{
		ID:            s.ID,
		RemoteAddr:    s.RemoteAddr,
		UserID:        s.UserID(),
		Authenticated: s.authenticated.Load(),
		CreatedAt:     s.CreatedAt,
		LastSeen:      time.Unix(0, s.lastSeen.Load()),
		UDPFlows:      udpFlows,
		TCPFlows:      tcpFlows,
		Flows:         flows,
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
