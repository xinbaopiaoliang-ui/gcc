package panel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/quic-go/quic-go"

	"gaccel-node/internal/auth"
	"gaccel-node/internal/protocol"
)

const connectivityProbeTimeout = 5 * time.Second

type NodeConnectivityProbeResponse struct {
	Status          string                       `json:"status"`
	NodeID          string                       `json:"node_id"`
	GeneratedAt     time.Time                    `json:"generated_at"`
	Endpoint        string                       `json:"endpoint"`
	AdminURL        string                       `json:"admin_url"`
	Summary         DiagnosticSummary            `json:"summary"`
	Metrics         NodeConnectivityProbeMetrics `json:"metrics"`
	Checks          []DiagnosticCheck            `json:"checks"`
	Recommendations []string                     `json:"recommendations"`
}

type NodeConnectivityProbeMetrics struct {
	ResolvedIPs            []string `json:"resolved_ips"`
	DNSLatencyMS           int64    `json:"dns_latency_ms"`
	AdminTCPLatencyMS      int64    `json:"admin_tcp_latency_ms"`
	AdminHealthLatencyMS   int64    `json:"admin_health_latency_ms"`
	AdminHTTPStatus        int      `json:"admin_http_status"`
	QUICHandshakeLatencyMS int64    `json:"quic_handshake_latency_ms"`
	QUICAuthPingLatencyMS  int64    `json:"quic_auth_ping_latency_ms"`
	ServerALPN             string   `json:"server_alpn"`
	ProtocolVersion        int      `json:"protocol_version"`
	TokenPolicy            string   `json:"token_policy"`
	Capabilities           []string `json:"capabilities"`
}

func (s *Server) handleGetNodeConnectivityProbe(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	resp, err := s.RunNodeConnectivityProbe(r.Context(), strings.TrimSpace(nodeID), time.Now().UTC())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		s.logger.Error("node connectivity probe", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "connectivity_probe_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) RunNodeConnectivityProbe(ctx context.Context, nodeID string, now time.Time) (NodeConnectivityProbeResponse, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !nodeIDPattern.MatchString(nodeID) {
		return NodeConnectivityProbeResponse{}, fmt.Errorf("node_id is invalid")
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return NodeConnectivityProbeResponse{}, err
	}

	metrics := NodeConnectivityProbeMetrics{}
	checks := make([]DiagnosticCheck, 0, 5)

	ips, dnsLatency, dnsErr := probeDNS(ctx, node.EndpointHost)
	metrics.ResolvedIPs = ips
	metrics.DNSLatencyMS = millis(dnsLatency)
	checks = append(checks, dnsProbeCheck(node.EndpointHost, ips, dnsLatency, dnsErr))

	adminURL := nodeAdminHealthURL(*node)
	adminTCPLatency, adminTCPErr := probeTCPConnect(ctx, node.AdminHost, node.AdminPort)
	metrics.AdminTCPLatencyMS = millis(adminTCPLatency)
	checks = append(checks, adminTCPProbeCheck(*node, adminTCPLatency, adminTCPErr))

	adminStatus, adminHealthLatency, adminHealthErr := probeAdminHealth(ctx, adminURL)
	metrics.AdminHTTPStatus = adminStatus
	metrics.AdminHealthLatencyMS = millis(adminHealthLatency)
	checks = append(checks, adminHealthProbeCheck(adminURL, adminStatus, adminHealthLatency, adminHealthErr))

	hmacSecret, hmacErr := s.nodeProbeHMACSecret(*node)
	quicResult := probeQUICRelay(ctx, *node, hmacSecret)
	metrics.QUICHandshakeLatencyMS = millis(quicResult.HandshakeLatency)
	metrics.QUICAuthPingLatencyMS = millis(quicResult.AuthPingLatency)
	metrics.ServerALPN = quicResult.ServerALPN
	metrics.ProtocolVersion = quicResult.ProtocolVersion
	metrics.TokenPolicy = quicResult.TokenPolicy
	metrics.Capabilities = quicResult.Capabilities
	checks = append(checks, quicHandshakeProbeCheck(*node, quicResult))
	checks = append(checks, quicAuthProbeCheck(*node, hmacErr, quicResult))

	summary := summarizeDiagnosticChecks(checks)
	return NodeConnectivityProbeResponse{
		Status:          overallDiagnosticStatus(summary),
		NodeID:          node.NodeID,
		GeneratedAt:     now,
		Endpoint:        net.JoinHostPort(node.EndpointHost, strconv.Itoa(node.EndpointPort)),
		AdminURL:        adminURL,
		Summary:         summary,
		Metrics:         metrics,
		Checks:          checks,
		Recommendations: connectivityRecommendations(*node, checks),
	}, nil
}

