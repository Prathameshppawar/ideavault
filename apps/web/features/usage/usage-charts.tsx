"use client";

import type { ReactNode } from "react";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis, type TooltipContentProps } from "recharts";
import { compactNumber } from "@/lib/format";
import { humanTask, providerLabel, providerToneVar } from "@/features/models/model-meta";
import { CHART_BASE_VARS, useCssVars } from "./chart-theme";
import { formatMs, formatUsd, type DailyRow } from "./usage-data";

type Base = Record<(typeof CHART_BASE_VARS)[number], string>;

function useBase(): Base {
  return useCssVars(CHART_BASE_VARS);
}

export function ChartFrame({ title, subtitle, legend, children, empty }: { title: string; subtitle?: ReactNode; legend?: ReactNode; children: ReactNode; empty?: boolean }) {
  return (
    <figure className="min-w-0">
      <figcaption className="mb-3 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <span>
          <span className="block text-[14px] font-medium text-fg">{title}</span>
          {subtitle && <span className="block text-[12px] text-faint">{subtitle}</span>}
        </span>
        {legend}
      </figcaption>
      {empty ? <div className="flex h-[220px] items-center justify-center rounded-[var(--radius-md)] border border-dashed border-border text-[13px] text-faint">No data in this range</div> : children}
    </figure>
  );
}

export function Legend({ items }: { items: { key: string; label: string; color: string }[] }) {
  return (
    <ul className="flex flex-wrap gap-x-3 gap-y-1 text-[12px] text-muted" aria-label="Legend">
      {items.map((i) => (
        <li key={i.key} className="inline-flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-[2px]" style={{ background: i.color }} aria-hidden />
          {i.label}
        </li>
      ))}
    </ul>
  );
}

function Tip({ title, rows, footer }: { title: ReactNode; rows: { key: string; label: string; color: string; value: string }[]; footer?: ReactNode }) {
  return (
    <div className="min-w-[160px] rounded-[var(--radius-md)] border border-border bg-surface px-3 py-2 text-[12px] shadow-md">
      <div className="mb-1 font-medium text-fg">{title}</div>
      {rows.map((r) => (
        <div key={r.key} className="flex items-center justify-between gap-4 py-px">
          <span className="inline-flex items-center gap-1.5 text-muted">
            <span className="h-2 w-2 rounded-[2px]" style={{ background: r.color }} aria-hidden />
            {r.label}
          </span>
          <span className="tabular-nums text-fg">{r.value}</span>
        </div>
      ))}
      {footer && <div className="mt-1 border-t border-border pt-1 text-faint">{footer}</div>}
    </div>
  );
}

const axisTick = (b: Base) => ({ fill: b["--text-faint"], fontSize: 11 });

export function TokensPerDayChart({ rows, providers }: { rows: DailyRow[]; providers: string[] }) {
  const b = useBase();
  const colors = useCssVars(providers.map(providerToneVar));
  const color = (p: string) => colors[providerToneVar(p)];
  return (
    <ResponsiveContainer width="100%" height={240}>
      <BarChart data={rows} margin={{ top: 4, right: 4, bottom: 0, left: 0 }} barCategoryGap="18%">
        <CartesianGrid vertical={false} stroke={b["--border"]} strokeWidth={1} />
        <XAxis dataKey="label" tick={axisTick(b)} tickLine={false} axisLine={{ stroke: b["--border"] }} interval="preserveStartEnd" minTickGap={20} />
        <YAxis tick={axisTick(b)} tickLine={false} axisLine={false} width={44} tickFormatter={(v: number) => compactNumber(v)} allowDecimals={false} />
        <Tooltip
          cursor={{ fill: b["--surface-2"] }}
          content={(p: TooltipContentProps) =>
            p.active && p.payload?.length ? (
              <Tip
                title={String(p.label)}
                rows={[...p.payload].reverse().map((e) => ({ key: String(e.dataKey), label: providerLabel(String(e.dataKey)), color: color(String(e.dataKey)), value: Number(e.value ?? 0).toLocaleString("en") }))}
                footer={`${p.payload.reduce((s, e) => s + (Number(e.value) || 0), 0).toLocaleString("en")} tokens`}
              />
            ) : null
          }
        />
        {providers.map((p, i) => (
          <Bar
            key={p}
            dataKey={p}
            stackId="tokens"
            fill={color(p)}
            stroke={b["--surface"]}
            strokeWidth={1}
            maxBarSize={24}
            radius={i === providers.length - 1 ? [4, 4, 0, 0] : 0}
            isAnimationActive={false}
          />
        ))}
      </BarChart>
    </ResponsiveContainer>
  );
}

