package quicserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"

	"gaccel-node/internal/auth"
	"gaccel-node/internal/config"
	"gaccel-node/internal/limiter"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/protocol"
	"gaccel-node/internal/router"
	"gaccel-node/internal/sessions"
)

type connSession struct {
	conn            *quic.Conn
	cfg             *config.Manager
	collector       *metrics.Collector
	record          *sessions.Session
	logger          *slog.Logger
	principal       atomic.Pointer[auth.Principal]
	limiter         *limiter.ByteLimiter
	nextFlow        atomic.Uint32
	releaseUserConn func()

	mu       sync.RWMutex
	flows    map[uint32]*udpFlow
	tcpFlows int
}

func newConnSession(conn *quic.Conn, cfg *config.Manager, collector *metrics.Collector, record *sessions.Session, logger *slog.Logger) *connSession {
	current := cfg.Current()
	return &connSession{
		conn:      conn,
		cfg:       cfg,
		collector: collector,
		record:    record,
		logger:    logger,
		limiter:   limiter.NewByteLimiter(current.Limits.UserRateLimitMbps),
		flows:     make(map[uint32]*udpFlow),
	}
}

func (s *connSession) setPrincipal(principal *auth.Principal) error {
	if s.authenticated() {
		return nil
	}
	release, err := s.collector.UserConnOpened(principal.UserID, s.cfg.Current().Limits.MaxUserConnections)
	if err != nil {
		return err
	}
	if !s.principal.CompareAndSwap(nil, principal) {
		release()
		return nil
	}
	s.releaseUserConn = release
	s.record.SetUser(principal.UserID)
	return nil
}

func (s *connSession) authenticated() bool {
	return s.principal.Load() != nil
}

func (s *connSession) userID() string {
	principal := s.principal.Load()
	if principal == nil || principal.UserID == "" {
		return "anonymous"
	}
	return principal.UserID
}

func (s *connSession) openUDP(ctx context.Context, targetHost string, targetPort int) (uint32, error) {
	if !s.authenticated() {
		return 0, errors.New("session is not authenticated")
	}
	s.record.Touch()
	cfg := s.cfg.Current()

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.flows)+s.tcpFlows >= cfg.Limits.MaxFlowsPerConn {
		return 0, errors.New("max flows per connection exceeded")
	}

	route, err := router.New(cfg.Security)
	if err != nil {
		return 0, err
	}
	target, err := route.ResolveTarget(ctx, "udp", targetHost, targetPort)
	if err != nil {
		return 0, err
	}
	addr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return 0, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return 0, err
	}

	flowID := s.nextFlow.Add(1)
	flow := newUDPFlow(udpFlowConfig{
		id:          flowID,
		conn:        conn,
		idleTimeout: cfg.Limits.UDPIdleTimeout,
		collector:   s.collector,
		userID:      s.userID(),
		limiter:     s.limiter,
		send:        s.conn.SendDatagram,
		remove:      s.removeUDPFlow,
		logger:      s.logger.With("flow_id", flowID, "target", target),
	})
	s.flows[flowID] = flow
	flow.record = s.record.AddFlow(flowID, "udp", target)
	s.collector.UDPFlowOpened()
	flow.start()

	return flowID, nil
}

func (s *connSession) openTCPTarget(ctx context.Context, targetHost string, targetPort int) (uint32, net.Conn, func(string), error) {
	if !s.authenticated() {
		return 0, nil, nil, errors.New("session is not authenticated")
	}
	s.record.Touch()
	cfg := s.cfg.Current()

	flowID, release, err := s.reserveTCPFlow()
	if err != nil {
		return 0, nil, nil, err
	}

	route, err := router.New(cfg.Security)
	if err != nil {
		release()
		return 0, nil, nil, err
	}
	target, err := route.ResolveTarget(ctx, "tcp", targetHost, targetPort)
	if err != nil {
		release()
		return 0, nil, nil, err
	}

	dialer := net.Dialer{Timeout: 10 * time.Second}
	targetConn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		release()
		return 0, nil, nil, err
	}

	s.logger.Debug("tcp flow opened", "flow_id", flowID, "target", target)
	flowRecord := s.record.AddFlow(flowID, "tcp", target)
	s.collector.TCPFlowOpened()
	metricsRelease := func(reason string) {
		s.collector.TCPFlowClosed(reason)
		s.record.RemoveFlow(flowID)
		release()
	}
	return flowID, &trackedConn{Conn: targetConn, userID: s.userID(), flow: flowRecord}, metricsRelease, nil
}

