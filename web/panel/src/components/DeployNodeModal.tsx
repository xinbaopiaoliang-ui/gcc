import { Alert, Form, Input, Modal, Space, Tag, Typography } from "antd";
import { getPanelAPIBaseURL } from "../api";
import type { DeployNodeInput, PanelNode } from "../types";

const { Text } = Typography;

export function DeployNodeModal({
  open,
  loading,
  node,
  onCancel,
  onSubmit
}: {
  open: boolean;
  loading: boolean;
  node?: PanelNode;
  onCancel: () => void;
  onSubmit: (input: DeployNodeInput) => Promise<void>;
}) {
  const [form] = Form.useForm<DeployNodeInput>();
  const defaultPanelURL = getPanelAPIBaseURL();

  const submit = async (values: DeployNodeInput) => {
    await onSubmit({
      version: values.version?.trim(),
      panel_base_url: values.panel_base_url?.trim()
    });
    form.resetFields();
  };

  const secretReady = Boolean(node?.hmac_secret_configured);

  return (
    <Modal
      title={node ? `一键部署：${node.node_id}` : "一键部署"}
      open={open}
      width={680}
      confirmLoading={loading}
      okText="创建部署任务"
      cancelText="取消"
      onCancel={onCancel}
      onOk={() => form.submit()}
      destroyOnClose
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={submit}
        initialValues={{
          version: node?.desired_version || "latest",
          panel_base_url: defaultPanelURL
        }}
      >
        <div className="form-grid two">
          <Form.Item label="节点版本" name="version">
            <Input placeholder="latest 或 v0.6.6" />
          </Form.Item>
          <Form.Item label="节点密钥状态">
            <Space wrap>
              <Tag color={secretReady ? "success" : "warning"}>{secretReady ? "已同步" : "未同步"}</Tag>
              {node?.hmac_secret_source ? <Text type="secondary">来源：{node.hmac_secret_source}</Text> : null}
              {node?.hmac_secret_updated_at ? (
                <Text type="secondary">更新时间：{new Date(node.hmac_secret_updated_at).toLocaleString()}</Text>
              ) : null}
            </Space>
          </Form.Item>
        </div>
        {!secretReady ? (
          <Alert
            className="inline-alert compact"
            type="warning"
            showIcon
            message="该节点还没有业务后台同步的 HMAC Secret"
            description="请先让业务后台为节点生成并保存 hmac_secret，再通过节点同步接口写入控制面板。部署时面板会自动把已保存密钥写入节点配置。"
          />
        ) : null}
        <Form.Item label="面板公网地址" name="panel_base_url">
          <Input placeholder="http://103.201.131.99:8091" />
        </Form.Item>
        <Text type="secondary">
          部署任务会通过 SSH 安装节点、写入配置和证书、启动 systemd，并检查节点本机 health/status。
          HMAC Secret 由业务后台生成并保存，面板只保存加密副本，任务日志不会显示密钥。
        </Text>
      </Form>
    </Modal>
  );
}
