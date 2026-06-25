package panel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	networkDiagnosticsWindow            = "15 min"
	recommendedUDPBufferBytes           = 64 * 1024 * 1024
	recommendedUDPDefaultBufferBytes    = 8 * 1024 * 1024
	recommendedNetworkDeviceBacklog     = 100000
	networkDiagnosticsLowRiskThreshold  = 30
	networkDiagnosticsHighRiskThreshold = 70
)

type NodeNetworkDiagnosticsResponse struct {
	Status          string             `json:"status"`
	NodeID          string             `json:"node_id"`
	GeneratedAt     time.Time          `json:"generated_at"`
	RiskLevel       string             `json:"risk_level"`
	RiskScore       int                `json:"risk_score"`
	Summary         DiagnosticSummary  `json:"summary"`
	Metrics         NodeNetworkMetrics `json:"metrics"`
	Checks          []DiagnosticCheck  `json:"checks"`
	Recommendations []string           `json:"recommendations"`
	Raw             map[string]string  `json:"raw,omitempty"`
}

type NodeNetworkMetrics struct {
	ReceiveBufferMax     int64  `json:"receive_buffer_max"`
	SendBufferMax        int64  `json:"send_buffer_max"`
	ReceiveBufferDefault int64  `json:"receive_buffer_default"`
	SendBufferDefault    int64  `json:"send_buffer_default"`
	NetdevMaxBacklog     int64  `json:"netdev_max_backlog"`
	UDPSocketCount       int64  `json:"udp_socket_count"`
	UDPRecvQueueTotal    int64  `json:"udp_recv_queue_total"`
	UDPSendQueueTotal    int64  `json:"udp_send_queue_total"`
	RXDropped            int64  `json:"rx_dropped"`
	TXDropped            int64  `json:"tx_dropped"`
	RXErrors             int64  `json:"rx_errors"`
	TXErrors             int64  `json:"tx_errors"`
	UDPBufferWarnings    int64  `json:"udp_buffer_warnings"`
	NodeWarnOrErrorLogs  int64  `json:"node_warn_or_error_logs"`
	AcceptedConnections  int64  `json:"accepted_connections"`
	Authenticated        int64  `json:"authenticated_connections"`
	CPUCount             int64  `json:"cpu_count"`
	LoadAverage          string `json:"load_average"`
}

func (s *Server) handleGetNodeNetworkDiagnostics(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	resp, err := s.RunNodeNetworkDiagnostics(r.Context(), strings.TrimSpace(nodeID), time.Now().UTC())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		if strings.Contains(err.Error(), "credential") {
			writeError(w, http.StatusBadRequest, "credential_required", "node SSH credential is required before network diagnostics")
			return
		}
		s.logger.Error("node network diagnostics", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "network_diagnostics_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) RunNodeNetworkDiagnostics(ctx context.Context, nodeID string, now time.Time) (NodeNetworkDiagnosticsResponse, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !nodeIDPattern.MatchString(nodeID) {
		return NodeNetworkDiagnosticsResponse{}, fmt.Errorf("node_id is invalid")
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return NodeNetworkDiagnosticsResponse{}, err
	}
	credential, err := s.store.GetNodeCredential(ctx, nodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return NodeNetworkDiagnosticsResponse{}, errors.New("node SSH credential is not configured")
		}
		return NodeNetworkDiagnosticsResponse{}, err
	}
	client, err := s.openSSHClient(ctx, *node, *credential)
	if err != nil {
		return NodeNetworkDiagnosticsResponse{}, err
	}
	defer client.Close()
	_ = s.store.MarkNodeCredentialUsed(ctx, nodeID, now)

	runner := sshRunner{
		client:  client,
		timeout: s.cfg.Deploy.CommandTimeout,
		root:    credential.SudoMode == CredentialSudoSudo,
	}
	output, err := runner.runRoot(ctx, "network-diagnostics", nodeNetworkDiagnosticsCommand(), deployLogger{})
	if err != nil {
		return NodeNetworkDiagnosticsResponse{}, fmt.Errorf("collect network diagnostics failed: %w", err)
	}
	values := parseNetworkDiagnosticsOutput(output)
	return BuildNodeNetworkDiagnostics(nodeID, values, now), nil
}

