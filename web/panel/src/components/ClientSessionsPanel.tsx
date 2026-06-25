import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, Button, Empty, Input, Select, Space, Table, Tag, Typography } from "antd";
import type { TableColumnsType } from "antd";
import { Activity, Clock3, Network, RefreshCw, Search, TimerOff, UserRound } from "lucide-react";
import { listClientSessions } from "../api";
import type { ClientSession, ClientSessionOverview, PanelNode } from "../types";

const { Text, Title } = Typography;

const windowOptions = [
  { value: 1, label: "最近 1 小时" },
  { value: 6, label: "最近 6 小时" },
  { value: 24, label: "最近 24 小时" },
  { value: 72, label: "最近 3 天" },
  { value: 168, label: "最近 7 天" }
];

const statusOptions = [
  { value: "online", label: "在线" },
  { value: "closed", label: "已断开" }
];

const closeReasonOptions = [
  { value: "heartbeat_timeout", label: "心跳超时" },
  { value: "quic_idle_timeout", label: "QUIC 空闲超时" },
  { value: "client_shutdown", label: "客户端关闭" },
  { value: "network_lost", label: "网络中断" },
  { value: "node_shutdown", label: "节点重启/停止" }
];

function emptyOverview(): ClientSessionOverview {
  return {
    online_sessions: 0,
    closed_sessions: 0,
    timeout_sessions: 0,
    total_sessions: 0,
    total_duration_seconds: 0,
    udp_client_to_target_bytes: 0,
    udp_target_to_client_bytes: 0,
    tcp_client_to_target_bytes: 0,
    tcp_target_to_client_bytes: 0
  };
}