export function CostPerDayChart({ rows }: { rows: DailyRow[] }) {
  const b = useBase();
  return (
    <ResponsiveContainer width="100%" height={240}>
      <BarChart data={rows} margin={{ top: 4, right: 4, bottom: 0, left: 0 }} barCategoryGap="18%">
        <CartesianGrid vertical={false} stroke={b["--border"]} />
        <XAxis dataKey="label" tick={axisTick(b)} tickLine={false} axisLine={{ stroke: b["--border"] }} interval="preserveStartEnd" minTickGap={20} />
        <YAxis tick={axisTick(b)} tickLine={false} axisLine={false} width={58} tickFormatter={(v: number) => (v === 0 ? "$0" : v < 0.01 ? `$${+v.toFixed(4)}` : `$${v.toFixed(2)}`)} />
        <Tooltip
          cursor={{ fill: b["--surface-2"] }}
          content={(p: TooltipContentProps) =>
            p.active && p.payload?.length ? (
              <Tip title={String(p.label)} rows={[{ key: "cost", label: "Estimated cost", color: b["--k-checkpoint"], value: formatUsd(Number(p.payload[0].value) || 0) }]} />
            ) : null
          }
        />
        <Bar dataKey="cost" fill={b["--k-checkpoint"]} maxBarSize={24} radius={[4, 4, 0, 0]} isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  );
}

export function LatencyChart({ rows }: { rows: { key: string; label: string; avg: number; p95: number; calls: number }[] }) {
  const b = useBase();
  const height = Math.max(120, rows.length * 44 + 28);
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={rows} layout="vertical" margin={{ top: 0, right: 12, bottom: 0, left: 0 }} barGap={2} barCategoryGap="24%">
        <CartesianGrid horizontal={false} stroke={b["--border"]} />
        <XAxis type="number" tick={axisTick(b)} tickLine={false} axisLine={{ stroke: b["--border"] }} tickFormatter={(v: number) => (v === 0 ? "0" : formatMs(v))} />
        <YAxis type="category" dataKey="label" tick={{ ...axisTick(b), fill: b["--text-muted"] }} tickLine={false} axisLine={false} width={150} tickFormatter={(v: string) => (v.length > 22 ? `${v.slice(0, 21)}…` : v)} />
        <Tooltip
          cursor={{ fill: b["--surface-2"] }}
          content={(p: TooltipContentProps) => {
            if (!p.active || !p.payload?.length) return null;
            const row = p.payload[0].payload as { key: string; avg: number; p95: number; calls: number };
            return (
              <Tip
                title={row.key}
                rows={[
                  { key: "avg", label: "Average", color: b["--k-checkpoint"], value: formatMs(row.avg) },
                  { key: "p95", label: "95th percentile", color: b["--text-faint"], value: formatMs(row.p95) },
                ]}
                footer={`${row.calls.toLocaleString("en")} calls`}
              />
            );
          }}
        />
        <Bar dataKey="avg" fill={b["--k-checkpoint"]} maxBarSize={12} radius={[0, 4, 4, 0]} isAnimationActive={false} />
        <Bar dataKey="p95" fill={b["--text-faint"]} maxBarSize={12} radius={[0, 4, 4, 0]} isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  );
}

export function TaskCallsChart({ rows }: { rows: { key: string; ok: number; failed: number; calls: number }[] }) {
  const b = useBase();
  const height = Math.max(120, rows.length * 32 + 28);
  const data = rows.map((r) => ({ ...r, label: humanTask(r.key) }));
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={data} layout="vertical" margin={{ top: 0, right: 12, bottom: 0, left: 0 }} barCategoryGap="28%">
        <CartesianGrid horizontal={false} stroke={b["--border"]} />
        <XAxis type="number" tick={axisTick(b)} tickLine={false} axisLine={{ stroke: b["--border"] }} allowDecimals={false} />
        <YAxis type="category" dataKey="label" tick={{ ...axisTick(b), fill: b["--text-muted"] }} tickLine={false} axisLine={false} width={130} />
        <Tooltip
          cursor={{ fill: b["--surface-2"] }}
          content={(p: TooltipContentProps) => {
            if (!p.active || !p.payload?.length) return null;
            const row = p.payload[0].payload as { label: string; ok: number; failed: number; calls: number };
            return (
              <Tip
                title={row.label}
                rows={[
                  { key: "ok", label: "Succeeded", color: b["--k-checkpoint"], value: row.ok.toLocaleString("en") },
                  { key: "failed", label: "Failed", color: b["--danger"], value: row.failed.toLocaleString("en") },
                ]}
                footer={`${row.calls.toLocaleString("en")} calls`}
              />
            );
          }}
        />
        <Bar dataKey="ok" stackId="calls" fill={b["--k-checkpoint"]} stroke={b["--surface"]} strokeWidth={1} maxBarSize={14} isAnimationActive={false} />
        <Bar dataKey="failed" stackId="calls" fill={b["--danger"]} stroke={b["--surface"]} strokeWidth={1} maxBarSize={14} radius={[0, 4, 4, 0]} isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  );
}