func (s *Server) nodeProbeHMACSecret(node Node) (string, error) {
	if !node.HMACSecretConfigured || strings.TrimSpace(node.HMACSecretEncrypted) == "" {
		return "", errors.New("node hmac_secret is not configured")
	}
	if s.secrets == nil {
		return "", errors.New("panel secret box is not configured")
	}
	secret, err := s.secrets.Decrypt(node.HMACSecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt node hmac_secret: %w", err)
	}
	secret = strings.TrimSpace(secret)
	if len(secret) < 16 {
		return "", errors.New("stored node hmac_secret is invalid")
	}
	return secret, nil
}

func probeDNS(ctx context.Context, host string) ([]string, time.Duration, error) {
	host = strings.TrimSpace(host)
	start := time.Now()
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return []string{ip.String()}, time.Since(start), nil
	}
	stepCtx, cancel := context.WithTimeout(ctx, connectivityProbeTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(stepCtx, host)
	if err != nil {
		return nil, time.Since(start), err
	}
	ips := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP.String())
	}
	return ips, time.Since(start), nil
}

func probeTCPConnect(ctx context.Context, host string, port int) (time.Duration, error) {
	if isLoopbackHost(host) {
		return 0, errors.New("admin host is loopback from panel side")
	}
	stepCtx, cancel := context.WithTimeout(ctx, connectivityProbeTimeout)
	defer cancel()
	start := time.Now()
	dialer := &net.Dialer{Timeout: connectivityProbeTimeout}
	conn, err := dialer.DialContext(stepCtx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return time.Since(start), err
	}
	_ = conn.Close()
	return time.Since(start), nil
}

func probeAdminHealth(ctx context.Context, url string) (int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	client := &http.Client{Timeout: connectivityProbeTimeout}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, time.Since(start), err
	}
	defer resp.Body.Close()
	return resp.StatusCode, time.Since(start), nil
}

type quicProbeResult struct {
	HandshakeLatency time.Duration
	AuthPingLatency  time.Duration
	ServerALPN       string
	ProtocolVersion  int
	TokenPolicy      string
	Capabilities     []string
	HandshakeErr     error
	AuthErr          error
}

func probeQUICRelay(ctx context.Context, node Node, hmacSecret string) quicProbeResult {
	result := quicProbeResult{}
	addr := net.JoinHostPort(node.EndpointHost, strconv.Itoa(node.EndpointPort))
	alpn := strings.TrimSpace(node.ALPN)
	if alpn == "" {
		alpn = "gaccel/1"
	}
	stepCtx, cancel := context.WithTimeout(ctx, connectivityProbeTimeout)
	defer cancel()
	start := time.Now()
	conn, err := quic.DialAddr(stepCtx, addr, &tls.Config{
		NextProtos:         []string{alpn},
		InsecureSkipVerify: true,
		ServerName:         node.EndpointHost,
		MinVersion:         tls.VersionTLS13,
	}, &quic.Config{
		EnableDatagrams:      true,
		HandshakeIdleTimeout: connectivityProbeTimeout,
		MaxIdleTimeout:       connectivityProbeTimeout,
	})
	result.HandshakeLatency = time.Since(start)
	if err != nil {
		result.HandshakeErr = err
		return result
	}
	defer conn.CloseWithError(0, "panel connectivity probe done")

	if strings.TrimSpace(hmacSecret) == "" {
		result.AuthErr = errors.New("hmac secret is not available")
		return result
	}

	authStart := time.Now()
	stream, err := conn.OpenStreamSync(stepCtx)
	if err != nil {
		result.AuthErr = err
		return result
	}
	_ = stream.SetDeadline(time.Now().Add(connectivityProbeTimeout))
	codec := protocol.NewCodec(stream)
	if err := codec.Write(protocol.Message{
		Type:           protocol.MessageHello,
		Version:        protocol.Version,
		ClientID:       "panel-connectivity-probe",
		ClientVersion:  "panel",
		ClientPlatform: "panel/server",
	}); err != nil {
		result.AuthErr = err
		return result
	}
	hello, err := codec.Read()
	if err != nil {
		result.AuthErr = err
		return result
	}
	if hello.Type != protocol.MessageHello {
		result.AuthErr = fmt.Errorf("unexpected hello response: %s", hello.Type)
		return result
	}
	if hello.Server != nil {
		result.ServerALPN = hello.Server.ALPN
		result.ProtocolVersion = hello.Server.ProtocolVersion
		result.TokenPolicy = hello.Server.TokenPolicy
		result.Capabilities = append([]string(nil), hello.Server.Capabilities...)
	}
	token, err := auth.SignHMACToken(auth.TokenClaims{
		Subject:        "panel-probe",
		UserID:         "panel-probe",
		DeviceID:       "panel-connectivity-probe",
		ExpiresAt:      time.Now().Add(2 * time.Minute).Unix(),
		NotBefore:      time.Now().Add(-5 * time.Second).Unix(),
		IssuedAt:       time.Now().Unix(),
		MaxConnections: 1,
		RateLimitMbps:  1,
	}, hmacSecret)
	if err != nil {
		result.AuthErr = err
		return result
	}
	if err := codec.Write(protocol.Message{
		Type:           protocol.MessageAuth,
		Version:        protocol.Version,
		Token:          token,
		ClientID:       "panel-connectivity-probe",
		ClientVersion:  "panel",
		ClientPlatform: "panel/server",
	}); err != nil {
		result.AuthErr = err
		return result
	}
	authOK, err := codec.Read()
	if err != nil {
		result.AuthErr = err
		return result
	}
	if authOK.Type != protocol.MessageAuthOK {
		result.AuthErr = messageProbeError(authOK)
		return result
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessagePing}); err != nil {
		result.AuthErr = err
		return result
	}
	pong, err := codec.Read()
	if err != nil {
		result.AuthErr = err
		return result
	}
	if pong.Type != protocol.MessagePong {
		result.AuthErr = messageProbeError(pong)
		return result
	}
	result.AuthPingLatency = time.Since(authStart)
	return result
}

