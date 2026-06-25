import { Button, Drawer, Empty, Space, Tag, Timeline, Typography } from "antd";
import { RefreshCw } from "lucide-react";
import type { NodeTask, NodeTaskLog } from "../types";

const { Paragraph, Text } = Typography;

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

function streamColor(stream: NodeTaskLog["stream"]) {
  switch (stream) {
    case "error":
      return "red";
    case "stderr":
      return "orange";
    case "stdout":
      return "blue";
    default:
      return "green";
  }
}

export function TaskLogDrawer({
  open,
  task,
  logs,
  loading,
  canManage = false,
  onRetryTask,
  onClose
}: {
  open: boolean;
  task?: NodeTask;
  logs: NodeTaskLog[];
  loading: boolean;
  canManage?: boolean;
  onRetryTask?: (task: NodeTask) => void;
  onClose: () => void;
}) {
  const retryable = task && (task.status === "success" || task.status === "failed" || task.status === "cancelled");
  return (
    <Drawer
      width={720}
      open={open}
      onClose={onClose}
      title={task ? `任务日志：${task.task_id}` : "任务日志"}
      extra={
        task && canManage && retryable ? (
          <Button size="small" icon={<RefreshCw size={14} />} onClick={() => onRetryTask?.(task)}>
            重试
          </Button>
        ) : null
      }
    >
      {!task ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未选择任务" />
      ) : logs.length === 0 && !loading ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无日志" />
      ) : (
        <Timeline
          className="task-log-timeline"
          items={logs.map((log) => ({
            color: streamColor(log.stream),
            children: (
              <div className="task-log-entry">
                <Space size={8} wrap>
                  <Tag color={streamColor(log.stream)}>{log.stream}</Tag>
                  <Text strong>{log.step}</Text>
                  <Text type="secondary" className="mono">
                    {formatDate(log.created_at)}
                  </Text>
                </Space>
                <Paragraph className="task-log-message">{log.message}</Paragraph>
              </div>
            )
          }))}
        />
      )}
    </Drawer>
  );
}