func nodeNetworkDiagnosticsCommand() string {
	return `set -eu
read_sysctl() {
  sysctl -n "$1" 2>/dev/null || printf "0"
}

printf "sysctl.net.core.rmem_max=%s\n" "$(read_sysctl net.core.rmem_max)"
printf "sysctl.net.core.wmem_max=%s\n" "$(read_sysctl net.core.wmem_max)"
printf "sysctl.net.core.rmem_default=%s\n" "$(read_sysctl net.core.rmem_default)"
printf "sysctl.net.core.wmem_default=%s\n" "$(read_sysctl net.core.wmem_default)"
printf "sysctl.net.core.netdev_max_backlog=%s\n" "$(read_sysctl net.core.netdev_max_backlog)"
printf "sysctl.net.ipv4.udp_rmem_min=%s\n" "$(read_sysctl net.ipv4.udp_rmem_min)"
printf "sysctl.net.ipv4.udp_wmem_min=%s\n" "$(read_sysctl net.ipv4.udp_wmem_min)"

if [ -r /proc/net/dev ]; then
  awk 'NR>2 {rx_drop += $5; tx_drop += $13; rx_err += $4; tx_err += $12} END {
    printf "netdev.rx_dropped=%d\n", rx_drop + 0
    printf "netdev.tx_dropped=%d\n", tx_drop + 0
    printf "netdev.rx_errors=%d\n", rx_err + 0
    printf "netdev.tx_errors=%d\n", tx_err + 0
  }' /proc/net/dev
else
  printf "netdev.rx_dropped=0\nnetdev.tx_dropped=0\nnetdev.rx_errors=0\nnetdev.tx_errors=0\n"
fi

if command -v ss >/dev/null 2>&1; then
  ss -u -a -n 2>/dev/null | awk 'NR>1 {recv += $2 + 0; send += $3 + 0; sockets += 1} END {
    printf "udp.socket_count=%d\n", sockets + 0
    printf "udp.recv_q_total=%d\n", recv + 0
    printf "udp.send_q_total=%d\n", send + 0
  }'
else
  printf "udp.socket_count=0\nudp.recv_q_total=0\nudp.send_q_total=0\n"
fi

journal="$(journalctl -u gaccel-node --since '-15 min' --no-pager 2>/dev/null || true)"
printf "journal.udp_buffer_warnings=%s\n" "$(printf "%s\n" "$journal" | grep -c 'failed to sufficiently increase receive buffer size' || true)"
printf "journal.warn_or_error=%s\n" "$(printf "%s\n" "$journal" | grep -Ec 'level=(WARN|ERROR)' || true)"
printf "journal.accepted_connections=%s\n" "$(printf "%s\n" "$journal" | grep -c 'accepted connection' || true)"
printf "journal.authenticated=%s\n" "$(printf "%s\n" "$journal" | grep -c 'msg=authenticated' || true)"

printf "kernel.cpu_count=%s\n" "$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf "0")"
printf "kernel.loadavg=%s\n" "$(cut -d ' ' -f1-3 /proc/loadavg 2>/dev/null || printf "")"`
}

func parseNetworkDiagnosticsOutput(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	return values
}

func BuildNodeNetworkDiagnostics(nodeID string, values map[string]string, now time.Time) NodeNetworkDiagnosticsResponse {
	metrics := NodeNetworkMetrics{
		ReceiveBufferMax:     int64Value(values, "sysctl.net.core.rmem_max"),
		SendBufferMax:        int64Value(values, "sysctl.net.core.wmem_max"),
		ReceiveBufferDefault: int64Value(values, "sysctl.net.core.rmem_default"),
		SendBufferDefault:    int64Value(values, "sysctl.net.core.wmem_default"),
		NetdevMaxBacklog:     int64Value(values, "sysctl.net.core.netdev_max_backlog"),
		UDPSocketCount:       int64Value(values, "udp.socket_count"),
		UDPRecvQueueTotal:    int64Value(values, "udp.recv_q_total"),
		UDPSendQueueTotal:    int64Value(values, "udp.send_q_total"),
		RXDropped:            int64Value(values, "netdev.rx_dropped"),
		TXDropped:            int64Value(values, "netdev.tx_dropped"),
		RXErrors:             int64Value(values, "netdev.rx_errors"),
		TXErrors:             int64Value(values, "netdev.tx_errors"),
		UDPBufferWarnings:    int64Value(values, "journal.udp_buffer_warnings"),
		NodeWarnOrErrorLogs:  int64Value(values, "journal.warn_or_error"),
		AcceptedConnections:  int64Value(values, "journal.accepted_connections"),
		Authenticated:        int64Value(values, "journal.authenticated"),
		CPUCount:             int64Value(values, "kernel.cpu_count"),
		LoadAverage:          strings.TrimSpace(values["kernel.loadavg"]),
	}
	checks := buildNetworkDiagnosticChecks(metrics)
	summary := summarizeDiagnosticChecks(checks)
	riskScore := networkRiskScore(metrics, summary)
	resp := NodeNetworkDiagnosticsResponse{
		Status:          overallDiagnosticStatus(summary),
		NodeID:          nodeID,
		GeneratedAt:     now,
		RiskLevel:       networkRiskLevel(riskScore),
		RiskScore:       riskScore,
		Summary:         summary,
		Metrics:         metrics,
		Checks:          checks,
		Recommendations: networkRecommendations(metrics, riskScore),
		Raw:             values,
	}
	return resp
}

