import { Button, Popconfirm, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Activity, Edit3, Eye, Send, Trash2 } from "lucide-react";
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

export function NodeTable({
  nodes,
  loading,
  canManage = true,
  onView,
  onDiagnose,
  onEdit,
  onApplyPolicy,
  onDelete
}: {
  nodes: PanelNode[];
  loading: boolean;
  canManage?: boolean;
  onView: (node: PanelNode) => void;
  onDiagnose: (node: PanelNode) => void;
  onEdit: (node: PanelNode) => void;
  onApplyPolicy: (node: PanelNode) => void;
  onDelete: (node: PanelNode) => void;
}) {
  const columns: ColumnsType<PanelNode> = [
    {
      title: "状态",
      dataIndex: "status",
      width: 104,
      render: (_, node) => <StatusBadge status={node.status} />
    },
    {
      title: "节点",
      dataIndex: "node_id",
      width: 220,
      render: (_, node) => (
        <div className="node-cell">
          <button className="link-button" type="button" onClick={() => onView(node)}>
            {node.node_id}
          </button>
          <Text type="secondary" className="node-name">
            {node.name}
          </Text>
          {node.last_error ? (
            <Text type="danger" className="node-name">
              {node.last_error}
            </Text>
          ) : null}
        </div>
      )
    },
    {
      title: "入口",
      dataIndex: "endpoint_host",
      width: 190,
      render: (_, node) => (
        <div>
          <Text className="mono">{endpoint(node)}</Text>
          <div className="muted-line">{node.alpn}</div>
        </div>
      )
    },
    {
      title: "区域/线路",
      width: 180,
      render: (_, node) => (
        <Space direction="vertical" size={2}>
          <Text>{[node.region, node.country].filter(Boolean).join(" / ") || "-"}</Text>
          <Text type="secondary">{[node.provider, node.line_type].filter(Boolean).join(" / ") || "-"}</Text>
        </Space>
      )
    },
    {
      title: "协议",
      width: 120,
      render: (_, node) => (
        <Space size={4}>
          <Tag color={node.allow_tcp ? "blue" : "default"}>TCP</Tag>
          <Tag color={node.allow_udp ? "green" : "default"}>UDP</Tag>
        </Space>
      )
    },
    {
      title: "版本",
      width: 190,
      render: (_, node) => {
        const drift = hasDrift(node.current_version || "", node.desired_version || "");
        return (
          <div className="node-version-stack">
            <Text className="mono">当前 {node.current_version || "-"}</Text>
            <Text className="mono subtle">目标 {node.desired_version || "-"}</Text>
            {drift ? <Tag color="warning">待更新</Tag> : null}
          </div>
        );
      }
    },
    {
      title: "策略",
      width: 190,
      render: (_, node) => {
        const drift = hasDrift(node.current_policy_revision || "", node.desired_policy_revision || "");
        return (
          <div className="node-version-stack">
            <Text className="mono">当前 {node.current_policy_revision || "-"}</Text>
            <Text className="mono subtle">目标 {node.desired_policy_revision || "-"}</Text>
            {drift ? <Tag color="processing">待同步</Tag> : null}
          </div>
        );
      }
    },
    {
      title: "上报",
      width: 170,
      render: (_, node) => (
        <div>
          <Text className="mono subtle">{formatDate(node.last_report_at)}</Text>
          <div className="muted-line">Admin {node.admin_host}:{node.admin_port}</div>
        </div>
      )
    },
    {
      title: "标签",
      width: 180,
      render: (_, node) => (
        <div className="compact-tags">
          {node.tags?.length ? node.tags.slice(0, 3).map((tag) => <Tag key={tag}>{tag}</Tag>) : "-"}
        </div>
      )
    },
    {
      title: "操作",
      fixed: "right",
      width: canManage ? 204 : 104,
      render: (_, node) => (
        <Space size={4}>
          <Tooltip title="查看">
            <Button type="text" icon={<Eye size={16} />} onClick={() => onView(node)} />
          </Tooltip>
          <Tooltip title="接入自检">
            <Button type="text" icon={<Activity size={16} />} onClick={() => onDiagnose(node)} />
          </Tooltip>
          {canManage && (
            <>
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
      className="node-table"
      columns={columns}
      dataSource={nodes}
      loading={loading}
      rowKey="node_id"
      scroll={{ x: 1580 }}
      pagination={{
        pageSize: 10,
        showSizeChanger: false,
        showTotal: (total) => `共 ${total} 个节点`
      }}
    />
  );
}
