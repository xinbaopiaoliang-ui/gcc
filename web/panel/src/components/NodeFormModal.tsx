import type { ReactNode } from "react";
import { Form, Input, InputNumber, Modal, Select, Switch } from "antd";
import { Network, Server, Settings2, ShieldCheck, Tags } from "lucide-react";
import type { NodeInput, NodeStatus, PanelNode } from "../types";

type NodeFormValues = Omit<NodeInput, "labels" | "endpoint_port" | "admin_port" | "ssh_port"> & {
  endpoint_port?: number | null;
  admin_port?: number | null;
  ssh_port?: number | null;
  labels_json?: string;
};

const statuses: Array<{ value: NodeStatus; label: string }> = [
  { value: "new", label: "新建" },
  { value: "deploying", label: "部署中" },
  { value: "online", label: "在线" },
  { value: "offline", label: "离线" },
  { value: "error", label: "异常" },
  { value: "disabled", label: "停用" }
];

const emptyCreateValues: NodeFormValues = {
  node_id: "",
  name: "",
  region: "",
  country: "",
  provider: "",
  line_type: "",
  endpoint_host: "",
  endpoint_port: undefined,
  alpn: "",
  admin_host: "",
  admin_port: undefined,
  ssh_host: "",
  ssh_port: undefined,
  ssh_user: "",
  allow_tcp: true,
  allow_udp: true,
  tags: [],
  labels_json: "",
  status: "new",
  desired_version: "",
  desired_policy_revision: ""
};

function nodeToValues(node?: PanelNode): NodeFormValues {
  if (!node) {
    return { ...emptyCreateValues };
  }

  const labelsJSON = node.labels && Object.keys(node.labels).length > 0 ? JSON.stringify(node.labels, null, 2) : "";

  return {
    node_id: node.node_id,
    name: node.name,
    region: node.region,
    country: node.country,
    provider: node.provider,
    line_type: node.line_type,
    endpoint_host: node.endpoint_host,
    endpoint_port: node.endpoint_port,
    alpn: node.alpn,
    admin_host: node.admin_host,
    admin_port: node.admin_port,
    ssh_host: node.ssh_host,
    ssh_port: node.ssh_port,
    ssh_user: node.ssh_user,
    allow_tcp: node.allow_tcp,
    allow_udp: node.allow_udp,
    tags: node.tags,
    labels_json: labelsJSON,
    status: node.status,
    desired_version: node.desired_version,
    desired_policy_revision: node.desired_policy_revision
  };
}

function cleanText(value?: string): string {
  return value?.trim() ?? "";
}

function numberOrDefault(value: number | null | undefined, fallback: number): number {
  return value === undefined || value === null ? fallback : Number(value);
}

function normalizeValues(values: NodeFormValues): NodeInput {
  let labels: Record<string, string> = {};
  const labelsJSON = values.labels_json?.trim();
  if (labelsJSON) {
    const parsed = JSON.parse(labelsJSON) as unknown;
    if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") {
      throw new Error("扩展字段必须是 JSON 对象");
    }
    labels = Object.fromEntries(
      Object.entries(parsed as Record<string, unknown>).map(([key, value]) => [key, String(value)])
    );
  }

  const endpointHost = cleanText(values.endpoint_host);

  return {
    node_id: cleanText(values.node_id),
    name: cleanText(values.name),
    region: cleanText(values.region),
    country: cleanText(values.country),
    provider: cleanText(values.provider),
    line_type: cleanText(values.line_type),
    endpoint_host: endpointHost,
    endpoint_port: numberOrDefault(values.endpoint_port, 5555),
    alpn: cleanText(values.alpn) || "gaccel/1",
    admin_host: cleanText(values.admin_host) || "127.0.0.1",
    admin_port: numberOrDefault(values.admin_port, 5557),
    ssh_host: cleanText(values.ssh_host) || endpointHost,
    ssh_port: numberOrDefault(values.ssh_port, 22),
    ssh_user: cleanText(values.ssh_user) || "root",
    allow_tcp: values.allow_tcp ?? true,
    allow_udp: values.allow_udp ?? true,
    tags: values.tags ?? [],
    labels,
    status: values.status ?? "new",
    desired_version: cleanText(values.desired_version),
    desired_policy_revision: cleanText(values.desired_policy_revision)
  };
}

function FormSection({
  icon,
  title,
  description,
  children
}: {
  icon: ReactNode;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="node-form-section">
      <div className="node-form-section-header">
        <span className="node-form-section-icon">{icon}</span>
        <div>
          <h3>{title}</h3>
          <p>{description}</p>
        </div>
      </div>
      {children}
    </section>
  );
}

