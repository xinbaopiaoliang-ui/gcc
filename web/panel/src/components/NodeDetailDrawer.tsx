import { Alert, Button, Descriptions, Drawer, Empty, Space, Table, Tabs, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { FileText, KeyRound, RefreshCw, Rocket, Send, ServerCog, Terminal, UploadCloud } from "lucide-react";
import { StatusBadge } from "./StatusBadge";
import type { NodeCredential, NodeReport, NodeSyncStatus, NodeTask, NodeTaskStatus, PanelNode } from "../types";

const { Text } = Typography;

function formatDate(value?: string) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function taskStatusColor(status: NodeTaskStatus) {
  switch (status) {
    case "pending":
      return "default";
    case "running":
      return "processing";
    case "success":
      return "success";
    case "failed":
      return "error";
    case "cancelled":
      return "warning";
    default:
      return "default";
  }
}

function hasDrift(currentValue: string, desiredValue: string) {
  return desiredValue.trim() !== "" && currentValue.trim() !== desiredValue.trim();
}

function syncStateText(state?: string) {
  switch (state) {
    case "synced":
      return "已同步";
    case "pending":
      return "待同步";
    case "waiting_report":
      return "等待上报";
    case "not_set":
      return "未设置目标";
    case "unknown":
      return "未知";
    default:
      return state || "-";
  }
}

function syncStateColor(state?: string) {
  switch (state) {
    case "synced":
      return "success";
    case "pending":
    case "waiting_report":
      return "warning";
    case "unknown":
      return "default";
    default:
      return "default";
  }
}

function isRetryableTask(status: NodeTaskStatus) {
  return status === "success" || status === "failed" || status === "cancelled";
}

export function NodeDetailDrawer({
  node,
  open,
  reports,
  tasks,
  syncStatus,
  credential,
  canManage = true,
  loading,
  testingCredential,
  onClose,
  onRefresh,
  onOpenCredential,
  onTestCredential,
  onDeploy,
  onUpdate,
  onViewTaskLogs,
  onRetryTask,
  onApplyPolicy
}: {
  node?: PanelNode;
  open: boolean;
  reports: NodeReport[];
  tasks: NodeTask[];
  syncStatus?: NodeSyncStatus | null;
  credential?: NodeCredential | null;
  canManage?: boolean;
  loading: boolean;
  testingCredential: boolean;
  onClose: () => void;
  onRefresh: () => void;
  onOpenCredential: (node: PanelNode) => void;
  onTestCredential: (node: PanelNode) => void;
  onDeploy: (node: PanelNode) => void;
  onUpdate: (node: PanelNode) => void;
  onViewTaskLogs: (task: NodeTask) => void;
  onRetryTask: (task: NodeTask) => void;
  onApplyPolicy: (node: PanelNode) => void;
}) {
  const latestReport = reports[0];
  const versionDrift = node ? hasDrift(node.current_version || "", node.desired_version || "") : false;
  const policyDrift = node ? hasDrift(node.current_policy_revision || "", node.desired_policy_revision || "") : false;

  const reportColumns: ColumnsType<NodeReport> = [
    {
      title: "上报时间",
      dataIndex: "reported_at",
      width: 170,
      render: (value: string) => <Text className="mono subtle">{formatDate(value)}</Text>
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 90,
      render: (value: string) => <Tag color={value === "ok" ? "success" : "warning"}>{value || "-"}</Tag>
    },
    {
      title: "版本",
      dataIndex: "version",
      width: 120,
      render: (value: string) => <Text className="mono">{value || "-"}</Text>
    },
    {
      title: "策略",
      dataIndex: "route_policy_revision",
      width: 170,
      render: (_, report) => (
        <Text className="mono subtle">
          {report.route_policy_revision || "-"} / {report.route_policy_count}
        </Text>
      )
    },
    {
      title: "连接/Flow",
      width: 150,
      render: (_, report) => (
        <Text className="mono subtle">
          {report.active_quic_connections} / {report.active_tcp_flows} / {report.active_udp_flows}
        </Text>
      )
    }
  ];

  const taskColumns: ColumnsType<NodeTask> = [
    {
      title: "任务",
      dataIndex: "task_id",
      width: 230,
      render: (value: string, task) => (
        <div className="node-cell">
          <Text className="mono">{value}</Text>
          <Text type="secondary" className="node-name">
            {task.type}
          </Text>
        </div>
      )
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (value: NodeTaskStatus) => <Tag color={taskStatusColor(value)}>{value}</Tag>
    },
    {
      title: "排队时间",
      dataIndex: "queued_at",
      width: 170,
      render: (value: string) => <Text className="mono subtle">{formatDate(value)}</Text>
    },
    {
      title: "完成时间",
      dataIndex: "finished_at",
      width: 170,
      render: (value?: string) => <Text className="mono subtle">{formatDate(value)}</Text>
    },
    {
      title: "错误",
      dataIndex: "error_message",
      ellipsis: true,
      render: (value: string) => value || "-"
    },
    {
      title: "操作",
      fixed: "right",
      width: 124,
      render: (_, task) => (
        <Space size={2}>
          <Button type="text" size="small" icon={<FileText size={14} />} onClick={() => onViewTaskLogs(task)} />
          {canManage && isRetryableTask(task.status) ? (
            <Button type="text" size="small" icon={<RefreshCw size={14} />} onClick={() => onRetryTask(task)} />
          ) : null}
        </Space>
      )
    }
  ];

  return (
    <Drawer
      width={860}
      open={open}
      onClose={onClose}
      extra={
        node ? (
          <Space>
            <Button size="small" icon={<RefreshCw size={14} />} onClick={onRefresh} loading={loading}>
              刷新
            </Button>
            {canManage && (
              <>
                <Button size="small" icon={<KeyRound size={14} />} onClick={() => onOpenCredential(node)}>
                  凭据
                </Button>
                <Button
                  size="small"
                  icon={<Terminal size={14} />}
                  onClick={() => onTestCredential(node)}
                  loading={testingCredential}
                  disabled={!credential}
                >
                  测试 SSH
                </Button>
                <Button size="small" icon={<Rocket size={14} />} onClick={() => onDeploy(node)} disabled={!credential}>
                  部署
                </Button>
                <Button size="small" icon={<UploadCloud size={14} />} onClick={() => onUpdate(node)} disabled={!credential}>
                  更新
                </Button>
                <Button size="small" type="primary" icon={<Send size={14} />} onClick={() => onApplyPolicy(node)}>
                  下发策略
                </Button>
              </>
            )}
          </Space>
        ) : null
      }
      title={
        <Space>
          <ServerCog size={18} />
          <span>节点详情</span>
        </Space>
      }
    >
      {!node ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无节点" />
      ) : (
        <div className="detail-stack">
          <div>
            <Text className="detail-kicker">NODE ID</Text>
            <div className="detail-title">{node.node_id}</div>
            <div className="detail-subtitle">{node.name}</div>
          </div>

          <div className="detail-health-grid">
            <div className="detail-health-card">
              <Text>状态</Text>
              <div>
                <StatusBadge status={node.status} />
              </div>
              <span>{node.last_error || "节点最近未上报错误"}</span>
            </div>
            <div className="detail-health-card">
              <Text>版本</Text>
              <strong>{node.current_version || "-"}</strong>
              <span>
                目标 {node.desired_version || "-"} {versionDrift ? "，待更新" : ""}
              </span>
            </div>
            <div className="detail-health-card">
              <Text>策略</Text>
              <strong>{node.current_policy_revision || "-"}</strong>
              <span>
                目标 {node.desired_policy_revision || "-"} {policyDrift ? "，待同步" : ""}
              </span>
            </div>
            <div className="detail-health-card">
              <Text>实时连接</Text>
              <strong>
                {latestReport
                  ? `${latestReport.active_quic_connections}/${latestReport.active_tcp_flows}/${latestReport.active_udp_flows}`
                  : "-"}
              </strong>
              <span>QUIC / TCP / UDP</span>
            </div>
          </div>

          {syncStatus ? (
            <section className="sync-status-panel">
              <div className="sync-status-head">
                <div>
                  <Text className="detail-kicker">同步状态</Text>
                  <strong>{syncStatus.node_id}</strong>
                </div>
                <Space wrap>
                  <Tag color={syncStateColor(syncStatus.version_state)}>版本 {syncStateText(syncStatus.version_state)}</Tag>
                  <Tag color={syncStateColor(syncStatus.policy_state)}>策略 {syncStateText(syncStatus.policy_state)}</Tag>
                </Space>
              </div>
              <div className="sync-status-grid">
                <div>
                  <span>当前/目标版本</span>
                  <strong>
                    {syncStatus.current_version || "-"} / {syncStatus.desired_version || "-"}
                  </strong>
                </div>
                <div>
                  <span>当前/目标策略</span>
                  <strong>
                    {syncStatus.current_policy_revision || "-"} / {syncStatus.desired_policy_revision || "-"}
                  </strong>
                </div>
                <div>
                  <span>任务队列</span>
                  <strong>
                    {syncStatus.pending_tasks} 待执行 / {syncStatus.running_tasks} 执行中 / {syncStatus.failed_tasks} 失败
                  </strong>
                </div>
                <div>
                  <span>上报年龄</span>
                  <strong>{syncStatus.report_age_seconds === undefined ? "-" : `${syncStatus.report_age_seconds}s`}</strong>
                </div>
              </div>
              {syncStatus.recommendations?.length ? (
                <Alert
                  className="inline-alert compact"
                  type={syncStatus.failed_tasks > 0 || syncStatus.last_error ? "warning" : "info"}
                  showIcon
                  message={syncStatus.recommendations.join("；")}
                />
              ) : null}
            </section>
          ) : null}

          <Tabs
            className="detail-tabs"
            items={[
              {
                key: "base",
                label: "基础信息",
                children: (
                  <Space direction="vertical" size={18} className="detail-stack">
                    <Descriptions column={1} size="small" bordered>
                      <Descriptions.Item label="状态">
                        <StatusBadge status={node.status} />
                      </Descriptions.Item>
                      <Descriptions.Item label="客户端入口">
                        {node.endpoint_host}:{node.endpoint_port}
                      </Descriptions.Item>
                      <Descriptions.Item label="ALPN">{node.alpn}</Descriptions.Item>
                      <Descriptions.Item label="管理地址">
                        {node.admin_host}:{node.admin_port}
                      </Descriptions.Item>
                      <Descriptions.Item label="SSH">
                        {node.ssh_user}@{node.ssh_host}:{node.ssh_port}
                      </Descriptions.Item>
                      <Descriptions.Item label="SSH 凭据">
                        {credential ? (
                          <Space>
                            <Tag color="success">已保存</Tag>
                            <Text className="mono subtle">
                              {credential.auth_type} / {credential.username}
                            </Text>
                          </Space>
                        ) : (
                          <Tag>未保存</Tag>
                        )}
                      </Descriptions.Item>
                      <Descriptions.Item label="区域线路">
                        {[node.region, node.country, node.provider, node.line_type].filter(Boolean).join(" / ") || "-"}
                      </Descriptions.Item>
                      <Descriptions.Item label="协议权限">
                        <Space>
                          <Tag color={node.allow_tcp ? "blue" : "default"}>TCP {node.allow_tcp ? "开启" : "关闭"}</Tag>
                          <Tag color={node.allow_udp ? "green" : "default"}>UDP {node.allow_udp ? "开启" : "关闭"}</Tag>
                        </Space>
                      </Descriptions.Item>
                      <Descriptions.Item label="版本">
                        当前 {node.current_version || "-"} / 目标 {node.desired_version || "-"}
                      </Descriptions.Item>
                      <Descriptions.Item label="策略版本">
                        当前 {node.current_policy_revision || "-"} / 目标 {node.desired_policy_revision || "-"}
                      </Descriptions.Item>
                      <Descriptions.Item label="最近上报">{formatDate(node.last_report_at)}</Descriptions.Item>
                      <Descriptions.Item label="最后错误">{node.last_error || "-"}</Descriptions.Item>
                      <Descriptions.Item label="更新时间">{formatDate(node.updated_at)}</Descriptions.Item>
                    </Descriptions>

                    <div>
                      <Text className="detail-kicker">标签</Text>
                      <div className="tag-row">
                        {node.tags?.length ? (
                          node.tags.map((tag) => <Tag key={tag}>{tag}</Tag>)
                        ) : (
                          <Text type="secondary">-</Text>
                        )}
                      </div>
                    </div>

                    <div>
                      <Text className="detail-kicker">Labels</Text>
                      <div className="label-grid">
                        {Object.entries(node.labels ?? {}).length ? (
                          Object.entries(node.labels).map(([key, value]) => (
                            <div className="label-item" key={key}>
                              <span>{key}</span>
                              <strong>{value}</strong>
                            </div>
                          ))
                        ) : (
                          <Text type="secondary">-</Text>
                        )}
                      </div>
                    </div>
                  </Space>
                )
              },
              {
                key: "reports",
                label: "最近上报",
                children: (
                  <Table
                    size="small"
                    className="detail-table"
                    columns={reportColumns}
                    dataSource={reports}
                    rowKey="id"
                    loading={loading}
                    pagination={false}
                    scroll={{ x: 760 }}
                    locale={{ emptyText: "暂无上报" }}
                  />
                )
              },
              {
                key: "tasks",
                label: "最近任务",
                children: (
                  <Table
                    size="small"
                    className="detail-table"
                    columns={taskColumns}
                    dataSource={tasks}
                    rowKey="task_id"
                    loading={loading}
                    pagination={false}
                    scroll={{ x: 920 }}
                    locale={{ emptyText: "暂无任务" }}
                  />
                )
              }
            ]}
          />
        </div>
      )}
    </Drawer>
  );
}
