import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Checkbox,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  message
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { AlertTriangle, CheckCircle2, FileCode2, GitBranch, RefreshCw, Route, Save, Search, Send, ShieldCheck } from "lucide-react";
import { listPolicyRevisions, savePolicyRevision, setNodeDesiredPolicy, validatePolicyRevision } from "../api";
import type {
  DesiredPolicyInput,
  PanelNode,
  PolicyRevision,
  PolicyRevisionInput,
  PolicyValidationResponse
} from "../types";

const { Text, Title } = Typography;

type PolicySourceFilter = "" | "manual" | "backend";

type PolicySummary = {
  revision: string;
  games: string[];
  policies: string[];
  rules: number;
  tcp: boolean;
  udp: boolean;
};

type SyncForm = DesiredPolicyInput & {
  node_id: string;
};

type PolicyBuilderState = {
  revision: string;
  game_id: string;
  policy_id: string;
  policy_name: string;
  allow_tcp: boolean;
  allow_udp: boolean;
  rule_id: string;
  network: "tcp" | "udp" | "any";
  target_type: "domain" | "domain_suffix" | "ip" | "cidr" | "any";
  target_value: string;
  port_start: number | null;
  port_end: number | null;
  action: "quic_relay";
  priority: number | null;
};

function defaultPolicyBuilder(): PolicyBuilderState {
  return {
    revision: "",
    game_id: "",
    policy_id: "",
    policy_name: "",
    allow_tcp: true,
    allow_udp: true,
    rule_id: "",
    network: "tcp",
    target_type: "domain_suffix",
    target_value: "",
    port_start: null,
    port_end: null,
    action: "quic_relay",
    priority: null
  };
}

