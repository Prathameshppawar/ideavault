"use client";

import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis, type TooltipContentProps } from "recharts";
import { format } from "date-fns";
import { compactNumber, plural } from "@/lib/format";
import { EVENT_CATEGORIES, type EventCategory } from "../lib/activity";
import { useCssVars } from "../lib/hooks";
import { keyToDate, type SeriesRow } from "../lib/timeline";

const VARS = ["--k-evidence", "--k-idea", "--k-decision", "--k-artifact", "--border", "--text-faint", "--surface", "--surface-2"] as const;

/** Rounded data-end only on the topmost non-zero segment of each stack; square at the baseline. */
function segmentPath(x: number, y: number, w: number, h: number, r: number): string {
  const rr = Math.max(0, Math.min(r, w / 2, h));
  return `M${x},${y + h} L${x},${y + rr} Q${x},${y} ${x + rr},${y} L${x + w - rr},${y} Q${x + w},${y} ${x + w},${y + rr} L${x + w},${y + h} Z`;
}

function makeShape(cat: EventCategory, fill: string) {
  const idx = EVENT_CATEGORIES.findIndex((c) => c.key === cat);
  const above = EVENT_CATEGORIES.slice(idx + 1).map((c) => c.key);
  return function renderSegment(props: { x?: number; y?: number; width?: number; height?: number; payload?: SeriesRow }) {
    const { x = 0, y = 0, width = 0, height = 0, payload } = props;
    if (!payload || height <= 0 || width <= 0) return null;
    const top = above.every((k) => !payload[k]);
    // 2px surface gap between stacked segments: trim 1px from each touching edge.
    const h = Math.max(0, height - (idx > 0 ? 1 : 0) - (top ? 0 : 1));
    const yy = y + (top ? 0 : 1);
    return <path d={top ? segmentPath(x, yy, width, h, 3) : `M${x},${yy}h${width}v${h}h${-width}Z`} fill={fill} />;
  };
}

function ChartTooltip({ active, payload }: Partial<TooltipContentProps<number, string>>) {
  if (!active || !payload?.length) return null;
  const row = payload[0]?.payload as SeriesRow | undefined;
  if (!row) return null;
  return (
    <div className="min-w-[12rem] rounded-[var(--radius-md)] border border-border bg-surface px-3 py-2 text-[12px] shadow-md">
      <p className="font-semibold tabular-nums text-fg">{plural(row.total, "event")}</p>
      <p className="text-muted">{row.label}</p>
      {row.total > 0 && (
        <ul className="mt-1.5 space-y-0.5 border-t border-border pt-1.5">
          {[...EVENT_CATEGORIES].reverse().map((c) =>
            row[c.key] > 0 ? (
              <li key={c.key} className="flex items-center gap-2 text-muted">
                <span className="h-0.5 w-3 rounded-full" style={{ background: `var(--k-${c.tone})` }} aria-hidden />
                <span className="flex-1">{c.label}</span>
                <span className="font-medium tabular-nums text-fg">{row[c.key]}</span>
              </li>
            ) : null,
          )}
        </ul>
      )}
    </div>
  );
}

/** Stacked columns of activity by category (per day, or per week for long ranges). */
export function ActivityChart({ rows, unit }: { rows: SeriesRow[]; unit: "day" | "week" }) {
  const c = useCssVars(VARS);
  const fills: Record<EventCategory, string> = {
    knowledge: c["--k-evidence"],
    ideas: c["--k-idea"],
    decisions: c["--k-decision"],
    structure: c["--k-artifact"],
  };
  if (!c["--surface"]) return <div className="h-[232px]" />;
  const tickFmt = (k: string) => format(keyToDate(k), "d MMM");
  return (
    <div className="h-[232px] w-full" role="img" aria-label={`Activity per ${unit}, stacked by kind of change`}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={rows} margin={{ top: 8, right: 4, bottom: 0, left: -18 }} barCategoryGap={rows.length > 60 ? 1 : "18%"} maxBarSize={24}>
          <CartesianGrid vertical={false} stroke={c["--border"]} strokeWidth={1} />
          <XAxis
            dataKey="key"
            tickFormatter={tickFmt}
            tick={{ fontSize: 11, fill: c["--text-faint"] }}
            tickLine={false}
            axisLine={{ stroke: c["--border"] }}
            minTickGap={28}
            interval="preserveStartEnd"
          />
          <YAxis allowDecimals={false} tickFormatter={(v: number) => compactNumber(v)} tick={{ fontSize: 11, fill: c["--text-faint"] }} tickLine={false} axisLine={false} width={44} />
          <Tooltip cursor={{ fill: c["--surface-2"] }} content={<ChartTooltip />} isAnimationActive={false} />
          {EVENT_CATEGORIES.map((cat) => (
            <Bar key={cat.key} dataKey={cat.key} name={cat.label} stackId="a" fill={fills[cat.key]} shape={makeShape(cat.key, fills[cat.key])} isAnimationActive={false} />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
