import { Alert, Checkbox, Empty, Form, InputNumber, Modal, Select, Typography } from "antd";
import { useMemo } from "react";
import type { DesiredPolicyInput, PanelNode, PolicyRevision } from "../types";

const { Text } = Typography;

function sourceText(value: PolicyRevision["source"] | string) {
  if (value === "backend") {
    return "业务后台";
  }
  if (value === "manual") {
    return "手动录入";
  }
  return value || "-";
}

export function SyncPolicyModal({
  open,
  loading,
  node,
  policies,
  onCancel,
  onSubmit
}: {
  open: boolean;
  loading: boolean;
  node?: PanelNode;
  policies: PolicyRevision[];
  onCancel: () => void;
  onSubmit: (input: DesiredPolicyInput) => Promise<void>;
}) {
  const [form] = Form.useForm<DesiredPolicyInput>();
  const revision = Form.useWatch("revision", form);
  const selectedPolicy = useMemo(
    () => policies.find((policy) => policy.revision === revision),
    [policies, revision]
  );

  const submit = async (values: DesiredPolicyInput) => {
    await onSubmit({
      revision: values.revision,
      create_task: values.create_task ?? true,
      priority: values.priority ?? 100
    });
    form.resetFields();
  };

  return (
    <Modal
      title={node ? `同步策略：${node.node_id}` : "同步策略"}
      open={open}
      confirmLoading={loading}
      okText="同步"
      cancelText="取消"
      width={760}
      onCancel={onCancel}
      onOk={() => form.submit()}
      destroyOnClose
    >
      {policies.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="暂无策略版本，请先由业务后台接口或控制面板保存策略。"
        />
      ) : (
        <Form
          form={form}
          layout="vertical"
          onFinish={submit}
          initialValues={{
            revision: node?.desired_policy_revision || policies[0]?.revision,
            create_task: true,
            priority: 100
          }}
        >
          <Alert
            className="inline-alert"
            type="info"
            showIcon
            message="该操作会写入节点 desired_policy_revision，并按需创建 apply_policy 任务供节点拉取。"
          />
          <Form.Item
            label="策略版本"
            name="revision"
            rules={[{ required: true, message: "请选择策略版本" }]}
          >
            <Select
              showSearch
              options={policies.map((policy) => ({
                value: policy.revision,
                label: `${policy.revision} / ${sourceText(policy.source)}`
              }))}
            />
          </Form.Item>
          <div className="form-grid two">
            <Form.Item name="create_task" valuePropName="checked">
              <Checkbox>创建 apply_policy 任务</Checkbox>
            </Form.Item>
            <Form.Item label="任务优先级" name="priority">
              <InputNumber min={1} max={1000} className="full-input" />
            </Form.Item>
          </div>
          {selectedPolicy ? (
            <div className="policy-preview">
              <Text className="detail-kicker">SHA256</Text>
              <Text className="mono subtle">{selectedPolicy.sha256}</Text>
              <pre>{selectedPolicy.route_policies_yaml}</pre>
            </div>
          ) : null}
        </Form>
      )}
    </Modal>
  );
}
