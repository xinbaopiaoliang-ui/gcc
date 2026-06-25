import { Badge, Tag } from "antd";
import type { NodeStatus } from "../types";

const statusMap: Record<NodeStatus, { color: string; text: string; badge: "default" | "processing" | "success" | "warning" | "error" }> = {
  new: { color: "blue", text: "新建", badge: "default" },
  deploying: { color: "gold", text: "部署中", badge: "processing" },
  online: { color: "green", text: "在线", badge: "success" },
  offline: { color: "default", text: "离线", badge: "default" },
  error: { color: "red", text: "异常", badge: "error" },
  disabled: { color: "default", text: "停用", badge: "warning" }
};

export function statusLabel(status: NodeStatus | string): string {
  return statusMap[status as NodeStatus]?.text ?? status;
}

export function StatusBadge({ status }: { status: NodeStatus | string }) {
  const item = statusMap[status as NodeStatus] ?? statusMap.offline;
  return (
    <Tag className="status-tag" color={item.color}>
      <Badge status={item.badge} />
      {item.text}
    </Tag>
  );
}
