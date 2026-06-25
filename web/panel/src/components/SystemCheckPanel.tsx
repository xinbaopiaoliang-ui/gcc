import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, Button, Empty, Space, Spin, Statistic, Tag, Typography } from "antd";
import { CheckCircle2, RefreshCw, ShieldAlert, TriangleAlert } from "lucide-react";
import { getSystemCheck } from "../api";
import type { DiagnosticCheck, DiagnosticStatus, SystemCheckResponse } from "../types";
import { TokenDefaultsPanel } from "./TokenDefaultsPanel";

const { Text, Title } = Typography;

function normalizeChecks(value?: DiagnosticCheck[]) {
  return Array.isArray(value) ? value : [];
}

function statusColor(status: DiagnosticStatus) {
  if (status === "ok") return "success";
  if (status === "warning") return "warning";
  if (status === "error") return "error";
  return "default";
}

function statusText(status: DiagnosticStatus) {
  if (status === "ok") return "正常";
  if (status === "warning") return "警告";
  if (status === "error") return "错误";
  return status || "-";
}

function statusIcon(status: DiagnosticStatus) {
  if (status === "ok") return <CheckCircle2 size={16} />;
  if (status === "error") return <ShieldAlert size={16} />;
  return <TriangleAlert size={16} />;
}

function formatDate(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatDetail(detail?: Record<string, unknown>) {
  if (!detail || Object.keys(detail).length === 0) {
    return "";
  }
  return JSON.stringify(detail, null, 2);
}

export function SystemCheckPanel({ onRequestError }: { onRequestError: (error: unknown) => void }) {
  const [data, setData] = useState<SystemCheckResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const checks = useMemo(() => normalizeChecks(data?.checks), [data?.checks]);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const response = await getSystemCheck();
      setData({
        ...response,
        checks: normalizeChecks(response.checks),
        config: {
          ...response.config,
          cors_allowed_origins: Array.isArray(response.config?.cors_allowed_origins)
            ? response.config.cors_allowed_origins
            : []
        }
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "系统自检失败");
      onRequestError(err);
    } finally {
      setLoading(false);
    }
  }, [onRequestError]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <main className="workbench system-check-panel">
      <div className="system-check-header">
        <div>
          <Text className="eyebrow">系统自检</Text>
          <Title level={3}>面板运行状态</Title>
          <Text type="secondary">检查控制面板配置、数据库表结构、登录安全、CORS 和部署目录，不回显任何密钥明文。</Text>
        </div>
        <Button icon={<RefreshCw size={16} />} loading={loading} onClick={() => void load()}>
          刷新
        </Button>
      </div>

      {error ? <Alert className="inline-alert" type="error" showIcon message={error} /> : null}

      <TokenDefaultsPanel onRequestError={onRequestError} />

      {loading && !data ? (
        <div className="system-loading">
          <Spin />
          <span>正在执行系统自检</span>
        </div>
      ) : data ? (
        <>
          <section className="diagnostic-summary">
            <div className="diagnostic-summary-item">
              <Statistic title="总体状态" value={statusText(data.status)} />
            </div>
            <div className="diagnostic-summary-item ok">
              <Statistic title="正常" value={data.summary?.ok ?? 0} />
            </div>
            <div className="diagnostic-summary-item warning">
              <Statistic title="警告" value={data.summary?.warning ?? 0} />
            </div>
            <div className="diagnostic-summary-item error">
              <Statistic title="错误" value={data.summary?.error ?? 0} />
            </div>
          </section>

          <section className="system-config-grid">
            <div>
              <span>后端版本</span>
              <strong>{data.version || "-"}</strong>
            </div>
            <div>
              <span>监听地址</span>
              <strong>{data.config.listen || "-"}</strong>
            </div>
            <div>
              <span>公网入口</span>
              <strong>{data.config.public_base_url || "-"}</strong>
            </div>
            <div>
              <span>数据库</span>
              <strong>{data.config.database_driver || "-"}</strong>
            </div>
            <div>
              <span>业务 API Key</span>
              <strong>{data.config.backend_api_key_count ?? 0}</strong>
            </div>
            <div>
              <span>登录有效期</span>
              <strong>{data.config.session_ttl_seconds ?? 0}s</strong>
            </div>
            <div>
              <span>CORS 来源</span>
              <strong>{data.config.cors_allowed_origins?.join(", ") || "-"}</strong>
            </div>
            <div>
              <span>生成时间</span>
              <strong>{formatDate(data.generated_at)}</strong>
            </div>
          </section>

          <section className="diagnostic-list">
            {checks.length ? (
              checks.map((check) => {
                const detail = formatDetail(check.detail);
                return (
                  <div className={`diagnostic-row ${check.status}`} key={check.key}>
                    <div className="diagnostic-icon">{statusIcon(check.status)}</div>
                    <div className="diagnostic-main">
                      <div className="diagnostic-title">
                        <strong>{check.label}</strong>
                        <Tag color={statusColor(check.status)}>{statusText(check.status)}</Tag>
                      </div>
                      <Text>{check.message}</Text>
                      {detail ? <pre>{detail}</pre> : null}
                    </div>
                  </div>
                );
              })
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无自检结果" />
            )}
          </section>
        </>
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无系统自检数据" />
      )}
    </main>
  );
}