func buildNetworkDiagnosticChecks(metrics NodeNetworkMetrics) []DiagnosticCheck {
	checks := make([]DiagnosticCheck, 0, 7)
	checks = append(checks, networkThresholdCheck(
		"network.rmem_max",
		"UDP 接收缓冲上限",
		metrics.ReceiveBufferMax,
		recommendedUDPBufferBytes,
		"接收缓冲上限充足",
		"接收缓冲上限偏小，突发 UDP/QUIC 流量下有丢包风险",
	))
	checks = append(checks, networkThresholdCheck(
		"network.wmem_max",
		"UDP 发送缓冲上限",
		metrics.SendBufferMax,
		recommendedUDPBufferBytes,
		"发送缓冲上限充足",
		"发送缓冲上限偏小，突发 UDP/QUIC 流量下有排队和丢包风险",
	))
	checks = append(checks, networkThresholdCheck(
		"network.default_buffer",
		"UDP 默认缓冲",
		minInt64(metrics.ReceiveBufferDefault, metrics.SendBufferDefault),
		recommendedUDPDefaultBufferBytes,
		"默认 socket 缓冲合理",
		"默认 socket 缓冲偏小，建议同步提高 rmem_default/wmem_default",
	))
	checks = append(checks, networkThresholdCheck(
		"network.netdev_backlog",
		"网卡 backlog",
		metrics.NetdevMaxBacklog,
		recommendedNetworkDeviceBacklog,
		"网卡 backlog 合理",
		"网卡 backlog 偏小，高峰包量下内核队列可能过早丢包",
	))
	checks = append(checks, droppedPacketCheck(metrics))
	checks = append(checks, udpQueueCheck(metrics))
	checks = append(checks, journalNetworkCheck(metrics))
	checks = append(checks, loadAverageCheck(metrics))
	return checks
}

func networkThresholdCheck(key string, label string, current int64, recommended int64, okMessage string, warnMessage string) DiagnosticCheck {
	status := DiagnosticStatusOK
	message := okMessage
	if current <= 0 || current < recommended {
		status = DiagnosticStatusWarning
		message = warnMessage
	}
	return newDiagnosticCheck(key, label, status, message, map[string]any{
		"current":     current,
		"recommended": recommended,
	})
}

func droppedPacketCheck(metrics NodeNetworkMetrics) DiagnosticCheck {
	totalDropped := metrics.RXDropped + metrics.TXDropped
	totalErrors := metrics.RXErrors + metrics.TXErrors
	status := DiagnosticStatusOK
	message := "网卡未观察到明显 dropped/error 计数"
	if totalErrors > 0 {
		status = DiagnosticStatusWarning
		message = "网卡存在 error 计数，需要检查宿主机网卡、云厂商线路或内核队列"
	} else if totalDropped > 1000 {
		status = DiagnosticStatusWarning
		message = "网卡 dropped 计数较高，可能存在本机队列或线路拥塞"
	} else if totalDropped > 0 {
		status = DiagnosticStatusWarning
		message = "网卡存在 dropped 计数，建议结合业务高峰和云监控继续观察"
	}
	return newDiagnosticCheck("network.netdev_drops", "网卡丢包计数", status, message, map[string]any{
		"rx_dropped": metrics.RXDropped,
		"tx_dropped": metrics.TXDropped,
		"rx_errors":  metrics.RXErrors,
		"tx_errors":  metrics.TXErrors,
	})
}

func udpQueueCheck(metrics NodeNetworkMetrics) DiagnosticCheck {
	totalQueue := metrics.UDPRecvQueueTotal + metrics.UDPSendQueueTotal
	status := DiagnosticStatusOK
	message := "UDP socket 队列没有明显积压"
	if totalQueue > 0 {
		status = DiagnosticStatusWarning
		message = "UDP socket 队列存在积压，可能出现延迟抖动或包被丢弃"
	}
	return newDiagnosticCheck("network.udp_queue", "UDP 队列积压", status, message, map[string]any{
		"socket_count": metrics.UDPSocketCount,
		"recv_q_total": metrics.UDPRecvQueueTotal,
		"send_q_total": metrics.UDPSendQueueTotal,
	})
}

func journalNetworkCheck(metrics NodeNetworkMetrics) DiagnosticCheck {
	status := DiagnosticStatusOK
	message := "最近 15 分钟没有观察到 QUIC/UDP buffer 告警"
	if metrics.UDPBufferWarnings > 0 {
		status = DiagnosticStatusWarning
		message = "最近日志出现 UDP buffer 告警，需要在面板执行 UDP Buffer 优化"
	} else if metrics.NodeWarnOrErrorLogs > 0 {
		status = DiagnosticStatusWarning
		message = "最近日志存在 WARN/ERROR，建议查看节点 journalctl 详情"
	}
	return newDiagnosticCheck("network.node_journal", "节点网络日志", status, message, map[string]any{
		"window":                    networkDiagnosticsWindow,
		"udp_buffer_warnings":       metrics.UDPBufferWarnings,
		"warn_or_error_logs":        metrics.NodeWarnOrErrorLogs,
		"accepted_connections":      metrics.AcceptedConnections,
		"authenticated_connections": metrics.Authenticated,
	})
}