func (s *connSession) relayTCP(ctx context.Context, stream *quic.Stream, targetConn net.Conn, flowID uint32, release func(string)) {
	closeReason := "eof"
	defer func() {
		release(closeReason)
		go s.notifyFlowClosed(flowID, closeReason)
	}()
	defer targetConn.Close()

	errCh := make(chan error, 2)
	go copyClientToTarget(ctx, stream, targetConn, s.collector, s.limiter, errCh)
	go copyTargetToClient(ctx, stream, targetConn, s.collector, s.limiter, errCh)

	select {
	case <-ctx.Done():
		closeReason = "session_closed"
		return
	case err := <-errCh:
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			closeReason = "error"
			s.logger.Debug("tcp flow closed with error", "flow_id", flowID, "error", err)
		} else if errors.Is(err, net.ErrClosed) {
			closeReason = "closed"
		}
		return
	}
}

func (s *connSession) reserveTCPFlow() (uint32, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.flows)+s.tcpFlows >= s.cfg.Current().Limits.MaxFlowsPerConn {
		return 0, nil, errors.New("max flows per connection exceeded")
	}
	s.tcpFlows++
	flowID := s.nextFlow.Add(1)
	released := atomic.Bool{}
	release := func() {
		if !released.CompareAndSwap(false, true) {
			return
		}
		s.mu.Lock()
		s.tcpFlows--
		s.mu.Unlock()
	}
	return flowID, release, nil
}

func (s *connSession) handleDatagram(packet []byte) error {
	if !s.authenticated() {
		return errors.New("session is not authenticated")
	}
	s.record.Touch()
	datagram, err := protocol.ParseDatagram(packet)
	if err != nil {
		return err
	}
	if datagram.Version != protocol.Version || datagram.Type != protocol.DatagramTypeUDP {
		return errors.New("unsupported datagram")
	}

	s.mu.RLock()
	flow := s.flows[datagram.FlowID]
	s.mu.RUnlock()
	if flow == nil {
		return errors.New("unknown flow")
	}
	return flow.write(datagram.Payload)
}

func (s *connSession) closeFlow(flowID uint32) {
	s.mu.RLock()
	flow := s.flows[flowID]
	s.mu.RUnlock()
	if flow != nil {
		flow.close("client_closed")
	}
}

func (s *connSession) close() {
	s.mu.RLock()
	flows := make([]*udpFlow, 0, len(s.flows))
	for _, flow := range s.flows {
		flows = append(flows, flow)
	}
	s.mu.RUnlock()

	for _, flow := range flows {
		flow.close("session_closed")
	}
	if s.releaseUserConn != nil {
		s.releaseUserConn()
		s.releaseUserConn = nil
	}
}

func (s *connSession) notifyFlowClosed(flowID uint32, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	stream, err := s.conn.OpenStreamSync(ctx)
	if err != nil {
		s.logger.Debug("open close notification stream failed", "flow_id", flowID, "reason", reason, "error", err)
		return
	}
	defer stream.Close()

	codec := protocol.NewCodec(stream)
	if err := codec.Write(protocol.Message{
		Type:      protocol.MessageClose,
		FlowID:    flowID,
		ErrorCode: reason,
	}); err != nil {
		s.logger.Debug("send close notification failed", "flow_id", flowID, "reason", reason, "error", err)
	}
}

func copyClientToTarget(ctx context.Context, stream *quic.Stream, targetConn net.Conn, collector *metrics.Collector, rateLimiter *limiter.ByteLimiter, errCh chan<- error) {
	userID := "anonymous"
	var flow *sessions.Flow
	if tracked, ok := targetConn.(*trackedConn); ok {
		userID = tracked.userID
		flow = tracked.flow
	}
	_, err := io.Copy(countingWriter{
		writer: targetConn,
		ctx:    ctx,
		limit:  rateLimiter,
		add: func(n int64) {
			collector.AddTCPClientToTarget(userID, n)
			if flow != nil {
				flow.AddClientToTarget(n)
			}
		},
	}, stream)
	if closeWriter, ok := targetConn.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
	errCh <- err
}

func copyTargetToClient(ctx context.Context, stream *quic.Stream, targetConn net.Conn, collector *metrics.Collector, rateLimiter *limiter.ByteLimiter, errCh chan<- error) {
	userID := "anonymous"
	var flow *sessions.Flow
	if tracked, ok := targetConn.(*trackedConn); ok {
		userID = tracked.userID
		flow = tracked.flow
	}
	_, err := io.Copy(countingWriter{
		writer: stream,
		ctx:    ctx,
		limit:  rateLimiter,
		add: func(n int64) {
			collector.AddTCPTargetToClient(userID, n)
			if flow != nil {
				flow.AddTargetToClient(n)
			}
		},
	}, targetConn)
	_ = stream.Close()
	errCh <- err
}

type countingWriter struct {
	writer io.Writer
	ctx    context.Context
	limit  *limiter.ByteLimiter
	add    func(int64)
}

func (w countingWriter) Write(p []byte) (int, error) {
	if err := w.limit.Wait(w.ctx, len(p)); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(p)
	if n > 0 {
		w.add(int64(n))
	}
	return n, err
}

