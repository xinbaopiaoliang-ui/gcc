import { Cpu, HardDrive, MemoryStick, Network } from "lucide-react";
import type { ReactNode } from "react";
import type { NodeSystemSnapshot } from "../types";

function clampPercent(value?: number) {
  if (!Number.isFinite(value ?? NaN)) return 0;
  return Math.max(0, Math.min(100, Number(value)));
}

function formatPercent(value?: number) {
  if (!Number.isFinite(value ?? NaN)) return "-";
  const number = clampPercent(value);
  return `${number >= 10 ? number.toFixed(0) : number.toFixed(1)}%`;
}

function formatBytes(bytes?: number) {
  if (!Number.isFinite(bytes ?? NaN) || (bytes ?? 0) <= 0) return "-";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = Number(bytes);
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 ? value.toFixed(1) : value.toFixed(2)} ${units[unit]}`;
}

function MetricRow({
  label,
  value,
  percent,
  icon
}: {
  label: string;
  value: string;
  percent?: number;
  icon: ReactNode;
}) {
  const width = clampPercent(percent);
  return (
    <div className="node-load-row">
      <div className="node-load-row-head">
        <span>
          {icon}
          {label}
        </span>
        <strong>{value}</strong>
      </div>
      {percent === undefined ? null : (
        <div className="node-load-bar">
          <span style={{ width: `${width}%` }} />
        </div>
      )}
    </div>
  );
}

export function NodeLoadMetrics({
  system,
  variant = "compact"
}: {
  system?: NodeSystemSnapshot;
  variant?: "compact" | "detail";
}) {
  if (!system) {
    return <span className="node-load-empty">等待上报</span>;
  }

  const memoryUsed = system.memory ? `${formatBytes(system.memory.used_bytes)} / ${formatBytes(system.memory.total_bytes)}` : "-";
  const diskUsed = system.disk ? `${formatBytes(system.disk.used_bytes)} / ${formatBytes(system.disk.total_bytes)}` : "-";
  const rxRate = formatBytes(system.network?.rx_rate_bytes_per_second);
  const txRate = formatBytes(system.network?.tx_rate_bytes_per_second);

  return (
    <div className={`node-load-metrics ${variant}`}>
      <MetricRow label="CPU" value={formatPercent(system.cpu?.percent)} percent={system.cpu?.percent} icon={<Cpu size={13} />} />
      <MetricRow
        label="内存"
        value={variant === "detail" ? memoryUsed : formatPercent(system.memory?.used_percent)}
        percent={system.memory?.used_percent}
        icon={<MemoryStick size={13} />}
      />
      <MetricRow
        label="磁盘"
        value={variant === "detail" ? diskUsed : formatPercent(system.disk?.used_percent)}
        percent={system.disk?.used_percent}
        icon={<HardDrive size={13} />}
      />
      {variant === "detail" ? (
        <MetricRow label="网络速率" value={`${rxRate}/s ↓  ${txRate}/s ↑`} icon={<Network size={13} />} />
      ) : null}
    </div>
  );
}
