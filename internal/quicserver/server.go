package quicserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"

	"gaccel-node/internal/auth"
	"gaccel-node/internal/config"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/protocol"
	"gaccel-node/internal/routepolicy"
	"gaccel-node/internal/router"
	"gaccel-node/internal/sessions"
)

type Server struct {
	cfg           *config.Manager
	logger        *slog.Logger
	collector     *metrics.Collector
	sessions      *sessions.Registry
	nextSessionID uint64
}

func New(cfg *config.Manager, logger *slog.Logger, collector *metrics.Collector, sessionRegistry *sessions.Registry) (*Server, error) {
	_, err := router.New(cfg.Current().Security)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:       cfg,
		logger:    logger.With("component", "quic"),
		collector: collector,
		sessions:  sessionRegistry,
	}, nil
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	cfg := s.cfg.Current()
	tlsConfig, err := loadTLSConfig(cfg)
	if err != nil {
		return err
	}

	listener, err := quic.ListenAddr(cfg.Server.Listen, tlsConfig, &quic.Config{
		EnableDatagrams: true,
		MaxIdleTimeout:  cfg.Limits.QUICIdleTimeout,
	})
	if err != nil {
		return err
	}
	defer listener.Close()

	s.logger.Info("quic listening", "listen", cfg.Server.Listen, "alpn", cfg.Server.ALPN)

	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		if s.collector.Snapshot().ActiveQUICConnections >= int64(s.cfg.Current().Limits.MaxQUICConnections) {
			_ = conn.CloseWithError(1, "connection limit exceeded")
			continue
		}
		s.collector.ConnOpened()
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn *quic.Conn) {
	connCtx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		s.collector.ConnClosed()
		_ = conn.CloseWithError(0, "closed")
	}()

	remote := conn.RemoteAddr().String()
	logger := s.logger.With("remote", remote)
	sessionID := fmt.Sprintf("%d", atomic.AddUint64(&s.nextSessionID, 1))
	sessionRecord := s.sessions.Register(sessionID, remote)
	session := newConnSession(conn, s.cfg, s.collector, sessionRecord, logger)
	defer func() {
		reason, source := session.closeReasonSource()
		s.sessions.End(sessionID, reason, source, time.Now())
		session.close()
		s.sessions.Remove(sessionID)
	}()

	logger.Info("accepted connection")

	go s.drainDatagrams(connCtx, session, conn, logger)
	go session.monitorHeartbeat(connCtx)

	for {
		stream, err := conn.AcceptStream(connCtx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				session.markCloseReason(classifySessionClose(err, ctx))
				return
			}
			logger.Debug("accept stream stopped", "error", err)
			session.markCloseReason(classifySessionClose(err, ctx))
			return
		}
		go s.handleControlStream(connCtx, session, stream, logger)
	}
}

func (s *Server) handleControlStream(ctx context.Context, session *connSession, stream *quic.Stream, logger *slog.Logger) {
	codec := protocol.NewCodec(stream)

	defer stream.Close()

	for {
		msg, err := codec.Read()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				logger.Debug("read control message", "error", err)
			}
			return
		}

		switch msg.Type {
		case protocol.MessageHello:
			session.record.SetClientInfo(msg.ClientID, msg.ClientVersion, msg.ClientPlatform, msg.Version)
			_ = codec.Write(protocol.Message{
				Type:    protocol.MessageHello,
				Version: protocol.Version,
				Server:  serverInfo(s.cfg.Current()),
			})
		case protocol.MessageAuth:
			session.record.SetClientInfo(msg.ClientID, msg.ClientVersion, msg.ClientPlatform, msg.Version)
			principal, err := auth.New(s.cfg.Current().Auth).Authenticate(msg.Token)
			if err != nil {
				_ = codec.Write(protocol.ErrorMessage(authErrorCode(err), authErrorText(err)))
				return
			}
			if err := session.setPrincipal(principal); err != nil {
				_ = codec.Write(protocol.ErrorMessage(authErrorCode(err), err.Error()))
				return
			}
			logger.Info("authenticated", "user_id", principal.UserID)
			_ = codec.Write(protocol.Message{
				Type:     protocol.MessageAuthOK,
				Version:  protocol.Version,
				UserID:   principal.UserID,
				DeviceID: principal.DeviceID,
				Server:   serverInfo(s.cfg.Current()),
			})
		case protocol.MessagePing:
			if !session.authenticated() {
				_ = codec.Write(protocol.ErrorMessage(protocol.ErrorUnauthorized, "authenticate first"))
				return
			}
			session.record.MarkPing()
			_ = codec.Write(protocol.Message{Type: protocol.MessagePong})
		case protocol.MessageOpenUDP:
			if !session.authenticated() {
				_ = codec.Write(protocol.ErrorMessage(protocol.ErrorUnauthorized, "authenticate first"))
				return
			}
			flowID, err := session.openUDP(ctx, msg.TargetHost, msg.TargetPort, msg.Metadata)
			if err != nil {
				s.collector.FlowOpenFailed("udp", flowFailureReason(err))
				logger.Debug("open udp failed", "target_host", msg.TargetHost, "target_port", msg.TargetPort, "metadata", string(msg.Metadata), "error", err)
				_ = codec.Write(protocol.ErrorMessage(flowErrorCode("udp", err), err.Error()))
				return
			}
			_ = codec.Write(protocol.Message{
				Type:   protocol.MessageOpenUDP,
				FlowID: flowID,
			})
		case protocol.MessageOpenTCP:
			if !session.authenticated() {
				_ = codec.Write(protocol.ErrorMessage(protocol.ErrorUnauthorized, "authenticate first"))
				return
			}
			flowID, targetConn, release, err := session.openTCPTarget(ctx, msg.TargetHost, msg.TargetPort, msg.Metadata)
			if err != nil {
				s.collector.FlowOpenFailed("tcp", flowFailureReason(err))
				logger.Debug("open tcp failed", "target_host", msg.TargetHost, "target_port", msg.TargetPort, "metadata", string(msg.Metadata), "error", err)
				_ = codec.Write(protocol.ErrorMessage(flowErrorCode("tcp", err), err.Error()))
				return
			}
			_ = codec.Write(protocol.Message{
				Type:   protocol.MessageOpenTCP,
				FlowID: flowID,
			})
			session.relayTCP(ctx, stream, targetConn, flowID, release)
			return
		case protocol.MessageClose:
			if !session.authenticated() {
				_ = codec.Write(protocol.ErrorMessage(protocol.ErrorUnauthorized, "authenticate first"))
				return
			}
			session.closeFlow(msg.FlowID)
		default:
			_ = codec.Write(protocol.ErrorMessage(protocol.ErrorUnknownMessage, "unknown message type"))
		}
	}
}

