"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Activity, TrendingDown, TrendingUp, Minus } from "lucide-react";
import { api } from "@/lib/api";
import type { IdeaMomentum } from "@/lib/types";
import { cn, plural, timeAgo } from "@/lib/format";
import { StatusBadge } from "@/components/ui/badges";
import { EmptyState, ErrorState } from "@/components/ui/primitives";
import { pickInt, useParamSetter } from "../lib/url";
import { RowsSkeleton, Segmented } from "../ui";

const WINDOWS = [7, 14, 30] as const;
type Win = (typeof WINDOWS)[number];
const WIN_LABEL: Record<Win, { now: string; before: string; option: string }> = {
  7: { now: "this week", before: "the week before", option: "7 days" },
  14: { now: "these two weeks", before: "the two weeks before", option: "14 days" },
  30: { now: "this month", before: "the month before", option: "30 days" },
};

/** Direction and wording for recent vs previous window. */
export function velocity(recent: number, previous: number): { dir: "up" | "down" | "flat" | "new"; text: string } {
  if (previous === 0 && recent > 0) return { dir: "new", text: "newly active" };
  if (recent === previous) return { dir: "flat", text: "steady" };
  const pct = Math.round((Math.abs(recent - previous) / Math.max(previous, 1)) * 100);
  return recent > previous ? { dir: "up", text: `up ${pct}%` } : { dir: "down", text: `down ${pct}%` };
}

export function MomentumView() {
  const params = useSearchParams();
  const setParams = useParamSetter();
  const days = pickInt(params.get("days"), WINDOWS, 7);
  const { data, isLoading, error, refetch, isPlaceholderData } = useQuery({
    queryKey: ["dashboard", "momentum", days],
    queryFn: () => api.get<IdeaMomentum[]>("/v1/dashboard/momentum", { query: { days } }),
    placeholderData: keepPreviousData,
  });
  const label = WIN_LABEL[days];

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Segmented<`${Win}`>
          label="Window"
          value={`${days}`}
          onChange={(v) => setParams({ days: v === "7" ? null : v })}
          options={WINDOWS.map((w) => ({ value: `${w}` as `${Win}`, label: WIN_LABEL[w].option }))}
        />
        <div className="flex items-center gap-4 text-[12px] text-muted" aria-hidden>
          <span className="flex items-center gap-1.5">
            <span className="h-1.5 w-4 rounded-full bg-accent" /> {label.now}
          </span>
          <span className="flex items-center gap-1.5">
            <span className="h-1.5 w-4 rounded-full bg-border-strong" /> {label.before}
          </span>
        </div>
      </div>

      {isLoading ? (
        <RowsSkeleton rows={5} />
      ) : error || !data ? (
        <ErrorState error={error} onRetry={() => void refetch()} />
      ) : data.length === 0 ? (
        <EmptyState icon={Activity} title="Everything is still" description={`No idea changed in the last ${days} days. Momentum appears as soon as you pick one back up.`} />
      ) : (
        <MomentumList rows={data} days={days} dim={isPlaceholderData} />
      )}
    </div>
  );
}

function MomentumList({ rows, days, dim }: { rows: IdeaMomentum[]; days: Win; dim: boolean }) {
  const max = Math.max(1, ...rows.map((r) => Math.max(r.recent, r.previous)));
  const label = WIN_LABEL[days];
  const top = rows[0];
  return (
    <div className={cn("transition-opacity", dim && "opacity-60")}>
      <p className="mb-6 max-w-2xl font-display text-[1.45rem] leading-snug text-fg">
        {plural(rows.length, "idea")} moved {label.now}.{" "}
        {top && (
          <span className="text-muted">
            {top.title} is changing fastest, with {plural(top.recent, "change")}.
          </span>
        )}
      </p>
      <ol className="divide-y divide-border border-y border-border">
        {rows.map((r, i) => {
          const v = velocity(r.recent, r.previous);
          const Icon = v.dir === "down" ? TrendingDown : v.dir === "flat" ? Minus : TrendingUp;
          const extras = [
            r.decisions ? plural(r.decisions, "decision") : null,
            r.reversals ? plural(r.reversals, "reversal") : null,
            r.branches ? plural(r.branches, "new branch", "new branches") : null,
          ].filter(Boolean);
          return (
            <li key={r.idea_id} className="group relative grid grid-cols-[1.75rem_minmax(0,1fr)] gap-x-3 py-4 sm:grid-cols-[2rem_minmax(0,1fr)_minmax(0,16rem)] sm:gap-x-6">
              <span className="pt-1 text-right font-mono text-[12px] tabular-nums text-faint">{i + 1}</span>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <Link
                    href={`/ideas/${r.idea_id}`}
                    className="font-display text-[1.3rem] leading-tight text-fg after:absolute after:inset-0 after:content-[''] group-hover:text-accent"
                  >
                    {r.title}
                  </Link>
                  <StatusBadge status={r.status} />
                </div>
                <p className="mt-1 text-[12px] text-faint">
                  Last touched {timeAgo(r.last_at)}
                  {extras.length > 0 && <> · {extras.join(" · ")}</>}
                </p>
              </div>
              <div className="col-span-2 mt-3 min-w-0 sm:col-span-1 sm:col-start-3 sm:mt-0">
                <div className="flex items-baseline justify-between gap-2 text-[12px]">
                  <span className="font-medium tabular-nums text-fg">{plural(r.recent, "change")}</span>
                  <span className={cn("inline-flex items-center gap-1", v.dir === "down" ? "text-muted" : v.dir === "flat" ? "text-faint" : "text-fg")}>
                    <Icon className="h-3 w-3" aria-hidden /> {v.text}
                  </span>
                </div>
                <div className="mt-1.5 space-y-1" aria-label={`${r.recent} changes ${label.now}, ${r.previous} ${label.before}`} role="img">
                  <div className="h-1.5 rounded-full bg-surface-3">
                    <div className="h-full rounded-full bg-accent" style={{ width: `${(r.recent / max) * 100}%`, minWidth: r.recent ? 4 : 0 }} />
                  </div>
                  <div className="h-1.5 rounded-full bg-surface-3">
                    <div className="h-full rounded-full bg-border-strong" style={{ width: `${(r.previous / max) * 100}%`, minWidth: r.previous ? 4 : 0 }} />
                  </div>
                </div>
                <p className="mt-1 text-right text-[11px] tabular-nums text-faint">
                  {r.previous} {label.before}
                </p>
              </div>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