function formatDate(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatDuration(seconds?: number) {
  const value = Math.max(0, Number(seconds) || 0);
  if (value < 60) return `${value}s`;
  if (value < 3600) return `${Math.floor(value / 60)}m ${value % 60}s`;
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  return `${hours}h ${minutes}m`;
}

function formatBytes(value?: number) {
  const bytes = Math.max(0, Number(value) || 0);
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let next = bytes / 1024;
  for (const unit of units) {
    if (next < 1024) {
      return `${next.toFixed(next >= 100 ? 0 : next >= 10 ? 1 : 2)} ${unit}`;
    }
    next /= 1024;
  }
  return `${next.toFixed(2)} PB`;
}

function statusText(value?: string) {
  switch (value) {
    case "online":
      return "在线";
    case "closed":
      return "已断开";
    default:
      return value || "-";
  }
}

function statusColor(value?: string) {
  switch (value) {
    case "online":
      return "success";
    case "closed":
      return "default";
    default:
      return "warning";
  }
}

function closeReasonText(value?: string) {
  switch (value) {
    case "heartbeat_timeout":
      return "心跳超时";
    case "quic_idle_timeout":
      return "QUIC 空闲超时";
    case "client_shutdown":
      return "客户端关闭";
    case "node_shutdown":
      return "节点重启/停止";
    case "network_lost":
      return "网络中断";
    default:
      return value || "-";
  }
}

function closeSourceText(value?: string) {
  switch (value) {
    case "client":
      return "客户端";
    case "node":
      return "节点";
    case "network":
      return "网络";
    default:
      return value || "-";
  }
}

function sessionTraffic(session: ClientSession) {
  return (
    session.udp_client_to_target_bytes +
    session.udp_target_to_client_bytes +
    session.tcp_client_to_target_bytes +
    session.tcp_target_to_client_bytes
  );
}

function overviewTraffic(overview: ClientSessionOverview) {
  return (
    overview.udp_client_to_target_bytes +
    overview.udp_target_to_client_bytes +
    overview.tcp_client_to_target_bytes +
    overview.tcp_target_to_client_bytes
  );
}

export function ClientSessionsPanel({
  nodes,
  onRequestError
}: {
  nodes: PanelNode[];
  onRequestError: (error: unknown) => void;
}) {
  const [sessions, setSessions] = useState<ClientSession[]>([]);
  const [overview, setOverview] = useState<ClientSessionOverview>(emptyOverview);
  const [windowHours, setWindowHours] = useState(24);
  const [nodeID, setNodeID] = useState("");
  const [status, setStatus] = useState("");
  const [closeReason, setCloseReason] = useState("");
  const [userID, setUserID] = useState("");
  const [deviceID, setDeviceID] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const nodeOptions = useMemo(
    () =>
      nodes.map((node) => ({
        value: node.node_id,
        label: `${node.name || node.node_id} (${node.node_id})`
      })),
    [nodes]
  );

  const cards = useMemo(
    () => [
      {
        key: "online",
        icon: <Activity size={18} />,
        label: "在线会话",
        value: overview.online_sessions,
        hint: "当前仍保持 QUIC 连接"
      },
      {
        key: "closed",
        icon: <Clock3 size={18} />,
        label: "已结束会话",
        value: overview.closed_sessions,
        hint: "窗口内已上报结束"
      },
      {
        key: "timeout",
        icon: <TimerOff size={18} />,
        label: "超时断开",
        value: overview.timeout_sessions,
        hint: "心跳或 QUIC 空闲超时"
      },
      {
        key: "traffic",
        icon: <Network size={18} />,
        label: "会话流量",
        value: formatBytes(overviewTraffic(overview)),
        hint: "TCP 与 UDP 合计"
      }
    ],
    [overview]
  );

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const params = new URLSearchParams({
        window_hours: String(windowHours),
        limit: "100"
      });
      if (nodeID) params.set("node_id", nodeID);
      if (status) params.set("status", status);
      if (closeReason) params.set("close_reason", closeReason);
      if (userID.trim()) params.set("user_id", userID.trim());
      if (deviceID.trim()) params.set("device_id", deviceID.trim());
      const response = await listClientSessions(params);
      setSessions(Array.isArray(response.sessions) ? response.sessions : []);
      setOverview(response.overview || emptyOverview());
    } catch (err) {
      setError(err instanceof Error ? err.message : "读取客户端会话失败");
      onRequestError(err);
    } finally {
      setLoading(false);
    }
  }, [closeReason, deviceID, nodeID, onRequestError, status, userID, windowHours]);

  useEffect(() => {
    void load();
  }, [load]);

  const columns: TableColumnsType<ClientSession> = [
    {
      title: "状态",
      dataIndex: "status",
      width: 105,
      render: (value) => <Tag color={statusColor(value)}>{statusText(value)}</Tag>
    },
    {
      title: "用户 / 设备",
      width: 220,
      render: (_value, record) => (
        <div className="session-identity">
          <strong>{record.user_id || "-"}</strong>
          <Text type="secondary">{record.device_id || "-"}</Text>
          <Text className="mono subtle">{record.client_platform || record.client_id || "-"}</Text>
        </div>
      )
    },
    {
      title: "节点 / 来源",
      width: 230,
      render: (_value, record) => (
        <div className="session-identity">
          <strong>{record.node_id || "-"}</strong>
          <Text className="mono subtle">{record.remote_addr || "-"}</Text>
        </div>
      )
    },
    {
      title: "连接时间",
      width: 210,
      render: (_value, record) => (
        <div className="session-identity compact">
          <Text>接入 {formatDate(record.connected_at)}</Text>
          <Text type="secondary">认证 {formatDate(record.authenticated_at)}</Text>
          <Text type="secondary">结束 {formatDate(record.ended_at)}</Text>
        </div>
      )
    },
    {
      title: "在线时长",
      dataIndex: "duration_seconds",
      width: 110,
      render: (value) => <Text className="mono">{formatDuration(value)}</Text>
    },
    {
      title: "断开原因",
      width: 150,
      render: (_value, record) => (
        <div className="session-identity compact">
          <Text>{closeReasonText(record.close_reason)}</Text>
          <Text type="secondary">{closeSourceText(record.close_source)}</Text>
        </div>
      )
    },
    {
      title: "游戏 / 策略",
      width: 190,
      render: (_value, record) => (
        <div className="compact-tags">
          {(record.game_ids || []).slice(0, 3).map((item) => (
            <Tag key={`game-${item}`}>{item}</Tag>
          ))}
          {(record.policy_ids || []).slice(0, 3).map((item) => (
            <Tag color="blue" key={`policy-${item}`}>
              {item}
            </Tag>
          ))}
          {!record.game_ids?.length && !record.policy_ids?.length ? <Text type="secondary">-</Text> : null}
        </div>
      )
    },
    {
      title: "TCP / UDP",
      width: 125,
      render: (_value, record) => (
        <Text className="mono subtle">
          {record.tcp_flows ?? 0} / {record.udp_flows ?? 0}
        </Text>
      )
    },
    {
      title: "流量",
      width: 120,
      render: (_value, record) => <strong>{formatBytes(sessionTraffic(record))}</strong>
    },
    {
      title: "最后心跳",
      width: 170,
      render: (_value, record) => (
        <div className="session-identity compact">
          <Text>{formatDate(record.last_seen_at)}</Text>
          <Text type="secondary">ping {formatDate(record.last_ping_at)}</Text>
        </div>
      )
    }
  ];

  return (
    <main className="workbench session-panel">
      <div className="session-header">
        <div>
          <Text className="eyebrow">客户端会话</Text>
          <Title level={3}>连接与断开记录</Title>
          <Text type="secondary">
            查看客户端什么时候连接节点、认证是否成功、什么时候结束，以及是否因为心跳超时被判定断开。
          </Text>
        </div>
        <Space wrap>
          <Select value={windowHours} options={windowOptions} onChange={setWindowHours} />
          <Button icon={<RefreshCw size={16} />} loading={loading} onClick={() => void load()}>
            刷新
          </Button>
        </Space>
      </div>

      <section className="session-metrics">
        {cards.map((card) => (
          <div className={`session-metric ${card.key}`} key={card.key}>
            <div className="session-metric-icon">{card.icon}</div>
            <div>
              <Text>{card.label}</Text>
              <strong>{card.value}</strong>
              <span>{card.hint}</span>
            </div>
          </div>
        ))}
      </section>

      <div className="session-filters">
        <Input
          allowClear
          prefix={<Search size={16} />}
          value={userID}
          onChange={(event) => setUserID(event.target.value)}
          onPressEnter={() => void load()}
          placeholder="用户 ID"
        />
        <Input
          allowClear
          prefix={<UserRound size={16} />}
          value={deviceID}
          onChange={(event) => setDeviceID(event.target.value)}
          onPressEnter={() => void load()}
          placeholder="设备 ID"
        />
        <Select allowClear showSearch value={nodeID || undefined} options={nodeOptions} onChange={(value) => setNodeID(value ?? "")} placeholder="节点" />
        <Select allowClear value={status || undefined} options={statusOptions} onChange={(value) => setStatus(value ?? "")} placeholder="状态" />
        <Select
          allowClear
          value={closeReason || undefined}
          options={closeReasonOptions}
          onChange={(value) => setCloseReason(value ?? "")}
          placeholder="断开原因"
        />
      </div>

      {error ? <Alert className="inline-alert" type="error" showIcon message={error} /> : null}

      <section className="session-section">
        <Table<ClientSession>
          rowKey={(record) => `${record.node_id}:${record.session_id}`}
          loading={loading}
          columns={columns}
          dataSource={sessions}
          pagination={{ pageSize: 10 }}
          scroll={{ x: 1520 }}
          locale={{
            emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无客户端会话记录" />
          }}
        />
      </section>
    </main>
  );
}
