import { Button, Popconfirm, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Activity, Edit3, Eye, KeyRound, Send, Server, Trash2 } from "lucide-react";
import { NodeLoadMetrics } from "./NodeLoadMetrics";
import { StatusBadge } from "./StatusBadge";
import type { PanelNode } from "../types";

const { Text } = Typography;

function endpoint(node: PanelNode) {
  return `${node.endpoint_host}:${node.endpoint_port}`;
}

function hasDrift(currentValue: string, desiredValue: string) {
  return desiredValue.trim() !== "" && currentValue.trim() !== desiredValue.trim();
}

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

function reportAge(value?: string) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "-";
  }
  const seconds = Math.max(0, Math.round((Date.now() - date.getTime()) / 1000));
  if (seconds < 60) {
    return `${seconds}s`;
  }
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m`;
  }
  const hours = Math.round(minutes / 60);
  if (hours < 24) {
    return `${hours}h`;
  }
  return `${Math.round(hours / 24)}d`;
}

export function NodeTable({
  nodes,
  loading,
  canManage = true,
  onView,
  onDiagnose,
  onHMACSecret,
  onEdit,
  onApplyPolicy,
  onDelete
}: {
  nodes: PanelNode[];
  loading: boolean;
  canManage?: boolean;
  onView: (node: PanelNode) => void;
  onDiagnose: (node: PanelNode) => void;
  onHMACSecret: (node: PanelNode) => void;
  onEdit: (node: PanelNode) => void;
  onApplyPolicy: (node: PanelNode) => void;
  onDelete: (node: PanelNode) => void;
}) {
  const columns: ColumnsType<PanelNode> = [
    {
      title: "服务器名称",
      dataIndex: "name",
      width: 320,
      render: (_, node) => (
        <div className="server-name-cell">
          <span className="server-row-icon">
            <Server size={17} />
          </span>
          <div className="server-name-copy">
            <div className="server-name-line">
              <button className="link-button server-name-button" type="button" onClick={() => onView(node)}>
                {node.name || "未命名服务器"}
              </button>
              <span className={`server-online-dot ${node.status}`} />
              <Tag className="sid-tag">ID:{node.id}</Tag>
            </div>
            <div className="server-meta-line">
              <StatusBadge status={node.status} />
              <span>最后心跳：{reportAge(node.last_report_at)}</span>
              <span>区域：{node.region || "-"}</span>
            </div>
            {node.last_error ? <Text type="danger" className="node-name">{node.last_error}</Text> : null}
          </div>
        </div>
      )
    },
    {
      title: "节点ID",
      dataIndex: "node_id",
      width: 230,
      render: (_, node) => (
        <div className="server-id-cell">
          <button className="link-button mono" type="button" onClick={() => onView(node)}>
            {node.node_id}
          </button>
          <span>{[node.provider, node.line_type].filter(Boolean).join(" / ") || "未设置服务商/线路"}</span>
        </div>
      )
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 120,
      render: (_, node) => <StatusBadge status={node.status} />
    },
    {
      title: "负载",
      width: 210,
      render: (_, node) => <NodeLoadMetrics system={node.latest_system} />
    },
    {
      title: "入口 / 协议",
      dataIndex: "endpoint_host",
      width: 210,
      render: (_, node) => (
        <div className="server-stack">
          <Text className="mono">{endpoint(node)}</Text>
          <div className="muted-line">{node.alpn}</div>
          <Space size={4} wrap>
            <Tag color={node.allow_tcp ? "blue" : "default"}>TCP</Tag>
            <Tag color={node.allow_udp ? "green" : "default"}>UDP</Tag>
          </Space>
        </div>
      )
    },
    {
      title: "版本 / 策略",
      width: 230,
      render: (_, node) => {
        const versionDrift = hasDrift(node.current_version || "", node.desired_version || "");
        const policyDrift = hasDrift(node.current_policy_revision || "", node.desired_policy_revision || "");
        return (
          <div className="server-sync-stack">
            <div>
              <span>版本</span>
              <strong>{node.current_version || "-"}</strong>
              {versionDrift ? <Tag color="warning">待更新</Tag> : null}
            </div>
            <div>
              <span>策略</span>
              <strong>{node.current_policy_revision || "-"}</strong>
              {policyDrift ? <Tag color="processing">待同步</Tag> : null}
            </div>
          </div>
        );
      }
    },
    {
      title: "最后上报",
      width: 170,
      render: (_, node) => (
        <div className="server-stack">
          <Text className="mono subtle">{formatDate(node.last_report_at)}</Text>
          <div className="muted-line">Admin {node.admin_host}:{node.admin_port}</div>
        </div>
      )
    },
    {
      title: "标签",
      width: 160,
      render: (_, node) => (
        <div className="compact-tags">
          {node.tags?.length ? node.tags.slice(0, 3).map((tag) => <Tag key={tag}>{tag}</Tag>) : "-"}
        </div>
      )
    },
    {
      title: "操作",
      fixed: "right",
      width: canManage ? 230 : 104,
      render: (_, node) => (
        <Space size={6} className="server-row-actions">
          <Tooltip title="查看">
            <Button type="text" icon={<Eye size={16} />} onClick={() => onView(node)} />
          </Tooltip>
          <Tooltip title="接入自检">
            <Button type="text" icon={<Activity size={16} />} onClick={() => onDiagnose(node)} />
          </Tooltip>
          {canManage && (
            <>
              <Tooltip title="节点密钥">
                <Button type="text" icon={<KeyRound size={16} />} onClick={() => onHMACSecret(node)} />
              </Tooltip>
              <Tooltip title="编辑">
                <Button type="text" icon={<Edit3 size={16} />} onClick={() => onEdit(node)} />
              </Tooltip>
              <Tooltip title="下发策略">
                <Button type="text" icon={<Send size={16} />} onClick={() => onApplyPolicy(node)} />
              </Tooltip>
              <Popconfirm
                title="删除节点"
                description={node.node_id}
                okText="删除"
                cancelText="取消"
                onConfirm={() => onDelete(node)}
              >
                <Tooltip title="删除">
                  <Button danger type="text" icon={<Trash2 size={16} />} />
                </Tooltip>
              </Popconfirm>
            </>
          )}
        </Space>
      )
    }
  ];

  return (
    <Table
      className="node-table server-table"
      columns={columns}
      dataSource={nodes}
      loading={loading}
      rowKey="node_id"
      scroll={{ x: 1710 }}
      pagination={{
        pageSize: 10,
        showQuickJumper: true,
        showSizeChanger: false,
        showTotal: (total, range) => `已选择 0 项，共 ${total} 项　${range[0]}-${range[1]}`
      }}
    />
  );
}
