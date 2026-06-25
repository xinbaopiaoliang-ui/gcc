import { Button, Form, Input, Modal, Popconfirm, Select, Space, Switch, Typography } from "antd";
import type { NodeCredential, NodeCredentialInput, PanelNode } from "../types";

const { Text } = Typography;

export function CredentialModal({
  open,
  loading,
  deleting,
  node,
  credential,
  onCancel,
  onSubmit,
  onDelete
}: {
  open: boolean;
  loading: boolean;
  deleting: boolean;
  node?: PanelNode;
  credential?: NodeCredential | null;
  onCancel: () => void;
  onSubmit: (input: NodeCredentialInput) => Promise<void>;
  onDelete: () => Promise<void>;
}) {
  const [form] = Form.useForm<NodeCredentialInput>();
  const authType = Form.useWatch("auth_type", form) ?? credential?.auth_type ?? "password";

  const submit = async (values: NodeCredentialInput) => {
    await onSubmit({
      auth_type: values.auth_type,
      username: values.username.trim(),
      password: values.password,
      private_key: values.private_key,
      private_key_passphrase: values.private_key_passphrase,
      sudo_mode: values.sudo_mode,
      is_one_time: values.is_one_time
    });
    form.resetFields();
  };

  return (
    <Modal
      title={node ? `SSH 凭据：${node.node_id}` : "SSH 凭据"}
      open={open}
      width={680}
      confirmLoading={loading}
      okText="保存凭据"
      cancelText="取消"
      onOk={() => form.submit()}
      onCancel={onCancel}
      destroyOnClose
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={submit}
        initialValues={{
          auth_type: credential?.auth_type ?? "password",
          username: credential?.username ?? node?.ssh_user ?? "root",
          sudo_mode: credential?.sudo_mode ?? "root",
          is_one_time: credential?.is_one_time ?? false
        }}
      >
        <div className="form-grid two">
          <Form.Item label="认证方式" name="auth_type" rules={[{ required: true }]}>
            <Select
              options={[
                { value: "password", label: "密码" },
                { value: "private_key", label: "SSH Key" }
              ]}
            />
          </Form.Item>
          <Form.Item label="SSH 用户" name="username" rules={[{ required: true, message: "请填写 SSH 用户" }]}>
            <Input placeholder="root" />
          </Form.Item>
        </div>

        {authType === "password" ? (
          <Form.Item label="SSH 密码" name="password" rules={[{ required: true, message: "请填写 SSH 密码" }]}>
            <Input.Password placeholder={credential?.has_password ? "重新填写后会替换旧密码" : "请输入 SSH 密码"} />
          </Form.Item>
        ) : (
          <>
            <Form.Item label="Private Key" name="private_key" rules={[{ required: true, message: "请填写 private key" }]}>
              <Input.TextArea className="policy-yaml-input" rows={9} spellCheck={false} />
            </Form.Item>
            <Form.Item label="Key Passphrase" name="private_key_passphrase">
              <Input.Password placeholder="可选" />
            </Form.Item>
          </>
        )}

        <div className="form-grid two">
          <Form.Item label="权限模式" name="sudo_mode" rules={[{ required: true }]}>
            <Select
              options={[
                { value: "root", label: "root" },
                { value: "sudo", label: "sudo" }
              ]}
            />
          </Form.Item>
          <Form.Item label="一次性凭据" name="is_one_time" valuePropName="checked">
            <Switch />
          </Form.Item>
        </div>

        <Space className="credential-actions" align="center">
          <Text type="secondary">保存只返回凭据状态，不会回显密码或私钥。</Text>
          <Popconfirm title="删除 SSH 凭据" okText="删除" cancelText="取消" onConfirm={onDelete}>
            <Button danger loading={deleting} disabled={!credential}>
              删除凭据
            </Button>
          </Popconfirm>
        </Space>
      </Form>
    </Modal>
  );
}