func dnsProbeCheck(host string, ips []string, latency time.Duration, err error) DiagnosticCheck {
	if err != nil {
		return newDiagnosticCheck("connectivity.dns", "入口 DNS", DiagnosticStatusError, "节点入口域名解析失败，客户端也可能无法连接该节点", map[string]any{
			"host":  host,
			"error": err.Error(),
		})
	}
	return newDiagnosticCheck("connectivity.dns", "入口 DNS", DiagnosticStatusOK, "节点入口解析正常", map[string]any{
		"host":       host,
		"ips":        ips,
		"latency_ms": millis(latency),
	})
}

func adminTCPProbeCheck(node Node, latency time.Duration, err error) DiagnosticCheck {
	if err != nil {
		status := DiagnosticStatusError
		message := "控制面板无法 TCP 连接节点 Admin 端口"
		if isLoopbackHost(node.AdminHost) {
			status = DiagnosticStatusWarning
			message = "Admin Host 是回环地址，控制面板服务器无法从公网侧直接探测"
		}
		return newDiagnosticCheck("connectivity.admin_tcp", "Admin TCP", status, message, map[string]any{
			"address": net.JoinHostPort(node.AdminHost, strconv.Itoa(node.AdminPort)),
			"error":   err.Error(),
		})
	}
	return newDiagnosticCheck("connectivity.admin_tcp", "Admin TCP", DiagnosticStatusOK, "控制面板到节点 Admin 端口连通", map[string]any{
		"address":    net.JoinHostPort(node.AdminHost, strconv.Itoa(node.AdminPort)),
		"latency_ms": millis(latency),
	})
}

func adminHealthProbeCheck(url string, statusCode int, latency time.Duration, err error) DiagnosticCheck {
	if err != nil {
		return newDiagnosticCheck("connectivity.admin_health", "Admin /health", DiagnosticStatusWarning, "无法访问节点 Admin /health，节点状态上报可能仍正常，但面板主动探测不可用", map[string]any{
			"url":   url,
			"error": err.Error(),
		})
	}
	status := DiagnosticStatusOK
	message := "节点 Admin /health 正常"
	if statusCode < 200 || statusCode >= 300 {
		status = DiagnosticStatusWarning
		message = "节点 Admin /health 返回非 2xx 状态"
	}
	return newDiagnosticCheck("connectivity.admin_health", "Admin /health", status, message, map[string]any{
		"url":         url,
		"http_status": statusCode,
		"latency_ms":  millis(latency),
	})
}