func (s *Server) drainDatagrams(ctx context.Context, session *connSession, conn *quic.Conn, logger *slog.Logger) {
	for {
		packet, err := conn.ReceiveDatagram(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logger.Debug("receive datagram stopped", "error", err)
			}
			return
		}
		if err := session.handleDatagram(packet); err != nil {
			logger.Debug("drop invalid datagram", "error", err)
		}
	}
}

func flowFailureReason(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, router.ErrTargetDenied) || errors.Is(err, routepolicy.ErrPolicyDenied) {
		return "denied"
	}
	if errors.Is(err, auth.ErrPermissionDenied) {
		return "permission_denied"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "context"
	}
	if strings.Contains(err.Error(), "max flows") {
		return "limit"
	}
	return "error"
}

func authErrorCode(err error) string {
	switch {
	case errors.Is(err, auth.ErrTokenExpired):
		return protocol.ErrorTokenExpired
	case errors.Is(err, auth.ErrTokenNotActive), errors.Is(err, auth.ErrTokenIssuedInFuture):
		return protocol.ErrorTokenNotActive
	case errors.Is(err, auth.ErrTokenMissingExpiration):
		return protocol.ErrorTokenMissingExp
	case errors.Is(err, metrics.ErrUserConnectionLimitExceeded):
		return protocol.ErrorMaxConnectionsExceeded
	case errors.Is(err, auth.ErrInvalidToken):
		return protocol.ErrorTokenInvalid
	default:
		return protocol.ErrorAuthFailed
	}
}

func authErrorText(err error) string {
	switch authErrorCode(err) {
	case protocol.ErrorTokenExpired:
		return "token expired"
	case protocol.ErrorTokenNotActive:
		return "token not active"
	case protocol.ErrorTokenMissingExp:
		return "token missing exp"
	case protocol.ErrorTokenInvalid:
		return "invalid token"
	default:
		return "authentication failed"
	}
}

func flowErrorCode(network string, err error) string {
	switch {
	case errors.Is(err, auth.ErrPermissionDenied):
		return protocol.ErrorPermissionDenied
	case errors.Is(err, router.ErrTargetDenied), errors.Is(err, routepolicy.ErrPolicyDenied):
		return protocol.ErrorTargetDenied
	case strings.Contains(err.Error(), "max flows"):
		return protocol.ErrorMaxFlowsExceeded
	default:
		if network == "tcp" {
			return protocol.ErrorOpenTCPFailed
		}
		return protocol.ErrorOpenUDPFailed
	}
}

func serverInfo(cfg *config.Config) *protocol.ServerInfo {
	keepalive := int(cfg.Limits.HeartbeatInterval.Seconds())
	if keepalive <= 0 {
		keepalive = 15
	}
	return &protocol.ServerInfo{
		ALPN:                            cfg.Server.ALPN,
		ProtocolVersion:                 protocol.Version,
		Capabilities:                    []string{"auth_hmac", "udp_datagram", "tcp_stream", "ping", "flow_close_notify", "flow_metadata", "route_policy"},
		KeepaliveIntervalSeconds:        keepalive,
		DatagramHeaderBytes:             protocol.DatagramHeaderLen,
		RecommendedDatagramBytes:        protocol.RecommendedDatagramBytes,
		RecommendedDatagramPayloadBytes: protocol.RecommendedDatagramPayloadBytes,
		TokenPolicy:                     "validated_on_auth",
	}
}

func classifySessionClose(err error, parent context.Context) (string, string) {
	if parent.Err() != nil || errors.Is(err, context.Canceled) {
		return "node_shutdown", "node"
	}
	if errors.Is(err, io.EOF) {
		return "client_shutdown", "client"
	}
	if err == nil {
		return "client_shutdown", "client"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "heartbeat_timeout"):
		return "heartbeat_timeout", "node"
	case strings.Contains(text, "idle") || strings.Contains(text, "timeout"):
		return "quic_idle_timeout", "node"
	case strings.Contains(text, "closed"):
		return "client_shutdown", "client"
	default:
		return "network_lost", "network"
	}
}

func loadTLSConfig(cfg *config.Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.Server.CertFile, cfg.Server.KeyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{cfg.Server.ALPN},
		MinVersion:   tls.VersionTLS13,
	}, nil
}
