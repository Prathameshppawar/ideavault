"use client";

import { useEffect, useRef, useState } from "react";
import { format } from "date-fns";
import { plural } from "@/lib/format";
import { EVENT_CATEGORIES } from "../lib/activity";
import type { HeatCell, Heatmap } from "../lib/timeline";

const GAP = 3;
const LEFT = 30;
const TOP = 18;

/** Sequential single-hue ramp (accent), mixed toward the empty-cell colour so it flips correctly in dark mode. */
export const HEAT_FILLS = [
  "var(--surface-3)",
  "color-mix(in srgb, var(--accent) 30%, var(--surface-3))",
  "color-mix(in srgb, var(--accent) 52%, var(--surface-3))",
  "color-mix(in srgb, var(--accent) 76%, var(--surface-3))",
  "var(--accent)",
] as const;

/** Contribution-style calendar of daily activity (weeks as columns, Monday first). */
export function ActivityHeatmap({ heatmap, label, cell = 12 }: { heatmap: Heatmap; label: string; cell?: number }) {
  const [hover, setHover] = useState<{ cell: HeatCell; col: number; row: number; scroll: number } | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const pitch = cell + GAP;
  const width = LEFT + heatmap.weeks.length * pitch;
  const height = TOP + 7 * pitch;

  // Keep the most recent weeks in view when the grid overflows (small screens).
  useEffect(() => {
    const el = scroller.current;
    if (el) el.scrollLeft = el.scrollWidth;
  }, [width]);

  return (
    <div className="relative max-w-full">
      <div ref={scroller} className="overflow-x-auto pb-1">
        <svg width={width} height={height} role="img" aria-label={label} className="block" onMouseLeave={() => setHover(null)}>
          {heatmap.months.map((m) => (
            <text key={`${m.col}-${m.label}`} x={LEFT + m.col * pitch} y={11} fontSize={11} style={{ fill: "var(--text-faint)" }}>
              {m.label}
            </text>
          ))}
          {["Mon", "Wed", "Fri"].map((d, i) => (
            <text key={d} x={0} y={TOP + i * 2 * pitch + cell / 2 + 4} fontSize={10} style={{ fill: "var(--text-faint)" }}>
              {d}
            </text>
          ))}
          {heatmap.weeks.map((week, col) =>
            week.map((c, row) =>
              c.outside ? null : (
                <rect
                  key={c.key}
                  x={LEFT + col * pitch}
                  y={TOP + row * pitch}
                  width={cell}
                  height={cell}
                  rx={Math.min(4, cell / 4)}
                  style={{
                    fill: HEAT_FILLS[c.level],
                    stroke: hover?.cell.key === c.key ? "var(--text)" : "transparent",
                    strokeWidth: 1.5,
                  }}
                  onMouseEnter={() => setHover({ cell: c, col, row, scroll: scroller.current?.scrollLeft ?? 0 })}
                >
                  <title>{`${format(c.date, "EEE d MMM yyyy")}: ${plural(c.count, "change")}`}</title>
                </rect>
              ),
            ),
          )}
        </svg>
      </div>
      {hover && (
        <div
          role="tooltip"
          className="pointer-events-none absolute z-10 w-max min-w-[11rem] -translate-x-1/2 -translate-y-full rounded-[var(--radius-md)] border border-border bg-surface px-3 py-2 text-[12px] shadow-md"
          style={{ left: Math.min(Math.max(LEFT + hover.col * pitch + cell / 2 - hover.scroll, 90), width - 90), top: TOP + hover.row * pitch - 6 }}
        >
          <p className="font-semibold tabular-nums text-fg">{plural(hover.cell.count, "change")}</p>
          <p className="text-muted">{format(hover.cell.date, "EEEE d MMM yyyy")}</p>
          {hover.cell.totals && hover.cell.count > 0 && (
            <ul className="mt-1.5 space-y-0.5 border-t border-border pt-1.5">
              {[...EVENT_CATEGORIES].reverse().map((c) =>
                hover.cell.totals!.byCategory[c.key] > 0 ? (
                  <li key={c.key} className="flex items-center gap-2 text-muted">
                    <span className="h-0.5 w-3 rounded-full" style={{ background: `var(--k-${c.tone})` }} aria-hidden />
                    <span className="flex-1">{c.label}</span>
                    <span className="font-medium tabular-nums text-fg">{hover.cell.totals!.byCategory[c.key]}</span>
                  </li>
                ) : null,
              )}
            </ul>
          )}
        </div>
      )}
      <div className="mt-2 flex items-center gap-1.5 pl-[30px] text-[11px] text-faint" aria-hidden>
        <span>Less</span>
        {HEAT_FILLS.map((f) => (
          <span key={f} className="h-2.5 w-2.5 rounded-[3px]" style={{ background: f }} />
        ))}
        <span>More</span>
      </div>
    </div>
  );
}
