import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, Button, Empty, Input, InputNumber, Space, Switch, Table, Tag, Typography } from "antd";
import type { TableColumnsType } from "antd";
import { RefreshCw, Save, SlidersHorizontal } from "lucide-react";
import { getTokenDefaults, saveTokenDefaults } from "../api";
import type { TokenPlanDefault } from "../types";

const { Text } = Typography;

function normalizePlans(value?: TokenPlanDefault[]) {
  return Array.isArray(value) ? value : [];
}

function clonePlans(plans: TokenPlanDefault[]) {
  return plans.map((plan) => ({ ...plan }));
}

export function TokenDefaultsPanel({ onRequestError }: { onRequestError: (error: unknown) => void }) {
  const [plans, setPlans] = useState<TokenPlanDefault[]>([]);
  const [savedPlans, setSavedPlans] = useState<TokenPlanDefault[]>([]);
  const [nodeHardLimit, setNodeHardLimit] = useState(512);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const maxConfigured = useMemo(
    () => Math.max(0, ...plans.map((plan) => Number(plan.max_connections) || 0)),
    [plans]
  );
  const hasDirtyState = useMemo(() => JSON.stringify(plans) !== JSON.stringify(savedPlans), [plans, savedPlans]);

  const updatePlan = useCallback((planID: string, patch: Partial<TokenPlanDefault>) => {
    setPlans((current) => current.map((plan) => (plan.plan_id === planID ? { ...plan, ...patch } : plan)));
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const response = await getTokenDefaults();
      const nextPlans = clonePlans(normalizePlans(response.token_defaults?.plans));
      setPlans(nextPlans);
      setSavedPlans(clonePlans(nextPlans));
      setNodeHardLimit(response.token_defaults?.node_hard_limit || 512);
    } catch (err) {
      setError(err instanceof Error ? err.message : "读取默认值失败");
      onRequestError(err);
    } finally {
      setLoading(false);
    }
  }, [onRequestError]);

  useEffect(() => {
    void load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      const response = await saveTokenDefaults({ plans });
      const nextPlans = clonePlans(normalizePlans(response.token_defaults?.plans));
      setPlans(nextPlans);
      setSavedPlans(clonePlans(nextPlans));
      setNodeHardLimit(response.token_defaults?.node_hard_limit || 512);
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存默认值失败");
      onRequestError(err);
    } finally {
      setSaving(false);
    }
  };

  const columns: TableColumnsType<TokenPlanDefault> = [
    {
      title: "档位",
      dataIndex: "name",
      width: 160,
      render: (_value, record) => (
        <div className="token-plan-name">
          <strong>{record.name}</strong>
          <Text type="secondary">{record.plan_id}</Text>
        </div>
      )
    },
    {
      title: "默认连接数",
      dataIndex: "max_connections",
      width: 170,
      render: (value, record) => (
        <InputNumber
          min={1}
          max={nodeHardLimit}
          value={value}
          onChange={(nextValue) => updatePlan(record.plan_id, { max_connections: Number(nextValue) || 1 })}
        />
      )
    },
    {
      title: "默认限速 Mbps",
      dataIndex: "rate_limit_mbps",
      width: 170,
      render: (value, record) => (
        <InputNumber
          min={1}
          max={10000}
          value={value}
          onChange={(nextValue) => updatePlan(record.plan_id, { rate_limit_mbps: Number(nextValue) || 1 })}
        />
      )
    },
    {
      title: "TCP",
      dataIndex: "allow_tcp",
      width: 96,
      render: (value, record) => (
        <Switch checked={Boolean(value)} onChange={(checked) => updatePlan(record.plan_id, { allow_tcp: checked })} />
      )
    },
    {
      title: "UDP",
      dataIndex: "allow_udp",
      width: 96,
      render: (value, record) => (
        <Switch checked={Boolean(value)} onChange={(checked) => updatePlan(record.plan_id, { allow_udp: checked })} />
      )
    },
    {
      title: "备注",
      dataIndex: "description",
      render: (value, record) => (
        <Input
          value={value}
          maxLength={255}
          onChange={(event) => updatePlan(record.plan_id, { description: event.target.value })}
        />
      )
    }
  ];

  return (
    <section className="token-defaults-panel">
      <div className="token-defaults-heading">
        <div className="token-defaults-title">
          <div className="token-defaults-icon">
            <SlidersHorizontal size={18} />
          </div>
          <div>
            <Text className="eyebrow">TOKEN DEFAULTS</Text>
            <h3>客户端会话默认值</h3>
          </div>
        </div>
        <Space wrap>
          <Tag color={maxConfigured > nodeHardLimit ? "error" : "blue"}>节点硬上限 {nodeHardLimit}</Tag>
          <Tag color="geekblue">当前最大 {maxConfigured}</Tag>
          <Button icon={<RefreshCw size={16} />} loading={loading} onClick={() => void load()}>
            刷新
          </Button>
          <Button
            type="primary"
            icon={<Save size={16} />}
            disabled={!hasDirtyState}
            loading={saving}
            onClick={() => void save()}
          >
            保存默认值
          </Button>
        </Space>
      </div>

      {error ? <Alert className="inline-alert" type="error" showIcon message={error} /> : null}

      <Table<TokenPlanDefault>
        rowKey="plan_id"
        className="token-defaults-table"
        loading={loading}
        pagination={false}
        scroll={{ x: 920 }}
        columns={columns}
        dataSource={plans}
        locale={{
          emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无默认档位" />
        }}
      />
    </section>
  );
}
