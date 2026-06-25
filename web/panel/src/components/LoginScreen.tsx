import { Alert, Button, Form, Input, Typography } from "antd";
import { LockKeyhole, Server, ShieldCheck } from "lucide-react";
import type { LoginInput } from "../types";

const { Text, Title } = Typography;

interface LoginScreenProps {
  loading: boolean;
  error: string;
  onSubmit: (input: LoginInput) => Promise<void> | void;
}

export function LoginScreen({ loading, error, onSubmit }: LoginScreenProps) {
  return (
    <main className="login-shell">
      <section className="login-panel">
        <div className="login-copy">
          <div className="login-mark">
            <Server size={24} />
          </div>
          <Text className="eyebrow">gaccel panel</Text>
          <Title level={1}>节点控制面板</Title>
          <p>登录后才能管理节点、下发策略和执行部署任务。</p>
          <div className="login-assurance">
            <ShieldCheck size={16} />
            <span>面板登录签发短期 Bearer JWT，业务后台和节点接口继续使用独立 API Key。</span>
          </div>
        </div>

        <Form<LoginInput> className="login-form" layout="vertical" requiredMark={false} onFinish={onSubmit}>
          <div className="login-form-title">
            <LockKeyhole size={18} />
            <strong>管理员登录</strong>
          </div>
          {error && <Alert type="error" showIcon message={error} />}
          <Form.Item name="username" label="账号" rules={[{ required: true, message: "请输入账号" }]}>
            <Input autoComplete="username" placeholder="admin" size="large" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: "请输入密码" }]}>
            <Input.Password autoComplete="current-password" placeholder="输入面板密码" size="large" />
          </Form.Item>
          <Button block type="primary" htmlType="submit" size="large" loading={loading}>
            登录
          </Button>
        </Form>
      </section>
    </main>
  );
}