func loadAverageCheck(metrics NodeNetworkMetrics) DiagnosticCheck {
	status := DiagnosticStatusOK
	message := "节点负载处于合理范围"
	load1 := firstLoadAverage(metrics.LoadAverage)
	if metrics.CPUCount <= 0 || load1 < 0 {
		status = DiagnosticStatusWarning
		message = "无法读取 CPU 或 loadavg，暂不能判断节点负载"
	} else if load1 > float64(metrics.CPUCount)*0.9 {
		status = DiagnosticStatusWarning
		message = "节点 1 分钟负载接近或超过 CPU 核数，可能导致转发延迟抖动"
	}
	return newDiagnosticCheck("network.load", "节点负载", status, message, map[string]any{
		"cpu_count":    metrics.CPUCount,
		"load_average": metrics.LoadAverage,
	})
}

func networkRiskScore(metrics NodeNetworkMetrics, summary DiagnosticSummary) int {
	score := summary.Warning*10 + summary.Error*30
	if metrics.ReceiveBufferMax > 0 && metrics.ReceiveBufferMax < recommendedUDPBufferBytes {
		score += 15
	}
	if metrics.SendBufferMax > 0 && metrics.SendBufferMax < recommendedUDPBufferBytes {
		score += 15
	}
	if metrics.NetdevMaxBacklog > 0 && metrics.NetdevMaxBacklog < recommendedNetworkDeviceBacklog {
		score += 10
	}
	if metrics.RXDropped+metrics.TXDropped > 1000 {
		score += 20
	}
	if metrics.RXErrors+metrics.TXErrors > 0 {
		score += 25
	}
	if metrics.UDPRecvQueueTotal+metrics.UDPSendQueueTotal > 0 {
		score += 20
	}
	if metrics.UDPBufferWarnings > 0 {
		score += 25
	}
	if load1 := firstLoadAverage(metrics.LoadAverage); metrics.CPUCount > 0 && load1 > float64(metrics.CPUCount)*0.9 {
		score += 15
	}
	return int(math.Min(100, float64(score)))
}

func networkRiskLevel(score int) string {
	if score >= networkDiagnosticsHighRiskThreshold {
		return "high"
	}
	if score >= networkDiagnosticsLowRiskThreshold {
		return "medium"
	}
	return "low"
}

func networkRecommendations(metrics NodeNetworkMetrics, score int) []string {
	items := make([]string, 0)
	if metrics.ReceiveBufferMax < recommendedUDPBufferBytes || metrics.SendBufferMax < recommendedUDPBufferBytes ||
		metrics.ReceiveBufferDefault < recommendedUDPDefaultBufferBytes || metrics.SendBufferDefault < recommendedUDPDefaultBufferBytes ||
		metrics.NetdevMaxBacklog < recommendedNetworkDeviceBacklog {
		items = append(items, "先在面板执行 UDP Buffer 优化，把 rmem/wmem、默认缓冲和 netdev backlog 调到游戏加速建议值。")
	}
	if metrics.RXDropped+metrics.TXDropped > 0 || metrics.RXErrors+metrics.TXErrors > 0 {
		items = append(items, "如果网卡 dropped/error 持续增长，优先排查云厂商网络质量、带宽峰值、网卡队列和节点出口线路。")
	}
	if metrics.UDPRecvQueueTotal+metrics.UDPSendQueueTotal > 0 {
		items = append(items, "UDP 队列积压时不要盲目提高并发，先看 CPU/load、单用户连接数和客户端是否在短时间内反复重连。")
	}
	if metrics.UDPBufferWarnings > 0 {
		items = append(items, "节点日志仍有 UDP buffer 告警，修复后需要重启 gaccel-node 并重新体检。")
	}
	if score >= networkDiagnosticsHighRiskThreshold {
		items = append(items, "该节点丢包风险较高，建议暂时减少新用户调度或切换到同区域其他节点。")
	}
	if len(items) == 0 {
		items = append(items, "当前节点本机丢包风险较低；如果用户仍然卡顿，重点排查用户到节点或节点到游戏服的线路质量。")
	}
	return dedupeStrings(items)
}

func int64Value(values map[string]string, key string) int64 {
	value := strings.TrimSpace(values[key])
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func firstLoadAverage(value string) float64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return -1
	}
	parsed, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return -1
	}
	return parsed
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
