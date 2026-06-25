import { Button, Empty, Modal, Spin, Tag, Typography } from "antd";
import {
  Activity,
  CheckCircle2,
  Gauge,
  Network,
  Radar,
  ShieldAlert,
  TriangleAlert,
  Wrench
} from "lucide-react";
import type {
  DiagnosticCheck,
  DiagnosticStatus,
  NodeConnectivityProbeResponse,
  NodeDiagnosticsResponse,
  NodeNetworkDiagnosticsResponse,
  PanelNode
} from "../types";

const { Text } = Typography;

function normalizeChecks(value?: DiagnosticCheck[]) {
  return Array.isArray(value) ? value : [];
}

function statusText(status?: DiagnosticStatus | string) {
  if (status === "ok") return "正常";
  if (status === "warning") return "警告";
  if (status === "error") return "错误";
  return status || "-";
}

function statusColor(status?: DiagnosticStatus | string) {
  if (status === "ok") return "success";
  if (status === "warning") return "warning";
  if (status === "error") return "error";
  return "default";
}

function statusIcon(status?: DiagnosticStatus | string) {
  if (status === "ok") return <CheckCircle2 size={16} />;
  if (status === "error") return <ShieldAlert size={16} />;
  return <TriangleAlert size={16} />;
}

function riskText(value?: string) {
  if (value === "low") return "低风险";
  if (value === "medium") return "中风险";
  if (value === "high") return "高风险";
  return value || "-";
}

function riskColor(value?: string) {
  if (value === "low") return "success";
  if (value === "medium") return "warning";
  if (value === "high") return "error";
  return "default";
}

function formatDetail(detail?: Record<string, unknown>) {
  if (!detail || Object.keys(detail).length === 0) {
    return "";
  }
  return JSON.stringify(detail, null, 2);
}

