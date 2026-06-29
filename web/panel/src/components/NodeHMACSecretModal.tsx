import { Alert, Button, Descriptions, Form, Input, Modal, Popconfirm, Space, Tag, Typography } from "antd";
import { AlertTriangle, CheckCircle2, KeyRound, RefreshCw } from "lucide-react";
import type { NodeHMACSecretInput, NodeHMACSecretStatus, PanelNode } from "../types";

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

function statusText(status?: string) {
  switch (status) {
    case "ok":
      return "正常";
    case "missing":
      return "未同步";
    case "unsupported_format":
      return "格式错误";
    case "invalid":
      return "长度不合法";
    case "secret_box_unavailable":
      return "主密钥不可用";
    case "decrypt_failed":
      return "解密失败";
    default:
      return "未知";
  }
}

function statusColor(status?: string) {
  switch (status) {
    case "ok":
      return "success";
    case "missing":
      return "warning";
    case "decrypt_failed":
    case "unsupported_format":
    case "invalid":
    case "secret_box_unavailable":
      return "error";
    default:
      return "default";
  }
}

function alertType(status?: string) {
  if (status === "ok") {
    return "success";
  }
  if (status === "missing") {
    return "warning";
  }
  return "error";
}

export function NodeHMACSecretModal({
  open,
  node,
  status,
  loading,
  syncing,
  clearing,
  onCancel,
  onRefresh,
  onSync,
  onClear
}: {
  open: boolean;
  node?: PanelNode;
  status?: NodeHMACSecretStatus | null;
  loading: boolean;
  syncing: boolean;
  clearing: boolean;
  onCancel: () => void;
  onRefresh: () => void;
  onSync: (input: NodeHMACSecretInput) => Promise<void>;
  onClear: () => Promise<void>;
}) {
  const [form] = Form.useForm<NodeHMACSecretInput>();

  const submit = async (values: NodeHMACSecretInput) => {
    await onSync({ hmac_secret: values.hmac_secret.trim() });
    form.resetFields();
  };

  return (
    <Modal
      title={
        <span className="modal-title-icon">
          <KeyRound size={18} />
          节点 HMAC Secret
        </span>
      }
      open={open}
      width={720}
      onCancel={onCancel}
      destroyOnClose
      footer={[
        <Button key="refresh" icon={<RefreshCw size={14} />} onClick={onRefresh} loading={loading}>
          刷新状态
        </Button>,
        <Popconfirm
          key="clear"
          title="清空节点 HMAC Secret 加密副本"
          description="清空后该节点不能部署，也不能用于业务后台签发客户端 token，直到重新同步密钥。"
          okText="确认清空"
          cancelText="取消"
          onConfirm={() => void onClear()}
        >
          <Button danger disabled={!status?.can_clear} loading={clearing}>
            清空副本
          </Button>
        </Popconfirm>,
        <Button key="cancel" onClick={onCancel}>
          关闭
        </Button>,
        <Button key="submit" type="primary" loading={syncing} onClick={() => form.submit()}>
          同步密钥
        </Button>
      ]}
    >
      <Space direction="vertical" size={14} className="hmac-secret-panel">
        <Alert
          type={alertType(status?.status)}
          showIcon
          icon={status?.status === "ok" ? <CheckCircle2 size={16} /> : <AlertTriangle size={16} />}
          message={status?.message || "正在读取节点 HMAC Secret 状态"}
          description="这里处理的是节点用于验证客户端 JWT 的 HMAC Secret。客户端不能拿到该密钥；业务后台保存明文，控制面板只保存加密副本。"
        />

        <Descriptions size="small" bordered column={2}>
          <Descriptions.Item label="节点">{node ? `${node.name || node.node_id} / ${node.node_id}` : "-"}</Descriptions.Item>
          <Descriptions.Item label="状态">
            <Tag color={statusColor(status?.status)}>{statusText(status?.status)}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="来源">{status?.source || "-"}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{formatDate(status?.updated_at)}</Descriptions.Item>
          <Descriptions.Item label="密钥指纹" span={2}>
            {status?.secret_fingerprint ? <Text className="mono">{status.secret_fingerprint}</Text> : "-"}
          </Descriptions.Item>
        </Descriptions>

        <Form form={form} layout="vertical" onFinish={submit}>
          <Form.Item
            label="重新同步 HMAC Secret"
            name="hmac_secret"
            rules={[
              { required: true, message: "请输入业务后台保存的节点 hmac_secret" },
              { min: 16, message: "hmac_secret 至少 16 个字符" }
            ]}
            extra="从业务后台复制该节点的 hmac_secret 明文到这里。保存后面板会立即加密入库，响应和审计日志不会回显明文。"
          >
            <Input.Password placeholder="输入业务后台保存的该节点 hmac_secret" autoComplete="off" />
          </Form.Item>
        </Form>
      </Space>
    </Modal>
  );
}