func (s *connSession) removeUDPFlow(flowID uint32) {
	s.mu.Lock()
	delete(s.flows, flowID)
	s.mu.Unlock()
	s.record.RemoveFlow(flowID)
}

type trackedConn struct {
	net.Conn
	userID string
	flow   *sessions.Flow
}

func (c *trackedConn) CloseWrite() error {
	closeWriter, ok := c.Conn.(interface{ CloseWrite() error })
	if !ok {
		return nil
	}
	return closeWriter.CloseWrite()
}

type udpFlowConfig struct {
	id          uint32
	conn        *net.UDPConn
	idleTimeout time.Duration
	collector   *metrics.Collector
	record      *sessions.Flow
	userID      string
	limiter     *limiter.ByteLimiter
	send        func([]byte) error
	remove      func(uint32)
	logger      *slog.Logger
}

type udpFlow struct {
	id          uint32
	conn        *net.UDPConn
	idleTimeout time.Duration
	collector   *metrics.Collector
	record      *sessions.Flow
	userID      string
	limiter     *limiter.ByteLimiter
	send        func([]byte) error
	remove      func(uint32)
	logger      *slog.Logger
	sendQ       chan []byte
	done        chan struct{}
	closed      atomic.Bool
	lastSeen    atomic.Int64
	seq         atomic.Uint32
}

func newUDPFlow(cfg udpFlowConfig) *udpFlow {
	flow := &udpFlow{
		id:          cfg.id,
		conn:        cfg.conn,
		idleTimeout: cfg.idleTimeout,
		collector:   cfg.collector,
		record:      cfg.record,
		userID:      cfg.userID,
		limiter:     cfg.limiter,
		send:        cfg.send,
		remove:      cfg.remove,
		logger:      cfg.logger,
		sendQ:       make(chan []byte, 64),
		done:        make(chan struct{}),
	}
	flow.touch()
	return flow
}

func (f *udpFlow) start() {
	go f.readLoop()
	go f.sendLoop()
	go f.idleLoop()
}

func (f *udpFlow) write(payload []byte) error {
	f.touch()
	if !f.limiter.Allow(len(payload)) {
		f.logger.Debug("drop udp packet because rate limit exceeded", "direction", "client_to_target", "bytes", len(payload))
		return nil
	}
	n, err := f.conn.Write(payload)
	if n > 0 {
		f.collector.AddUDPClientToTarget(f.user(), int64(n))
		if f.record != nil {
			f.record.AddClientToTarget(int64(n))
		}
	}
	return err
}

func (f *udpFlow) readLoop() {
	buf := make([]byte, 2048)
	for {
		n, err := f.conn.Read(buf)
		if err != nil {
			f.close("upstream_closed")
			return
		}
		f.touch()
		if !f.limiter.Allow(n) {
			f.logger.Debug("drop udp packet because rate limit exceeded", "direction", "target_to_client", "bytes", n)
			continue
		}
		f.collector.AddUDPTargetToClient(f.user(), int64(n))
		if f.record != nil {
			f.record.AddTargetToClient(int64(n))
		}
		packet := protocol.MarshalDatagram(protocol.Datagram{
			Version: protocol.Version,
			Type:    protocol.DatagramTypeUDP,
			FlowID:  f.id,
			Seq:     f.seq.Add(1),
			Payload: append([]byte(nil), buf[:n]...),
		})
		f.enqueue(packet)
	}
}

func (f *udpFlow) sendLoop() {
	for {
		select {
		case <-f.done:
			return
		case packet := <-f.sendQ:
			if err := f.send(packet); err != nil {
				f.logger.Debug("send datagram failed", "error", err)
				f.close("send_error")
				return
			}
		}
	}
}

func (f *udpFlow) idleLoop() {
	ticker := time.NewTicker(f.idleTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-f.done:
			return
		case <-ticker.C:
			lastSeen := time.Unix(0, f.lastSeen.Load())
			if time.Since(lastSeen) > f.idleTimeout {
				f.logger.Debug("udp flow idle timeout")
				f.close("idle_timeout")
				return
			}
		}
	}
}

func (f *udpFlow) enqueue(packet []byte) {
	select {
	case f.sendQ <- packet:
		return
	default:
	}

	select {
	case <-f.sendQ:
	default:
	}

	select {
	case f.sendQ <- packet:
	default:
		f.logger.Debug("drop datagram because send queue is full")
	}
}

func (f *udpFlow) touch() {
	f.lastSeen.Store(time.Now().UnixNano())
	if f.record != nil {
		f.record.Touch()
	}
}

func (f *udpFlow) user() string {
	if f.userID == "" {
		return "anonymous"
	}
	return f.userID
}

func (f *udpFlow) close(reason string) {
	if !f.closed.CompareAndSwap(false, true) {
		return
	}
	close(f.done)
	_ = f.conn.Close()
	f.collector.UDPFlowClosed(reason)
	f.remove(f.id)
}