export function NodeFormModal({
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
  onSubmit: (input: NodeInput) => Promise<void>;
}) {
  const [form] = Form.useForm<NodeFormValues>();

  return (
    <Modal
      width={980}
      open={open}
      className="node-form-modal"
      title={
        <span className="modal-title-icon">
          <Server size={18} />
          {node ? "编辑节点" : "新增节点"}
        </span>
      }
      okText={node ? "保存" : "创建"}
      cancelText="取消"
      confirmLoading={loading}
      destroyOnClose
      onCancel={onCancel}
      onOk={() => form.submit()}
      afterOpenChange={(visible) => {
        if (visible) {
          form.setFieldsValue(nodeToValues(node));
        }
      }}
    >
      <Form
        form={form}
        layout="vertical"
        className="node-form"
        onFinish={async (values) => {
          try {
            await onSubmit(normalizeValues(values));
          } catch (error) {
            form.setFields([
              {
                name: "labels_json",
                errors: [error instanceof Error ? error.message : "扩展字段格式错误"]
              }
            ]);
          }
        }}
      >
        <FormSection
          icon={<Server size={16} />}
          title="基础信息"
          description="业务后台和控制面板识别节点所需的信息。"
        >
          <div className="form-grid two">
            <Form.Item
              label="节点 ID"
              name="node_id"
              extra={node ? "节点 ID 已绑定，编辑时不可修改。" : "建议使用业务后台生成的唯一节点 ID。"}
              rules={[{ required: true, message: "请输入节点 ID" }]}
            >
              <Input disabled={Boolean(node)} placeholder="输入唯一节点 ID" allowClear />
            </Form.Item>
            <Form.Item
              label="节点名称"
              name="name"
              extra="显示在控制面板和业务后台的节点名称。"
              rules={[{ required: true, message: "请输入节点名称" }]}
            >
              <Input placeholder="输入节点名称" allowClear />
            </Form.Item>
          </div>

          <div className="form-grid four">
            <Form.Item label="区域" name="region" extra="用于筛选和调度。">
              <Input placeholder="输入区域代码" allowClear />
            </Form.Item>
            <Form.Item label="国家/地区" name="country" extra="用于展示节点位置。">
              <Input placeholder="输入国家或地区代码" allowClear />
            </Form.Item>
            <Form.Item label="服务商" name="provider" extra="运营侧备注字段。">
              <Input placeholder="输入服务商名称" allowClear />
            </Form.Item>
            <Form.Item label="线路" name="line_type" extra="运营侧线路分组。">
              <Input placeholder="输入线路类型" allowClear />
            </Form.Item>
          </div>
        </FormSection>

        <FormSection
          icon={<Network size={16} />}
          title="连接入口"
          description="客户端通过这里连接节点，节点管理接口只建议内网或本机访问。"
        >
          <div className="form-grid three">
            <Form.Item
              label="客户端入口 Host"
              name="endpoint_host"
              extra="公网 IP 或域名，客户端会连接这个地址。"
              rules={[{ required: true, message: "请输入客户端入口 Host" }]}
            >
              <Input placeholder="输入公网 IP 或域名" allowClear />
            </Form.Item>
            <Form.Item label="QUIC 端口" name="endpoint_port" extra="留空使用默认端口 5555。">
              <InputNumber min={1} max={65535} className="full-input" placeholder="留空使用默认端口" />
            </Form.Item>
            <Form.Item label="ALPN" name="alpn" extra="留空使用 gaccel/1。">
              <Input placeholder="留空使用默认 ALPN" allowClear />
            </Form.Item>
          </div>

          <div className="form-grid three">
            <Form.Item label="Admin Host" name="admin_host" extra="留空使用 127.0.0.1。">
              <Input placeholder="留空使用本机地址" allowClear />
            </Form.Item>
            <Form.Item label="Admin 端口" name="admin_port" extra="留空使用 5557。">
              <InputNumber min={1} max={65535} className="full-input" placeholder="留空使用默认端口" />
            </Form.Item>
            <Form.Item label="状态" name="status" extra="新建节点建议保持新建状态。">
              <Select options={statuses} />
            </Form.Item>
          </div>
        </FormSection>

        <FormSection
          icon={<ShieldCheck size={16} />}
          title="SSH 与部署"
          description="用于一键部署和更新节点；如暂不部署，可以先留空 SSH Host。"
        >
          <div className="form-grid three">
            <Form.Item label="SSH Host" name="ssh_host" extra="留空时使用客户端入口 Host。">
              <Input placeholder="留空使用入口 Host" allowClear />
            </Form.Item>
            <Form.Item label="SSH 端口" name="ssh_port" extra="留空使用 22。">
              <InputNumber min={1} max={65535} className="full-input" placeholder="留空使用默认端口" />
            </Form.Item>
            <Form.Item label="SSH 用户" name="ssh_user" extra="留空使用 root。">
              <Input placeholder="留空使用默认用户" allowClear />
            </Form.Item>
          </div>
        </FormSection>

        <FormSection
          icon={<Settings2 size={16} />}
          title="能力与目标"
          description="控制节点支持的转发协议，以及面板希望节点更新到的版本和策略。"
        >
          <div className="form-grid four compact">
            <Form.Item label="TCP" name="allow_tcp" valuePropName="checked" extra="允许 TCP flow。">
              <Switch checkedChildren="开启" unCheckedChildren="关闭" />
            </Form.Item>
            <Form.Item label="UDP" name="allow_udp" valuePropName="checked" extra="允许 UDP flow。">
              <Switch checkedChildren="开启" unCheckedChildren="关闭" />
            </Form.Item>
            <Form.Item label="目标版本" name="desired_version" extra="留空表示不指定。">
              <Input placeholder="留空不指定版本" allowClear />
            </Form.Item>
            <Form.Item label="目标策略" name="desired_policy_revision" extra="留空表示不指定。">
              <Input placeholder="留空不指定策略" allowClear />
            </Form.Item>
          </div>
        </FormSection>

        <FormSection icon={<Tags size={16} />} title="标签与扩展字段" description="用于业务后台筛选、分组和同步扩展属性。">
          <Form.Item label="标签" name="tags" extra="输入标签后按 Enter 添加。">
            <Select mode="tags" tokenSeparators={[",", " "]} placeholder="添加标签" />
          </Form.Item>

          <Form.Item label="扩展字段 JSON" name="labels_json" extra="留空表示不设置扩展字段；只接受 JSON 对象。">
            <Input.TextArea rows={4} spellCheck={false} className="policy-yaml-input" />
          </Form.Item>
        </FormSection>
      </Form>
    </Modal>
  );
}
