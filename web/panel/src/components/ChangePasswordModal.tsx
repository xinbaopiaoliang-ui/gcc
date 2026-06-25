import { Alert, Form, Input, Modal, Typography } from "antd";
import { KeyRound } from "lucide-react";
import type { ChangePasswordInput } from "../types";

const { Text } = Typography;

interface ChangePasswordModalProps {
  open: boolean;
  loading: boolean;
  error: string;
  onCancel: () => void;
  onSubmit: (input: ChangePasswordInput) => Promise<void> | void;
}

export function ChangePasswordModal({ open, loading, error, onCancel, onSubmit }: ChangePasswordModalProps) {
  const [form] = Form.useForm<ChangePasswordInput>();

  return (
    <Modal
      title={
        <span className="modal-title-icon">
          <KeyRound size={18} />
          修改登录密码
        </span>
      }
      open={open}
      okText="保存"
      cancelText="取消"
      confirmLoading={loading}
      onOk={() => form.submit()}
      onCancel={() => {
        form.resetFields();
        onCancel();
      }}
      destroyOnClose
    >
      <Form<ChangePasswordInput>
        form={form}
        layout="vertical"
        requiredMark={false}
        onFinish={(values) => {
          void onSubmit(values);
        }}
      >
        {error && <Alert className="inline-alert" type="error" showIcon message={error} />}
        <Form.Item name="current_password" label="当前密码" rules={[{ required: true, message: "请输入当前密码" }]}>
          <Input.Password autoComplete="current-password" placeholder="输入当前登录密码" />
        </Form.Item>
        <Form.Item
          name="new_password"
          label="新密码"
          rules={[
            { required: true, message: "请输入新密码" },
            { min: 10, message: "新密码至少 10 位" },
            { max: 128, message: "新密码最多 128 位" }
          ]}
        >
          <Input.Password autoComplete="new-password" placeholder="输入新密码" />
        </Form.Item>
        <Text className="modal-note" type="secondary">
          保存后当前登录态会刷新，后续请使用新密码登录。
        </Text>
      </Form>
    </Modal>
  );
}