function formatDate(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatBytes(value?: number) {
  const bytes = Number(value || 0);
  if (bytes <= 0) return "-";
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} B`;
}

function formatMS(value?: number) {
  const ms = Number(value || 0);
  if (ms <= 0) return "-";
  return `${ms} ms`;
}

function DiagnosticRows({ checks }: { checks: DiagnosticCheck[] }) {
  if (!checks.length) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无诊断结果" />;
  }
  return (
    <>
      {checks.map((check) => {
        const detail = formatDetail(check.detail);
        return (
          <div className={`diagnostic-row ${check.status}`} key={check.key}>
            <div className="diagnostic-icon">{statusIcon(check.status)}</div>
            <div className="diagnostic-main">
              <div className="diagnostic-title">
                <strong>{check.label}</strong>
                <Tag color={statusColor(check.status)}>{statusText(check.status)}</Tag>
              </div>
              <Text>{check.message}</Text>
              {detail ? <pre>{detail}</pre> : null}
            </div>
          </div>
        );
      })}
    </>
  );
}

function RecommendationList({ title, items }: { title: string; items: string[] }) {
  if (!items.length) return null;
  return (
    <section className="diagnostic-advice">
      <strong>{title}</strong>
      <ul>
        {items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ul>
    </section>
  );
}

function ConnectivityProbePanel({ data }: { data: NodeConnectivityProbeResponse }) {
  const checks = normalizeChecks(data.checks);
  const recommendations = Array.isArray(data.recommendations) ? data.recommendations : [];
  const metrics = data.metrics;
  return (
    <section className="connectivity-probe-panel">
      <div className="network-diagnostic-head">
        <div>
          <strong>主动连通性探测</strong>
          <Text type="secondary">从控制面板服务器探测节点入口、Admin 口和 QUIC 鉴权 Ping。</Text>
        </div>
        <Tag color={statusColor(data.status)}>{statusText(data.status)}</Tag>
      </div>
      <div className="network-metrics-grid">
        <div>
          <span>节点入口</span>
          <strong>{data.endpoint || "-"}</strong>
        </div>
        <div>
          <span>解析结果</span>
          <strong>{metrics?.resolved_ips?.length ? metrics.resolved_ips.join(", ") : "-"}</strong>
        </div>
        <div>
          <span>QUIC 握手</span>
          <strong>{formatMS(metrics?.quic_handshake_latency_ms)}</strong>
        </div>
        <div>
          <span>鉴权 Ping</span>
          <strong>{formatMS(metrics?.quic_auth_ping_latency_ms)}</strong>
        </div>
        <div>
          <span>Admin /health</span>
          <strong>{metrics?.admin_http_status ? `${metrics.admin_http_status}` : "-"}</strong>
        </div>
        <div>
          <span>服务端协议</span>
          <strong>{metrics?.server_alpn || "-"}</strong>
        </div>
      </div>
      <RecommendationList title="处理建议" items={recommendations} />
      <section className="diagnostic-list compact network-checks">
        <DiagnosticRows checks={checks} />
      </section>
    </section>
  );
}

function NetworkDiagnosticsPanel({ data }: { data: NodeNetworkDiagnosticsResponse }) {
  const checks = normalizeChecks(data.checks);
  const recommendations = Array.isArray(data.recommendations) ? data.recommendations : [];
  const metrics = data.metrics;
  return (
    <section className="network-diagnostic-panel">
      <div className="network-diagnostic-head">
        <div>
          <strong>节点本机网络体检</strong>
          <Text type="secondary">通过 SSH 读取 UDP buffer、网卡 dropped/error、UDP 队列和最近节点日志。</Text>
        </div>
        <Tag color={riskColor(data.risk_level)}>
          {riskText(data.risk_level)} {data.risk_score}
        </Tag>
      </div>
      <div className="network-metrics-grid">
        <div>
          <span>接收 / 发送缓冲</span>
          <strong>
            {formatBytes(metrics?.receive_buffer_max)} / {formatBytes(metrics?.send_buffer_max)}
          </strong>
        </div>
        <div>
          <span>UDP 队列</span>
          <strong>{(metrics?.udp_recv_queue_total || 0) + (metrics?.udp_send_queue_total || 0)}</strong>
        </div>
        <div>
          <span>网卡 dropped / error</span>
          <strong>
            {(metrics?.rx_dropped || 0) + (metrics?.tx_dropped || 0)} /{" "}
            {(metrics?.rx_errors || 0) + (metrics?.tx_errors || 0)}
          </strong>
        </div>
        <div>
          <span>负载</span>
          <strong>{metrics?.load_average || "-"}</strong>
        </div>
      </div>
      <RecommendationList title="处理建议" items={recommendations} />
      <section className="diagnostic-list compact network-checks">
        <DiagnosticRows checks={checks} />
      </section>
    </section>
  );
}

export function NodeDiagnosticsModal({
  open,
  loading,
  node,
  diagnostics,
  connectivityProbe,
  networkDiagnostics,
  canManage = false,
  repairingAdmin = false,
  connectivityProbeLoading = false,
  networkDiagnosticsLoading = false,
  tuningUDPBuffer = false,
  onRunConnectivityProbe,
  onRunNetworkDiagnostics,
  onRepairAdmin,
  onTuneUDPBuffer,
  onCancel
}: {
  open: boolean;
  loading: boolean;
  node?: PanelNode;
  diagnostics: NodeDiagnosticsResponse | null;
  connectivityProbe?: NodeConnectivityProbeResponse | null;
  networkDiagnostics?: NodeNetworkDiagnosticsResponse | null;
  canManage?: boolean;
  repairingAdmin?: boolean;
  connectivityProbeLoading?: boolean;
  networkDiagnosticsLoading?: boolean;
  tuningUDPBuffer?: boolean;
  onRunConnectivityProbe?: (node?: PanelNode) => void;
  onRunNetworkDiagnostics?: (node?: PanelNode) => void;
  onRepairAdmin?: (node?: PanelNode) => void;
  onTuneUDPBuffer?: (node?: PanelNode) => void;
  onCancel: () => void;
}) {
  const checks = normalizeChecks(diagnostics?.checks);
  const recommendations = Array.isArray(diagnostics?.recommendations) ? diagnostics.recommendations : [];

  return (
    <Modal
      title={node ? `节点接入自检：${node.node_id}` : "节点接入自检"}
      open={open}
      footer={null}
      width={980}
      onCancel={onCancel}
      destroyOnClose
    >
      {loading ? (
        <div className="diagnostic-loading">
          <Spin />
          <span>正在探测节点 Admin 接口</span>
        </div>
      ) : diagnostics ? (
        <div className="node-diagnostic-body">
          <section className="diagnostic-summary modal-summary">
            <div className="diagnostic-summary-item">
              <span>总体状态</span>
              <strong>{statusText(diagnostics.status)}</strong>
            </div>
            <div className="diagnostic-summary-item ok">
              <span>正常</span>
              <strong>{diagnostics.summary?.ok ?? 0}</strong>
            </div>
            <div className="diagnostic-summary-item warning">
              <span>警告</span>
              <strong>{diagnostics.summary?.warning ?? 0}</strong>
            </div>
            <div className="diagnostic-summary-item error">
              <span>错误</span>
              <strong>{diagnostics.summary?.error ?? 0}</strong>
            </div>
          </section>

          <section className="node-diagnostic-meta">
            <div>
              <span>Admin URL</span>
              <strong>{diagnostics.admin_url || "-"}</strong>
            </div>
            <div>
              <span>策略状态</span>
              <strong>
                {diagnostics.sync_status?.current_policy_revision || "-"} /{" "}
                {diagnostics.sync_status?.desired_policy_revision || "-"}
              </strong>
            </div>
            <div>
              <span>最后上报</span>
              <strong>{formatDate(diagnostics.sync_status?.last_report_at)}</strong>
            </div>
          </section>

          <RecommendationList title="接入自检建议" items={recommendations} />

          {canManage && node ? (
            <section className="diagnostic-actionbar">
              <div>
                <strong>远程检查与修复</strong>
                <span>主动探测不改节点；网络体检只读 SSH；修复和 UDP 优化会创建远程任务。</span>
              </div>
              <div className="diagnostic-action-buttons">
                <Button
                  icon={<Radar size={15} />}
                  loading={connectivityProbeLoading}
                  disabled={repairingAdmin || tuningUDPBuffer || networkDiagnosticsLoading}
                  onClick={() => onRunConnectivityProbe?.(node)}
                >
                  主动探测
                </Button>
                <Button
                  icon={<Network size={15} />}
                  loading={networkDiagnosticsLoading}
                  disabled={repairingAdmin || tuningUDPBuffer || connectivityProbeLoading}
                  onClick={() => onRunNetworkDiagnostics?.(node)}
                >
                  网络体检
                </Button>
                <Button
                  icon={<Wrench size={15} />}
                  loading={repairingAdmin}
                  disabled={networkDiagnosticsLoading || tuningUDPBuffer || connectivityProbeLoading}
                  onClick={() => onRepairAdmin?.(node)}
                >
                  修复 Admin
                </Button>
                <Button
                  type="primary"
                  icon={<Gauge size={15} />}
                  loading={tuningUDPBuffer}
                  disabled={repairingAdmin || networkDiagnosticsLoading || connectivityProbeLoading}
                  onClick={() => onTuneUDPBuffer?.(node)}
                >
                  优化 UDP Buffer
                </Button>
              </div>
            </section>
          ) : null}

          {connectivityProbe ? <ConnectivityProbePanel data={connectivityProbe} /> : null}
          {networkDiagnostics ? <NetworkDiagnosticsPanel data={networkDiagnostics} /> : null}

          {!connectivityProbe && !networkDiagnostics && canManage && node ? (
            <section className="network-diagnostic-placeholder">
              <Activity size={18} />
              <span>
                需要排查连不上、断连或丢包时，先点“主动探测”确认入口可达，再点“网络体检”检查节点本机 UDP 和网卡状态。
              </span>
            </section>
          ) : null}

          <section className="diagnostic-list compact">
            <DiagnosticRows checks={checks} />
          </section>
        </div>
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无节点诊断数据" />
      )}
    </Modal>
  );
}
