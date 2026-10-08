// Pure transforms for usage analytics (unit-tested).
import { addDays, format, parseISO, startOfDay } from "date-fns";
import type { UsageGroup } from "@/lib/types";
import { compactNumber, plural } from "@/lib/format";

export type DailyRow = { day: string; label: string } & Record<string, number | string>;

/** Calendar days (yyyy-MM-dd, local time) from `since` through `today`, inclusive. */
export function dayKeys(since: string | Date, today: Date = new Date()): string[] {
  const start = startOfDay(typeof since === "string" ? parseISO(since) : since);
  const end = startOfDay(today);
  const out: string[] = [];
  for (let d = start; d <= end && out.length < 400; d = addDays(d, 1)) out.push(format(d, "yyyy-MM-dd"));
  return out;
}

function label(day: string): string {
  return format(parseISO(day), "d MMM");
}

/**
 * Dense daily series stacked by series key (e.g. provider): one row per day, a numeric
 * column per series (0 when there was no usage). Days outside `keys` are ignored.
 */
export function buildDailySeries(keys: string[], bySeries: Record<string, UsageGroup[] | undefined>, metric: (g: UsageGroup) => number): DailyRow[] {
  const series = Object.keys(bySeries);
  const index = new Map<string, DailyRow>(keys.map((day) => [day, { day, label: label(day), ...Object.fromEntries(series.map((s) => [s, 0])) } as DailyRow]));
  for (const s of series) {
    for (const g of bySeries[s] ?? []) {
      const row = index.get(g.key);
      if (row) row[s] = (row[s] as number) + metric(g);
    }
  }
  return keys.map((k) => index.get(k)!);
}

/** True when every value of every series is zero. */
export function isEmptySeries(rows: DailyRow[], series: string[]): boolean {
  return rows.every((r) => series.every((s) => !r[s]));
}

/** Share of calls whose token counts were estimated (0–1). */
export function estimatedShare(g: Pick<UsageGroup, "calls" | "estimated">): number {
  return g.calls > 0 ? g.estimated / g.calls : 0;
}

export function formatUsd(n: number): string {
  if (!n) return "$0.00";
  if (n < 0.01) return `$${n.toFixed(4)}`;
  if (n < 100) return `$${n.toFixed(2)}`;
  return `$${Math.round(n).toLocaleString("en")}`;
}

export function formatMs(ms: number): string {
  if (!ms) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
}

/** Headline parts: "1,284 model calls · 2.1M tokens · ~$0.84 estimated · 12 failures". */
export function headlineParts(t: UsageGroup): string[] {
  return [
    `${t.calls.toLocaleString("en")} model ${t.calls === 1 ? "call" : "calls"}`,
    `${compactNumber(t.total_tokens)} tokens`,
    `~${formatUsd(t.cost_usd)} estimated`,
    plural(t.failures, "failure"),
  ];
}

/** Latency rows per model, slowest p95 first, with display labels. */
export function latencyRows(byModel: UsageGroup[], max = 10) {
  return [...byModel]
    .filter((g) => g.calls > 0)
    .sort((a, b) => b.p95_latency_ms - a.p95_latency_ms || b.avg_latency_ms - a.avg_latency_ms)
    .slice(0, max)
    .map((g) => ({ key: g.key, label: g.key.slice(g.key.indexOf("/") + 1), avg: Math.round(g.avg_latency_ms), p95: Math.round(g.p95_latency_ms), calls: g.calls }));
}

/** Calls per task split into succeeded / failed, most calls first. */
export function taskRows(byTask: UsageGroup[]) {
  return [...byTask]
    .sort((a, b) => b.calls - a.calls)
    .map((g) => ({ key: g.key, ok: g.calls - g.failures, failed: g.failures, calls: g.calls }));
}
