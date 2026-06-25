import { Alert, Form, Input, Modal, Typography } from "antd";
import type { UpdateNodeInput, PanelNode } from "../types";

const { Text } = Typography;

export function UpdateNodeModal({
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
  onSubmit: (input: UpdateNodeInput) => Promise<void>;
}) {
  const [form] = Form.useForm<UpdateNodeInput>();

  const submit = async (values: UpdateNodeInput) => {
    await onSubmit({
      version: values.version?.trim()
    });
    form.resetFields();
  };

  return (
    <Modal
      title={node ? `一键更新：${node.node_id}` : "一键更新"}
      open={open}
      width={620}
      confirmLoading={loading}
      okText="创建更新任务"
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
          version: node?.desired_version || node?.current_version || "latest"
        }}
      >
        <Form.Item label="目标版本" name="version" extra="可填写 latest 或 v0.4.6 这类 Release 版本号。">
          <Input placeholder="latest 或 v0.4.6" />
        </Form.Item>
        <Alert
          type="info"
          showIcon
          message="更新任务会先备份当前 /usr/local/bin/gaccel-*，再安装目标版本并重启 gaccel-node。"
          description="如果 health、status 或版本校验失败，控制面板会尝试自动恢复备份并重启服务。"
        />
        <Text type="secondary" className="modal-note">
          更新只操作节点程序、systemd 服务和 gaccel-node 校验流程，不会修改服务器上的其它业务文件。
        </Text>
      </Form>
    </Modal>
  );
}