func quicHandshakeProbeCheck(node Node, result quicProbeResult) DiagnosticCheck {
	if result.HandshakeErr != nil {
		return newDiagnosticCheck("connectivity.quic_handshake", "QUIC 握手", DiagnosticStatusError, "控制面板到节点 QUIC 端口握手失败，优先检查 UDP 端口、防火墙和节点服务", map[string]any{
			"address": net.JoinHostPort(node.EndpointHost, strconv.Itoa(node.EndpointPort)),
			"alpn":    node.ALPN,
			"error":   result.HandshakeErr.Error(),
		})
	}
	return newDiagnosticCheck("connectivity.quic_handshake", "QUIC 握手", DiagnosticStatusOK, "控制面板到节点 QUIC 端口握手成功", map[string]any{
		"address":    net.JoinHostPort(node.EndpointHost, strconv.Itoa(node.EndpointPort)),
		"alpn":       node.ALPN,
		"latency_ms": millis(result.HandshakeLatency),
	})
}

func quicAuthProbeCheck(node Node, hmacErr error, result quicProbeResult) DiagnosticCheck {
	if result.HandshakeErr != nil {
		return newDiagnosticCheck("connectivity.quic_auth", "QUIC 鉴权 Ping", DiagnosticStatusWarning, "QUIC 握手失败，暂不能继续鉴权 Ping", map[string]any{
			"address": net.JoinHostPort(node.EndpointHost, strconv.Itoa(node.EndpointPort)),
		})
	}
	if hmacErr != nil {
		return newDiagnosticCheck("connectivity.quic_auth", "QUIC 鉴权 Ping", DiagnosticStatusWarning, "节点 HMAC Secret 不可用，只完成 QUIC 握手，未做鉴权 Ping", map[string]any{
			"reason": hmacErr.Error(),
		})
	}
	if result.AuthErr != nil {
		return newDiagnosticCheck("connectivity.quic_auth", "QUIC 鉴权 Ping", DiagnosticStatusError, "QUIC 已握手但鉴权或 Ping 失败，检查业务后台同步的 hmac_secret 是否与节点一致", map[string]any{
			"error": result.AuthErr.Error(),
		})
	}
	return newDiagnosticCheck("connectivity.quic_auth", "QUIC 鉴权 Ping", DiagnosticStatusOK, "临时 token 鉴权和 Ping 正常", map[string]any{
		"latency_ms":       millis(result.AuthPingLatency),
		"server_alpn":      result.ServerALPN,
		"protocol_version": result.ProtocolVersion,
		"token_policy":     result.TokenPolicy,
		"capabilities":     result.Capabilities,
	})
}

func connectivityRecommendations(node Node, checks []DiagnosticCheck) []string {
	items := make([]string, 0)
	for _, check := range checks {
		if check.Status == DiagnosticStatusOK {
			continue
		}
		switch check.Key {
		case "connectivity.quic_handshake":
			items = append(items, "如果 QUIC 握手失败，先确认节点 UDP 端口已放行、gaccel-node 正在监听、云安全组没有拦截。")
		case "connectivity.quic_auth":
			items = append(items, "如果 QUIC 鉴权 Ping 失败，重点检查业务后台保存的节点 hmac_secret、控制面板加密副本和节点 /etc/gaccel-node/config.yaml 是否一致。")
		case "connectivity.admin_tcp", "connectivity.admin_health":
			items = append(items, "Admin 探测失败只影响面板主动检查和远程任务，不一定影响客户端 QUIC 转发；需要远程部署/更新时再修复 Admin Host/端口。")
		case "connectivity.dns":
			items = append(items, "如果入口 DNS 失败，客户端应临时使用节点 IP，业务后台也要检查下发的 endpoint_host 是否正确。")
		}
	}
	if isLoopbackHost(node.AdminHost) {
		items = append(items, "当前 Admin Host 是回环地址；如果希望面板远程探测和一键修复，请把节点 Admin 监听改为公网可达地址并做好防火墙限制。")
	}
	if len(items) == 0 {
		items = append(items, "主动探测正常；如果用户仍反馈卡顿，下一步看网络体检、节点到游戏服务器链路、以及客户端本地网络。")
	}
	return dedupeStrings(items)
}

func nodeAdminHealthURL(node Node) string {
	return "http://" + net.JoinHostPort(node.AdminHost, strconv.Itoa(node.AdminPort)) + "/health"
}

func messageProbeError(msg *protocol.Message) error {
	if msg == nil {
		return errors.New("empty response")
	}
	if msg.Type == protocol.MessageError {
		if msg.ErrorCode != "" && msg.Error != "" {
			return fmt.Errorf("%s: %s", msg.ErrorCode, msg.Error)
		}
		if msg.ErrorCode != "" {
			return errors.New(msg.ErrorCode)
		}
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
	}
	return fmt.Errorf("unexpected response: %s", msg.Type)
}

func millis(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64(duration / time.Millisecond)
}
