import { Form, Input, Modal, Typography } from "antd";
import type { ApplyPolicyInput, PanelNode } from "../types";

const { Text } = Typography;

const defaultPolicyYAML = `route_policies:
  revision: ""
  policies: []
`;

export function ApplyPolicyModal({
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
  onSubmit: (input: ApplyPolicyInput) => Promise<void>;
}) {
  const [form] = Form.useForm<ApplyPolicyInput>();

  const submit = async (values: ApplyPolicyInput) => {
    await onSubmit({
      revision: values.revision?.trim(),
      sha256: values.sha256?.trim(),
      route_policies_yaml: values.route_policies_yaml
    });
    form.resetFields();
  };

  return (
    <Modal
      title={node ? `下发策略：${node.node_id}` : "下发策略"}
      open={open}
      confirmLoading={loading}
      okText="创建任务"
      cancelText="取消"
      width={760}
      onCancel={onCancel}
      onOk={() => form.submit()}
      destroyOnClose
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={submit}
        initialValues={{
          revision: node?.desired_policy_revision || "",
          sha256: "",
          route_policies_yaml: defaultPolicyYAML
        }}
      >
        <div className="form-grid two">
          <Form.Item label="策略版本" name="revision">
            <Input placeholder="例如 20260617.1" />
          </Form.Item>
          <Form.Item label="SHA256" name="sha256">
            <Input placeholder="可留空，由面板自动计算" />
          </Form.Item>
        </div>
        <Form.Item
          label="路由策略 YAML"
          name="route_policies_yaml"
          rules={[{ required: true, message: "请填写策略 YAML" }]}
        >
          <Input.TextArea className="policy-yaml-input" rows={14} spellCheck={false} />
        </Form.Item>
        <Text type="secondary">
          创建后任务会进入 pending，节点下一次调用 /api/nodes/commands 拉取时会变为 running，并在后续上报里回写执行结果。
        </Text>
      </Form>
    </Modal>
  );
}
