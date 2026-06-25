import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, Button, Empty, Select, Space, Table, Tag, Typography } from "antd";
import type { TableColumnsType } from "antd";
import { Activity, AlertTriangle, BarChart3, Gauge, Network, RefreshCw, Route, UsersRound } from "lucide-react";
import { getTrafficOverview } from "../api";
import type {
  TrafficFlowEventStats,
  TrafficNodeStats,
  TrafficOverview,
  TrafficPolicyConsistency,
  TrafficPolicyEventStats,
  TrafficUserStats
} from "../types";

const { Text, Title } = Typography;

const windowOptions = [
  { value: 1, label: "最近 1 小时" },
  { value: 6, label: "最近 6 小时" },
  { value: 24, label: "最近 24 小时" },
  { value: 72, label: "最近 3 天" },
  { value: 168, label: "最近 7 天" }
];

function normalizeArray<T>(value?: T[]) {
  return Array.isArray(value) ? value : [];
}

function formatBytes(value?: number) {
  const bytes = Number(value) || 0;
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

function formatDate(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatAge(seconds?: number) {
  const value = Number(seconds) || 0;
  if (value <= 0) return "-";
  if (value < 60) return `${value}s`;
  if (value < 3600) return `${Math.round(value / 60)}m`;
  return `${(value / 3600).toFixed(value < 86400 ? 1 : 0)}h`;
}

function policyStateText(state?: string) {
  switch (state) {
    case "synced":
      return "已同步";
    case "pending":
      return "待同步";
    case "waiting_report":
      return "等上报";
    case "not_set":
      return "未设置";
    default:
      return state || "-";
  }
}

function policyStateColor(state?: string) {
  switch (state) {
    case "synced":
      return "success";
    case "pending":
    case "waiting_report":
      return "warning";
    default:
      return "default";
  }
}

function eventText(value?: string) {
  switch (value) {
    case "open":
      return "打开";
    case "close":
      return "关闭";
    case "success":
      return "成功";
    case "denied":
      return "拒绝";
    case "session_closed":
      return "会话关闭";
    case "eof":
      return "EOF";
    default:
      return value || "-";
  }
}

function metricCards(traffic: TrafficOverview | null) {
  const totals = traffic?.totals;
  return [
    {
      key: "bytes",
      icon: <BarChart3 size={18} />,
      label: "窗口流量",
      value: formatBytes(totals?.total_bytes),
      hint: traffic?.sample_mode === "window_delta" ? "按窗口增量统计" : "单样本累计值"
    },
    {
      key: "quic",
      icon: <Activity size={18} />,
      label: "活跃 QUIC",
      value: `${totals?.active_quic_connections ?? 0}`,
      hint: "当前客户端连接"
    },
    {
      key: "flows",
      icon: <Network size={18} />,
      label: "TCP / UDP Flow",
      value: `${totals?.active_tcp_flows ?? 0} / ${totals?.active_udp_flows ?? 0}`,
      hint: "实时转发流"
    },
    {
      key: "errors",
      icon: <AlertTriangle size={18} />,
      label: "打开失败",
      value: `${totals?.flow_open_errors ?? 0}`,
      hint: "按 reason 排查"
    },
    {
      key: "policy",
      icon: <Route size={18} />,
      label: "策略漂移",
      value: `${totals?.policy_drift_nodes ?? 0}`,
      hint: "目标策略未落地"
    }
  ];
}

export function TrafficOverviewPanel({ onRequestError }: { onRequestError: (error: unknown) => void }) {
  const [traffic, setTraffic] = useState<TrafficOverview | null>(null);
  const [windowHours, setWindowHours] = useState(24);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const cards = useMemo(() => metricCards(traffic), [traffic]);
  const nodes = useMemo(() => normalizeArray(traffic?.nodes), [traffic?.nodes]);
  const users = useMemo(() => normalizeArray(traffic?.users), [traffic?.users]);
  const flowEvents = useMemo(() => normalizeArray(traffic?.flow_events), [traffic?.flow_events]);
  const policyEvents = useMemo(() => normalizeArray(traffic?.policy_events), [traffic?.policy_events]);
  const policyConsistency = useMemo(() => normalizeArray(traffic?.policy_consistency), [traffic?.policy_consistency]);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const response = await getTrafficOverview(windowHours, 30);
      setTraffic(response.traffic || response.overview);
    } catch (err) {
      setError(err instanceof Error ? err.message : "读取流量统计失败");
      onRequestError(err);
    } finally {
      setLoading(false);
    }
  }, [onRequestError, windowHours]);

  useEffect(() => {
    void load();
  }, [load]);

  const nodeColumns: TableColumnsType<TrafficNodeStats> = [
    {
      title: "节点",
      dataIndex: "name",
      width: 190,
      render: (_value, record) => (
        <div className="traffic-identity">
          <strong>{record.name || record.node_id}</strong>
          <Text type="secondary">{record.node_id}</Text>
        </div>
      )
    },
    {
      title: "入口",
      dataIndex: "endpoint",
      width: 170,
      render: (value) => <Text className="mono">{value || "-"}</Text>
    },
    {
      title: "流量",
      dataIndex: ["traffic", "total_bytes"],
      width: 130,
      render: (_value, record) => <strong>{formatBytes(record.traffic?.total_bytes)}</strong>
    },
    {
      title: "TCP / UDP",
      width: 170,
      render: (_value, record) => (
        <Text className="mono subtle">
          {formatBytes((record.traffic?.tcp_client_to_target_bytes || 0) + (record.traffic?.tcp_target_to_client_bytes || 0))}
          {" / "}
          {formatBytes((record.traffic?.udp_client_to_target_bytes || 0) + (record.traffic?.udp_target_to_client_bytes || 0))}
        </Text>
      )
    },
    {
      title: "连接",
      width: 140,
      render: (_value, record) => (
        <Text className="mono subtle">
          {record.active_quic_connections} / {record.active_tcp_flows} / {record.active_udp_flows}
        </Text>
      )
    },
    {
      title: "策略",
      width: 110,
      render: (_value, record) => <Tag color={policyStateColor(record.policy_state)}>{policyStateText(record.policy_state)}</Tag>
    },
    {
      title: "上报",
      width: 150,
      render: (_value, record) => (
        <div className="traffic-identity compact">
          <Text className="mono">{formatAge(record.report_age_seconds)}</Text>
          <Text type="secondary">{formatDate(record.latest_report_at)}</Text>
        </div>
      )
    }
  ];

  const userColumns: TableColumnsType<TrafficUserStats> = [
    {
      title: "用户",
      dataIndex: "user_id",
      render: (value) => <Text className="mono">{value || "-"}</Text>
    },
    {
      title: "流量",
      dataIndex: ["traffic", "total_bytes"],
      width: 120,
      render: (_value, record) => <strong>{formatBytes(record.traffic?.total_bytes)}</strong>
    },
    {
      title: "连接",
      dataIndex: "active_connections",
      width: 90,
      render: (value) => <Text className="mono">{value ?? 0}</Text>
    }
  ];

  const flowColumns: TableColumnsType<TrafficFlowEventStats> = [
    {
      title: "事件",
      width: 130,
      render: (_value, record) => (
        <Space size={4}>
          <Tag color={record.network === "udp" ? "green" : "blue"}>{record.network || "-"}</Tag>
          <Tag>{eventText(record.event)}</Tag>
        </Space>
      )
    },
    {
      title: "原因",
      dataIndex: "reason",
      render: (value) => eventText(value)
    },
    {
      title: "游戏/策略",
      width: 160,
      render: (_value, record) => (
        <Text className="mono subtle">
          {record.game_id || "-"} / {record.policy_id || "-"}
        </Text>
      )
    },
    {
      title: "次数",
      dataIndex: "count",
      width: 90,
      render: (value) => <strong>{value ?? 0}</strong>
    }
  ];

  const policyColumns: TableColumnsType<TrafficPolicyEventStats> = [
    {
      title: "游戏/策略",
      render: (_value, record) => (
        <Text className="mono">
          {record.game_id || "-"} / {record.policy_id || "-"}
        </Text>
      )
    },
    {
      title: "协议",
      dataIndex: "network",
      width: 90,
      render: (value) => <Tag color={value === "udp" ? "green" : "blue"}>{value || "-"}</Tag>
    },
    {
      title: "打开/关闭/错误",
      width: 150,
      render: (_value, record) => (
        <Text className="mono subtle">
          {record.open} / {record.close} / {record.error}
        </Text>
      )
    },
    {
      title: "总计",
      dataIndex: "total",
      width: 90,
      render: (value) => <strong>{value ?? 0}</strong>
    }
  ];

  const consistencyColumns: TableColumnsType<TrafficPolicyConsistency> = [
    {
      title: "节点",
      render: (_value, record) => (
        <div className="traffic-identity compact">
          <strong>{record.name || record.node_id}</strong>
          <Text type="secondary">{record.node_id}</Text>
        </div>
      )
    },
    {
      title: "当前 / 目标",
      render: (_value, record) => (
        <Text className="mono subtle">
          {record.current_policy_revision || "-"} / {record.desired_policy_revision || "-"}
        </Text>
      )
    },
    {
      title: "状态",
      dataIndex: "state",
      width: 110,
      render: (value) => <Tag color={policyStateColor(value)}>{policyStateText(value)}</Tag>
    },
    {
      title: "最后上报",
      width: 160,
      render: (_value, record) => <Text className="mono subtle">{formatAge(record.report_age_seconds)}</Text>
    }
  ];

  return (
    <main className="workbench traffic-panel">
      <div className="traffic-header">
        <div>
          <Text className="eyebrow">流量与联调观测</Text>
          <Title level={3}>节点实时统计</Title>
          <Text type="secondary">按节点上报汇总 TCP、UDP、用户、策略和 flow 事件，用于排查客户端慢、断连和策略未生效。</Text>
        </div>
        <Space wrap>
          <Select value={windowHours} options={windowOptions} onChange={setWindowHours} />
          <Button icon={<RefreshCw size={16} />} loading={loading} onClick={() => void load()}>
            刷新
          </Button>
        </Space>
      </div>

      {error ? <Alert className="inline-alert" type="error" showIcon message={error} /> : null}

      <section className="traffic-metrics">
        {cards.map((card) => (
          <div className={`traffic-metric ${card.key}`} key={card.key}>
            <div className="traffic-metric-icon">{card.icon}</div>
            <div>
              <Text>{card.label}</Text>
              <strong>{card.value}</strong>
              <span>{card.hint}</span>
            </div>
          </div>
        ))}
      </section>

      {traffic?.sample_mode === "latest_cumulative" ? (
        <Alert
          className="inline-alert"
          type="info"
          showIcon
          message="当前窗口内样本不足两条，流量暂按节点本次启动后的累计值展示。"
        />
      ) : null}

      {traffic?.recommendations?.length ? (
        <section className="traffic-recommendations">
          <div className="traffic-section-title">
            <Gauge size={17} />
            <strong>排障建议</strong>
          </div>
          {traffic.recommendations.map((item) => (
            <Alert key={item} type="warning" showIcon message={item} />
          ))}
        </section>
      ) : null}

      <section className="traffic-section">
        <div className="traffic-section-title">
          <Network size={17} />
          <strong>节点流量排行</strong>
        </div>
        <Table<TrafficNodeStats>
          rowKey="node_id"
          loading={loading}
          pagination={{ pageSize: 8 }}
          columns={nodeColumns}
          dataSource={nodes}
          scroll={{ x: 1080 }}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无节点流量样本" /> }}
        />
      </section>

      <section className="traffic-split">
        <div className="traffic-section">
          <div className="traffic-section-title">
            <UsersRound size={17} />
            <strong>用户流量排行</strong>
          </div>
          <Table<TrafficUserStats>
            rowKey="user_id"
            size="small"
            loading={loading}
            pagination={false}
            columns={userColumns}
            dataSource={users}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无用户流量" /> }}
          />
        </div>
        <div className="traffic-section">
          <div className="traffic-section-title">
            <AlertTriangle size={17} />
            <strong>Flow 事件排行</strong>
          </div>
          <Table<TrafficFlowEventStats>
            rowKey={(record) =>
              `${record.network}:${record.event}:${record.reason}:${record.game_id || "-"}:${record.policy_id || "-"}`
            }
            size="small"
            loading={loading}
            pagination={false}
            columns={flowColumns}
            dataSource={flowEvents}
            scroll={{ x: 520 }}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无 flow 事件" /> }}
          />
        </div>
      </section>

      <section className="traffic-split">
        <div className="traffic-section">
          <div className="traffic-section-title">
            <Route size={17} />
            <strong>游戏/策略事件</strong>
          </div>
          <Table<TrafficPolicyEventStats>
            rowKey={(record) => `${record.network}:${record.game_id || "-"}:${record.policy_id || "-"}`}
            size="small"
            loading={loading}
            pagination={false}
            columns={policyColumns}
            dataSource={policyEvents}
            scroll={{ x: 520 }}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无策略事件" /> }}
          />
        </div>
        <div className="traffic-section">
          <div className="traffic-section-title">
            <Route size={17} />
            <strong>策略一致性</strong>
          </div>
          <Table<TrafficPolicyConsistency>
            rowKey="node_id"
            size="small"
            loading={loading}
            pagination={false}
            columns={consistencyColumns}
            dataSource={policyConsistency}
            scroll={{ x: 520 }}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无策略状态" /> }}
          />
        </div>
      </section>
    </main>
  );
}