function yamlQuote(value: string) {
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

function buildPolicyYAML(input: PolicyBuilderState & { revision: string; port_start: number; port_end: number }) {
  const lines = [
    "route_policies:",
    `  revision: ${yamlQuote(input.revision)}`,
    "  policies:",
    `    - policy_id: ${yamlQuote(input.policy_id.trim())}`,
    `      game_id: ${yamlQuote(input.game_id.trim())}`
  ];
  if (input.policy_name.trim()) {
    lines.push(`      name: ${yamlQuote(input.policy_name.trim())}`);
  }
  lines.push(
    `      allow_tcp: ${input.allow_tcp ? "true" : "false"}`,
    `      allow_udp: ${input.allow_udp ? "true" : "false"}`,
    "      rules:",
    `        - rule_id: ${yamlQuote(input.rule_id.trim())}`,
    `          network: ${yamlQuote(input.network)}`,
    `          target_type: ${yamlQuote(input.target_type)}`,
    `          target_value: ${yamlQuote(input.target_type === "any" ? "" : input.target_value.trim())}`,
    `          port_start: ${input.port_start}`,
    `          port_end: ${input.port_end}`,
    `          action: ${yamlQuote(input.action)}`
  );
  if (input.priority && input.priority > 0) {
    lines.push(`          priority: ${input.priority}`);
  }
  lines.push("          enabled: true");
  return `${lines.join("\n")}\n`;
}

function uniqueMatches(yaml: string, pattern: RegExp) {
  const values = new Set<string>();
  for (const match of yaml.matchAll(pattern)) {
    const value = (match[1] ?? "").trim();
    if (value) {
      values.add(value);
    }
  }
  return Array.from(values);
}

function parsePolicySummary(policy: PolicyRevision): PolicySummary {
  const yaml = policy.route_policies_yaml || "";
  const revision =
    yaml.match(/revision:\s*["']?([^"'\n#]+)/i)?.[1]?.trim() ||
    policy.revision ||
    "-";
  const games = uniqueMatches(yaml, /game_id:\s*["']?([^"'\n#]+)/gi);
  const policies = uniqueMatches(yaml, /policy_id:\s*["']?([^"'\n#]+)/gi);
  const rules = (yaml.match(/rule_id:\s*["']?[^"'\n#]+/gi) || []).length;
  const tcp = /allow_tcp:\s*true/i.test(yaml) || /network:\s*["']?tcp/i.test(yaml);
  const udp = /allow_udp:\s*true/i.test(yaml) || /network:\s*["']?udp/i.test(yaml);
  return { revision, games, policies, rules, tcp, udp };
}

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

function shortSha(value: string) {
  return value ? `${value.slice(0, 12)}...${value.slice(-8)}` : "-";
}

function normalizePolicyYAML(value: string) {
  const normalized = value
    .replace(/\r\n/g, "\n")
    .replace(/\\r\\n/g, "\n")
    .replace(/\\n/g, "\n")
    .replace(/\\"/g, '"')
    .trim();
  return normalized || "# empty route_policies";
}

function sourceText(value: PolicyRevision["source"] | string) {
  if (value === "backend") {
    return "业务后台";
  }
  if (value === "manual") {
    return "手动录入";
  }
  return value || "-";
}

function policyErrorText(error: unknown) {
  if (error instanceof Error) {
    return error.message;
  }
  return "请求失败";
}

function PolicyValidationPanel({ validation }: { validation: PolicyValidationResponse | null }) {
  if (!validation) {
    return (
      <div className="policy-validation-card muted">
        <ShieldCheck size={18} />
        <div>
          <strong>保存前会自动校验</strong>
          <span>检查 YAML 结构、节点策略语义、SHA256、规则数量和差异摘要。</span>
        </div>
      </div>
    );
  }
  const summary = validation.summary;
  const diff = validation.diff;
  return (
    <div className={`policy-validation-card ${validation.valid ? "ok" : "bad"}`}>
      {validation.valid ? <CheckCircle2 size={18} /> : <AlertTriangle size={18} />}
      <div className="policy-validation-body">
        <strong>{validation.valid ? "策略校验通过" : "策略校验未通过"}</strong>
        <div className="policy-validation-stats">
          <Tag>SHA {shortSha(validation.sha256)}</Tag>
          <Tag>策略 {summary.policy_count || 0}</Tag>
          <Tag>规则 {summary.rule_count || 0}</Tag>
          <Tag>可转发 {summary.relay_rule_count || 0}</Tag>
          <Tag>游戏 {(summary.games || []).length}</Tag>
        </div>
        {validation.errors?.length ? (
          <ul className="policy-validation-list error-list">
            {validation.errors.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        ) : null}
        {validation.warnings?.length ? (
          <ul className="policy-validation-list warning-list">
            {validation.warnings.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        ) : null}
        {diff ? (
          <div className="policy-diff-summary">
            <span>差异</span>
            <code>
              +策略 {diff.added_policies?.length || 0} / -策略 {diff.removed_policies?.length || 0} / 改策略{" "}
              {diff.changed_policies?.length || 0} / +规则 {diff.added_rules?.length || 0} / -规则{" "}
              {diff.removed_rules?.length || 0} / 改规则 {diff.changed_rules?.length || 0}
            </code>
          </div>
        ) : null}
      </div>
    </div>
  );
}

function TerminalPolicyPreview({ policy }: { policy: PolicyRevision }) {
  const yaml = normalizePolicyYAML(policy.route_policies_yaml);
  const lines = yaml.split("\n");
  const summary = parsePolicySummary({
    ...policy,
    route_policies_yaml: yaml
  });

  return (
    <div className="policy-terminal-preview">
      <div className="terminal-topbar">
        <div className="terminal-dots" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
        <div className="terminal-title">
          <strong>{policy.revision}</strong>
          <span>策略配置 YAML</span>
        </div>
        <Tag className="terminal-source-tag">{sourceText(policy.source)}</Tag>
      </div>

      <div className="terminal-meta-grid">
        <div className="terminal-meta-item">
          <span>策略版本</span>
          <strong>{summary.revision}</strong>
        </div>
        <div className="terminal-meta-item">
          <span>游戏 ID</span>
          <strong>{summary.games.join(", ") || "-"}</strong>
        </div>
        <div className="terminal-meta-item">
          <span>策略 ID</span>
          <strong>{summary.policies.join(", ") || "-"}</strong>
        </div>
        <div className="terminal-meta-item">
          <span>规则数量</span>
          <strong>{summary.rules}</strong>
        </div>
        <div className="terminal-meta-item">
          <span>协议</span>
          <strong>
            {summary.tcp ? "TCP" : ""}
            {summary.tcp && summary.udp ? " / " : ""}
            {summary.udp ? "UDP" : ""}
            {!summary.tcp && !summary.udp ? "-" : ""}
          </strong>
        </div>
        <div className="terminal-meta-item wide">
          <span>校验 SHA256</span>
          <strong>{policy.sha256 || "-"}</strong>
        </div>
      </div>

      <div className="terminal-code-wrap" role="region" aria-label="策略 YAML">
        <div className="terminal-code">
          {lines.map((line, index) => (
            <div className="terminal-line" key={`${index}-${line}`}>
              <span className="terminal-line-number">{String(index + 1).padStart(2, "0")}</span>
              <code>{line || " "}</code>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function PolicyManagementPanel({
  nodes,
  canManage,
  onNodesRefresh,
  onRequestError
}: {
  nodes: PanelNode[];
  canManage: boolean;
  onNodesRefresh: () => Promise<void> | void;
  onRequestError: (error: unknown) => void;
}) {
  const [messageAPI, contextHolder] = message.useMessage();
  const [policies, setPolicies] = useState<PolicyRevision[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [validating, setValidating] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [syncOpen, setSyncOpen] = useState(false);
  const [previewPolicy, setPreviewPolicy] = useState<PolicyRevision | null>(null);
  const [syncPolicy, setSyncPolicy] = useState<PolicyRevision | null>(null);
  const [validation, setValidation] = useState<PolicyValidationResponse | null>(null);
  const [query, setQuery] = useState("");
  const [source, setSource] = useState<PolicySourceFilter>("");
  const [error, setError] = useState("");
  const [builder, setBuilder] = useState<PolicyBuilderState>(() => defaultPolicyBuilder());
  const [createForm] = Form.useForm<PolicyRevisionInput>();
  const [syncForm] = Form.useForm<SyncForm>();

  const loadPolicies = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const response = await listPolicyRevisions("", 100);
      setPolicies(response.policy_revisions);
    } catch (err) {
      setError(policyErrorText(err));
      onRequestError(err);
    } finally {
      setLoading(false);
    }
  }, [onRequestError]);

  useEffect(() => {
    void loadPolicies();
  }, [loadPolicies]);

  const enrichedPolicies = useMemo(
    () =>
      policies.map((policy) => ({
        ...policy,
        summary: parsePolicySummary(policy)
      })),
    [policies]
  );

  const filteredPolicies = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return enrichedPolicies.filter((policy) => {
      const summary = policy.summary;
      const sourceMatched = source === "" || policy.source === source;
      const keywordMatched =
        keyword === "" ||
        policy.revision.toLowerCase().includes(keyword) ||
        policy.sha256.toLowerCase().includes(keyword) ||
        summary.games.some((item) => item.toLowerCase().includes(keyword)) ||
        summary.policies.some((item) => item.toLowerCase().includes(keyword));
      return sourceMatched && keywordMatched;
    });
  }, [enrichedPolicies, query, source]);

  const policyStats = useMemo(() => {
    const games = new Set<string>();
    const ids = new Set<string>();
    let rules = 0;
    for (const policy of enrichedPolicies) {
      policy.summary.games.forEach((item) => games.add(item));
      policy.summary.policies.forEach((item) => ids.add(item));
      rules += policy.summary.rules;
    }
    return {
      revisions: policies.length,
      games: games.size,
      policyIDs: ids.size,
      rules
    };
  }, [enrichedPolicies, policies.length]);

  const nodeOptions = useMemo(
    () =>
      nodes.map((node) => ({
        value: node.node_id,
        label: `${node.node_id} / ${node.name || node.endpoint_host}`
      })),
    [nodes]
  );

  const openSync = (policy: PolicyRevision) => {
    setSyncPolicy(policy);
    syncForm.setFieldsValue({
      revision: policy.revision,
      create_task: true,
      priority: 100
    });
    setSyncOpen(true);
  };

  const openCreatePolicy = () => {
    setValidation(null);
    setBuilder(defaultPolicyBuilder());
    createForm.resetFields();
    setCreateOpen(true);
  };

  const updateBuilder = <K extends keyof PolicyBuilderState>(key: K, value: PolicyBuilderState[K]) => {
    setBuilder((current) => ({ ...current, [key]: value }));
  };

  const generatePolicyYAML = () => {
    const revision = (builder.revision || createForm.getFieldValue("revision") || "").trim();
    const portStart = builder.port_start ?? 0;
    const portEnd = builder.port_end ?? portStart;
    const missing = [
      [revision, "策略版本"],
      [builder.game_id.trim(), "游戏 ID"],
      [builder.policy_id.trim(), "策略 ID"],
      [builder.rule_id.trim(), "规则 ID"]
    ].filter(([value]) => !value);
    if (builder.target_type !== "any" && builder.target_value.trim() === "") {
      missing.push(["", "目标"]);
    }
    if (missing.length > 0) {
      messageAPI.warning(`请先填写：${missing.map(([, label]) => label).join("、")}`);
      return;
    }
    if (portStart < 1 || portStart > 65535 || portEnd < 1 || portEnd > 65535 || portEnd < portStart) {
      messageAPI.warning("端口范围必须在 1-65535 之间，且结束端口不能小于起始端口");
      return;
    }
    const yaml = buildPolicyYAML({
      ...builder,
      revision,
      port_start: portStart,
      port_end: portEnd
    });
    createForm.setFieldsValue({
      revision,
      sha256: "",
      route_policies_yaml: yaml
    });
    setValidation(null);
    messageAPI.success("已生成 route_policies YAML，请继续执行校验");
  };

  const runValidation = async (values: PolicyRevisionInput, showSuccess = true) => {
    setValidating(true);
    try {
      const revision = values.revision?.trim() ?? "";
      const basePolicy = policies.find((policy) => policy.revision === revision);
      const result = await validatePolicyRevision("", {
        revision,
        sha256: values.sha256?.trim(),
        route_policies_yaml: values.route_policies_yaml,
        base_revision: basePolicy?.revision
      });
      setValidation(result);
      if (showSuccess) {
        if (result.valid) {
          messageAPI.success("策略校验通过");
        } else {
          messageAPI.warning("策略校验未通过，请查看错误列表");
        }
      }
      return result;
    } catch (err) {
      onRequestError(err);
      throw err;
    } finally {
      setValidating(false);
    }
  };

  const validateCurrentForm = async () => {
    const values = await createForm.validateFields();
    await runValidation(values);
  };

  const submitPolicy = async (values: PolicyRevisionInput) => {
    setSaving(true);
    try {
      const validationResult = await runValidation(values, false);
      if (!validationResult.valid) {
        messageAPI.error("策略校验未通过，已阻止保存");
        return;
      }
      await savePolicyRevision("", {
        revision: values.revision.trim(),
        sha256: values.sha256?.trim(),
        route_policies_yaml: values.route_policies_yaml
      });
      messageAPI.success("策略版本已保存");
      createForm.resetFields();
      setCreateOpen(false);
      await loadPolicies();
    } catch (err) {
      onRequestError(err);
      throw err;
    } finally {
      setSaving(false);
    }
  };

  const submitSync = async (values: SyncForm) => {
    setSyncing(true);
    try {
      await setNodeDesiredPolicy("", values.node_id, {
        revision: values.revision,
        create_task: values.create_task ?? true,
        priority: values.priority ?? 100
      });
      messageAPI.success("策略已进入节点待同步队列");
      setSyncOpen(false);
      syncForm.resetFields();
      await onNodesRefresh();
    } catch (err) {
      onRequestError(err);
      throw err;
    } finally {
      setSyncing(false);
    }
  };

  const columns: ColumnsType<PolicyRevision & { summary: PolicySummary }> = [
    {
      title: "策略版本",
      dataIndex: "revision",
      width: 230,
      render: (_, policy) => (
        <div className="policy-identity">
          <button className="link-button" type="button" onClick={() => setPreviewPolicy(policy)}>
            {policy.revision}
          </button>
          <Text className="mono subtle">{shortSha(policy.sha256)}</Text>
        </div>
      )
    },
    {
      title: "游戏",
      width: 180,
      render: (_, policy) => (
        <div className="compact-tags">
          {policy.summary.games.length ? policy.summary.games.slice(0, 3).map((game) => <Tag key={game}>{game}</Tag>) : "-"}
        </div>
      )
    },
    {
      title: "策略 ID",
      width: 220,
      render: (_, policy) => (
        <div className="compact-tags">
          {policy.summary.policies.length
            ? policy.summary.policies.slice(0, 3).map((item) => <Tag key={item}>{item}</Tag>)
            : "-"}
        </div>
      )
    },
    {
      title: "规则/协议",
      width: 150,
      render: (_, policy) => (
        <Space size={6} wrap>
          <Tag>{policy.summary.rules} 条规则</Tag>
          <Tag color={policy.summary.tcp ? "blue" : "default"}>TCP</Tag>
          <Tag color={policy.summary.udp ? "green" : "default"}>UDP</Tag>
        </Space>
      )
    },
    {
      title: "来源",
      dataIndex: "source",
      width: 100,
      render: (value: PolicyRevision["source"]) => (
        <Tag color={value === "backend" ? "geekblue" : "default"}>{sourceText(value)}</Tag>
      )
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      width: 180,
      render: (value: string) => <Text className="mono subtle">{formatDate(value)}</Text>
    },
    {
      title: "操作",
      fixed: "right",
      width: canManage ? 130 : 70,
      render: (_, policy) => (
        <Space size={4}>
          <Tooltip title="预览 YAML">
            <Button type="text" icon={<FileCode2 size={16} />} onClick={() => setPreviewPolicy(policy)} />
          </Tooltip>
          {canManage && (
            <Tooltip title="同步到节点">
              <Button type="text" icon={<Send size={16} />} onClick={() => openSync(policy)} />
            </Tooltip>
          )}
        </Space>
      )
    }
  ];

  return (
    <main className="workbench policy-panel">
      {contextHolder}
      <div className="policy-header">
        <div>
          <Text className="eyebrow">路由策略</Text>
          <Title level={3}>策略与游戏配置</Title>
          <Text type="secondary">维护业务后台生成的 route_policies，并把目标策略同步到指定节点。</Text>
        </div>
        <Space wrap>
          <Tag color="blue">版本 {policyStats.revisions}</Tag>
          <Tag color="cyan">游戏 {policyStats.games}</Tag>
          <Tag color="green">策略 {policyStats.policyIDs}</Tag>
          <Tag>规则 {policyStats.rules}</Tag>
          <Button icon={<RefreshCw size={16} />} onClick={() => void loadPolicies()} loading={loading}>
            刷新
          </Button>
          {canManage && (
            <Button type="primary" icon={<Save size={16} />} onClick={openCreatePolicy}>
              保存策略
            </Button>
          )}
        </Space>
      </div>

      <section className="policy-flow">
        <div className="policy-flow-step">
          <GitBranch size={18} />
          <div>
            <strong>业务后台生成</strong>
            <span>游戏 ID、策略 ID、规则汇总为 YAML</span>
          </div>
        </div>
        <div className="policy-flow-step">
          <ShieldCheck size={18} />
          <div>
            <strong>控制面板保存</strong>
            <span>记录 revision、sha256 和来源，方便审计</span>
          </div>
        </div>
        <div className="policy-flow-step">
          <Route size={18} />
          <div>
            <strong>节点拉取应用</strong>
            <span>下发 desired_policy_revision，节点热更新并回报</span>
          </div>
        </div>
      </section>

      <div className="policy-toolbar">
        <Input
          className="policy-search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          prefix={<Search size={16} />}
          placeholder="搜索策略版本、游戏 ID、策略 ID、SHA256"
        />
        <Select
          className="users-filter"
          allowClear
          value={source || undefined}
          onChange={(value) => setSource((value ?? "") as PolicySourceFilter)}
          placeholder="来源"
          options={[
            { value: "manual", label: "手动录入" },
            { value: "backend", label: "业务后台" }
          ]}
        />
        <Text className="users-result-count">当前显示 {filteredPolicies.length} 个策略版本</Text>
      </div>

      {error && <Alert className="inline-alert" type="error" showIcon message={error} />}

      {policies.length === 0 && !loading ? (
        <div className="policy-empty">
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无策略版本">
            {canManage && (
              <Button type="primary" icon={<Save size={16} />} onClick={openCreatePolicy}>
                保存第一份策略
              </Button>
            )}
          </Empty>
        </div>
      ) : (
        <Table
          className="policy-table"
          columns={columns}
          dataSource={filteredPolicies}
          loading={loading}
          rowKey="revision"
          scroll={{ x: 1180 }}
          pagination={{
            pageSize: 10,
            showSizeChanger: false,
            showTotal: (total) => `共 ${total} 个策略版本`
          }}
        />
      )}

      <Modal
        title="保存策略版本"
        open={createOpen}
        confirmLoading={saving || validating}
        okText="保存"
        cancelText="取消"
        width={860}
        onCancel={() => {
          setCreateOpen(false);
          setValidation(null);
        }}
        onOk={() => createForm.submit()}
        destroyOnClose
      >
        <Form form={createForm} layout="vertical" onFinish={submitPolicy}>
          <section className="policy-builder-panel">
            <div className="policy-builder-head">
              <div>
                <strong>可视化生成器</strong>
                <span>用于快速生成一条 TCP/UDP 初始规则；复杂规则可继续在 YAML 中编辑。</span>
              </div>
              <Button icon={<Route size={16} />} onClick={generatePolicyYAML}>
                生成 YAML
              </Button>
            </div>
            <div className="form-grid four compact">
              <div className="builder-field">
                <label>策略版本</label>
                <Input
                  value={builder.revision}
                  placeholder="例如 20260618.1"
                  onChange={(event) => updateBuilder("revision", event.target.value)}
                />
              </div>
              <div className="builder-field">
                <label>游戏 ID</label>
                <Input
                  value={builder.game_id}
                  placeholder="例如 steam"
                  onChange={(event) => updateBuilder("game_id", event.target.value)}
                />
              </div>
              <div className="builder-field">
                <label>策略 ID</label>
                <Input
                  value={builder.policy_id}
                  placeholder="例如 steam-web-v1"
                  onChange={(event) => updateBuilder("policy_id", event.target.value)}
                />
              </div>
              <div className="builder-field">
                <label>策略名称</label>
                <Input
                  value={builder.policy_name}
                  placeholder="可选"
                  onChange={(event) => updateBuilder("policy_name", event.target.value)}
                />
              </div>
            </div>
            <div className="form-grid four compact">
              <div className="builder-field">
                <label>规则 ID</label>
                <Input
                  value={builder.rule_id}
                  placeholder="例如 steam-store-tcp-443"
                  onChange={(event) => updateBuilder("rule_id", event.target.value)}
                />
              </div>
              <div className="builder-field">
                <label>协议</label>
                <Select
                  value={builder.network}
                  onChange={(value) => updateBuilder("network", value as PolicyBuilderState["network"])}
                  options={[
                    { value: "tcp", label: "TCP" },
                    { value: "udp", label: "UDP" },
                    { value: "any", label: "TCP / UDP" }
                  ]}
                />
              </div>
              <div className="builder-field">
                <label>目标类型</label>
                <Select
                  value={builder.target_type}
                  onChange={(value) => updateBuilder("target_type", value as PolicyBuilderState["target_type"])}
                  options={[
                    { value: "domain_suffix", label: "域名后缀" },
                    { value: "domain", label: "完整域名" },
                    { value: "ip", label: "IP" },
                    { value: "cidr", label: "CIDR" },
                    { value: "any", label: "任意目标" }
                  ]}
                />
              </div>
              <div className="builder-field">
                <label>目标</label>
                <Input
                  value={builder.target_value}
                  disabled={builder.target_type === "any"}
                  placeholder={builder.target_type === "any" ? "任意目标无需填写" : "域名、IP 或 CIDR"}
                  onChange={(event) => updateBuilder("target_value", event.target.value)}
                />
              </div>
            </div>
            <div className="form-grid four compact">
              <div className="builder-field">
                <label>起始端口</label>
                <InputNumber
                  className="full-input"
                  min={1}
                  max={65535}
                  value={builder.port_start}
                  placeholder="1-65535"
                  onChange={(value) => updateBuilder("port_start", typeof value === "number" ? value : null)}
                />
              </div>
              <div className="builder-field">
                <label>结束端口</label>
                <InputNumber
                  className="full-input"
                  min={1}
                  max={65535}
                  value={builder.port_end}
                  placeholder="留空则同起始端口"
                  onChange={(value) => updateBuilder("port_end", typeof value === "number" ? value : null)}
                />
              </div>
              <div className="builder-field">
                <label>规则动作</label>
                <Select value={builder.action} disabled options={[{ value: "quic_relay", label: "QUIC 转发" }]} />
              </div>
              <div className="builder-field">
                <label>优先级</label>
                <InputNumber
                  className="full-input"
                  min={1}
                  max={1000}
                  value={builder.priority}
                  placeholder="可选"
                  onChange={(value) => updateBuilder("priority", typeof value === "number" ? value : null)}
                />
              </div>
            </div>
            <div className="policy-builder-switches">
              <Checkbox checked={builder.allow_tcp} onChange={(event) => updateBuilder("allow_tcp", event.target.checked)}>
                策略允许 TCP
              </Checkbox>
              <Checkbox checked={builder.allow_udp} onChange={(event) => updateBuilder("allow_udp", event.target.checked)}>
                策略允许 UDP
              </Checkbox>
            </div>
          </section>
          <div className="form-grid two">
            <Form.Item
              label="策略版本"
              name="revision"
              rules={[{ required: true, message: "请填写策略版本" }]}
              extra="建议和客户端 config_revision 保持一致，例如 20260618.1。"
            >
              <Input placeholder="输入 revision" />
            </Form.Item>
            <Form.Item label="SHA256" name="sha256" extra="可留空，后端会按 YAML 原文计算。">
              <Input placeholder="可选" />
            </Form.Item>
          </div>
          <div className="policy-validation-actions">
            <PolicyValidationPanel validation={validation} />
            <Button icon={<ShieldCheck size={16} />} onClick={() => void validateCurrentForm()} loading={validating}>
              校验策略
            </Button>
          </div>
          <Form.Item
            label="路由策略 YAML"
            name="route_policies_yaml"
            rules={[{ required: true, message: "请粘贴路由策略 YAML" }]}
            extra="可以包含顶层 route_policies，也可以直接填写 revision 和 policies 块。"
          >
            <Input.TextArea
              className="policy-yaml-input"
              rows={16}
              spellCheck={false}
              placeholder={'route_policies:\n  revision: "20260618.1"\n  policies:\n    - policy_id: "..."\n      game_id: "..."\n      rules: []'}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={syncPolicy ? `同步策略：${syncPolicy.revision}` : "同步策略"}
        open={syncOpen}
        confirmLoading={syncing}
        okText="下发"
        cancelText="取消"
        width={620}
        onCancel={() => setSyncOpen(false)}
        onOk={() => syncForm.submit()}
        destroyOnClose
      >
        <Form form={syncForm} layout="vertical" onFinish={submitSync}>
          <Alert
            className="inline-alert"
            type="info"
            showIcon
            message="该操作会写入节点 desired_policy_revision，并按需创建 apply_policy 任务。"
          />
          <Form.Item name="revision" label="策略版本" rules={[{ required: true, message: "请选择策略版本" }]}>
            <Input disabled />
          </Form.Item>
          <Form.Item name="node_id" label="目标节点" rules={[{ required: true, message: "请选择目标节点" }]}>
            <Select showSearch options={nodeOptions} placeholder="选择节点" />
          </Form.Item>
          <div className="form-grid two">
            <Form.Item name="create_task" valuePropName="checked" initialValue>
              <Checkbox>创建 apply_policy 任务</Checkbox>
            </Form.Item>
            <Form.Item name="priority" label="任务优先级" initialValue={100}>
              <InputNumber className="full-input" min={1} max={1000} />
            </Form.Item>
          </div>
        </Form>
      </Modal>

      <Modal
        title={previewPolicy ? `策略预览：${previewPolicy.revision}` : "策略预览"}
        open={previewPolicy !== null}
        className="policy-preview-modal"
        rootClassName="policy-preview-root"
        footer={null}
        width={980}
        onCancel={() => setPreviewPolicy(null)}
      >
        {previewPolicy ? <TerminalPolicyPreview policy={previewPolicy} /> : null}
      </Modal>
    </main>
  );
}
