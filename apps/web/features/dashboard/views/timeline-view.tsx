"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { format } from "date-fns";
import { CalendarRange, Table2 } from "lucide-react";
import { api } from "@/lib/api";
import type { ActivityEvent, TimelineView as TimelineData } from "@/lib/types";
import { entityHref } from "@/lib/entities";
import { cn, plural } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/primitives";
import { ActivityChart } from "../components/activity-chart";
import { eventVisual } from "../components/activity-feed";
import { ActivityHeatmap } from "../components/heatmap";
import { EVENT_CATEGORIES, groupByMonth } from "../lib/activity";
import { useNow } from "../lib/hooks";
import { buildHeatmap, buildSeries, coversRange, keyToDate, localBuckets } from "../lib/timeline";
import { pickInt, useParamSetter } from "../lib/url";
import { Heading, IdeaLink, QuietEmpty, Segmented, ToggleChip } from "../ui";

const RANGES = [30, 90, 180, 365] as const;
const ACTIVITY_LIMIT = 5000;
type Range = (typeof RANGES)[number];
const RANGE_LABEL: Record<Range, string> = { 30: "30 days", 90: "90 days", 180: "6 months", 365: "1 year" };
/** For sentences: "in the last …". */
const RANGE_PHRASE: Record<Range, string> = { 30: "30 days", 90: "90 days", 180: "six months", 365: "year" };

/** Milestone families for the filter row. */
const MILESTONE_KINDS: { key: string; label: string; tone: string; match: (t: string) => boolean }[] = [
  { key: "ideas", label: "New ideas", tone: "idea", match: (t) => t === "idea.created" },
  { key: "decisions", label: "Decisions", tone: "decision", match: (t) => t.startsWith("decision.") },
  { key: "checkpoints", label: "Checkpoints", tone: "checkpoint", match: (t) => t === "checkpoint.created" },
  { key: "branches", label: "Branches & merges", tone: "branch", match: (t) => t === "branch.created" || t === "delta.merged" },
  { key: "conclusions", label: "Conclusions", tone: "action", match: (t) => t === "idea.concluded" || t === "idea.status_changed" },
  { key: "artifacts", label: "Artifacts", tone: "artifact", match: (t) => t === "artifact.created" },
  { key: "imports", label: "Imports", tone: "conversation", match: (t) => t === "conversation.imported" },
];

function milestoneTag(t: string): string | null {
  if (t === "decision.reversed") return "Reversed";
  if (t === "decision.superseded") return "Superseded";
  if (t === "idea.concluded") return "Concluded";
  return null;
}

export function TimelineView() {
  const params = useSearchParams();
  const setParams = useParamSetter();
  const days = pickInt(params.get("days"), RANGES, 90);
  const now = useNow();
  const { data, isLoading, error, refetch, isFetching, isPlaceholderData } = useQuery({
    queryKey: ["dashboard", "timeline", days],
    queryFn: () => api.get<TimelineData>("/v1/dashboard/timeline", { query: { days } }),
    placeholderData: keepPreviousData,
  });
  const [table, setTable] = useState(false);
  // Raw events let us bucket by the viewer's local day (the API buckets by UTC day).
  const { data: events } = useQuery({
    queryKey: ["activity", "timeline", ACTIVITY_LIMIT],
    queryFn: () => api.get<ActivityEvent[]>("/v1/activity", { query: { limit: ACTIVITY_LIMIT } }),
    staleTime: 30_000,
  });
  const localExact = useMemo(() => !!events && !!now && coversRange(events, days, now), [events, days, now]);
  const buckets = useMemo(() => (localExact && events ? localBuckets(events) : (data?.buckets ?? [])), [localExact, events, data]);

  const heat = useMemo(() => (data && now ? buildHeatmap(buckets, days, now) : null), [data, buckets, days, now]);
  const series = useMemo(() => (data && now ? buildSeries(buckets, days, now) : null), [data, buckets, days, now]);

  return (
    <div className="space-y-10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Segmented<`${Range}`>
          label="Time range"
          value={`${days}`}
          onChange={(v) => setParams({ days: v === "90" ? null : v })}
          options={RANGES.map((r) => ({ value: `${r}` as `${Range}`, label: RANGE_LABEL[r] }))}
        />
        {isFetching && !isLoading && <span className="text-[12px] text-faint">Updating…</span>}
      </div>

      {isLoading || !now ? (
        <TimelineSkeleton />
      ) : error || !data || !heat || !series ? (
        <ErrorState error={error} onRetry={() => void refetch()} />
      ) : (
        <div className={cn("space-y-12 transition-opacity", isPlaceholderData && "opacity-60")}>
          <section aria-labelledby="rhythm">
            <Heading id="rhythm">Rhythm</Heading>
            <div className={cn("flex flex-wrap gap-x-12 gap-y-6", days === 365 ? "flex-col-reverse" : "items-center")}>
              <ActivityHeatmap
                heatmap={heat}
                cell={days === 365 ? 14 : 18}
                label={`Daily activity over the last ${RANGE_PHRASE[days]}: ${heat.total} changes on ${heat.activeDays} days`}
              />
              <div className="min-w-[16rem] max-w-md flex-1">
                {heat.total === 0 ? (
                  <p className="font-display text-[1.45rem] leading-snug text-muted">No recorded thinking in the last {RANGE_PHRASE[days]}.</p>
                ) : (
                  <p className="font-display text-[1.45rem] leading-snug text-fg">
                    {plural(heat.total, "change")} across {plural(heat.activeDays, "day")}{" "}
                    <span className="text-muted">in the last {RANGE_PHRASE[days]}.</span>
                  </p>
                )}
                {heat.busiest && (
                  <p className="mt-2 text-[14px] text-muted">
                    Busiest day: <span className="text-fg">{format(keyToDate(heat.busiest.key), "EEEE d MMM")}</span>, with {plural(heat.busiest.count, "change")}.
                  </p>
                )}
                {!localExact && <p className="mt-2 text-[12px] text-faint">Days are counted in UTC for very large vaults.</p>}
              </div>
            </div>
          </section>

          <section aria-labelledby="composition">
            <Heading
              id="composition"
              action={
                <Button size="xs" variant="ghost" onClick={() => setTable((t) => !t)} aria-pressed={table}>
                  <Table2 className="h-3.5 w-3.5" aria-hidden /> {table ? "Show chart" : "View as table"}
                </Button>
              }
            >
              What kind of thinking
            </Heading>
            <ul className="mb-4 flex flex-wrap gap-x-5 gap-y-1.5" aria-label="Legend">
              {[...EVENT_CATEGORIES].reverse().map((c) => {
                const n = series.rows.reduce((s, r) => s + r[c.key], 0);
                return (
                  <li key={c.key} className="flex items-center gap-2 text-[12px] text-muted">
                    <span className="h-2.5 w-2.5 rounded-[3px]" style={{ background: `var(--k-${c.tone})` }} aria-hidden />
                    {c.label}
                    <span className="tabular-nums text-faint">{n}</span>
                  </li>
                );
              })}
            </ul>
            {heat.total === 0 ? (
              <QuietEmpty>Nothing recorded in this range — the chart fills in as you think.</QuietEmpty>
            ) : table ? (
              <ActivityTable rows={series.rows} unit={series.unit} />
            ) : (
              <ActivityChart rows={series.rows} unit={series.unit} />
            )}
          </section>

          <Milestones events={data.milestones ?? []} days={days} />
        </div>
      )}
    </div>
  );
}

function ActivityTable({ rows, unit }: { rows: ReturnType<typeof buildSeries>["rows"]; unit: "day" | "week" }) {
  const active = rows.filter((r) => r.total > 0).reverse();
  if (!active.length) return <p className="text-[13px] text-faint">No activity in this range.</p>;
  return (
    <div className="max-h-[320px] overflow-auto rounded-[var(--radius-md)] border border-border">
      <table className="w-full text-[13px]">
        <thead className="sticky top-0 bg-surface-2 text-left text-[12px] text-muted">
          <tr>
            <th className="px-3 py-2 font-medium">{unit === "week" ? "Week" : "Day"}</th>
            {[...EVENT_CATEGORIES].reverse().map((c) => (
              <th key={c.key} className="px-3 py-2 text-right font-medium">
                {c.label}
              </th>
            ))}
            <th className="px-3 py-2 text-right font-medium">Total</th>
          </tr>
        </thead>
        <tbody>
          {active.map((r) => (
            <tr key={r.key} className="border-t border-border">
              <td className="px-3 py-1.5 text-fg">{r.label}</td>
              {[...EVENT_CATEGORIES].reverse().map((c) => (
                <td key={c.key} className="px-3 py-1.5 text-right tabular-nums text-muted">
                  {r[c.key] || "–"}
                </td>
              ))}
              <td className="px-3 py-1.5 text-right font-medium tabular-nums text-fg">{r.total}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Milestones({ events, days }: { events: ActivityEvent[]; days: Range }) {
  const [kind, setKind] = useState<string | null>(null);
  const present = MILESTONE_KINDS.map((k) => ({ ...k, count: events.filter((e) => k.match(e.event_type)).length })).filter((k) => k.count > 0);
  const filtered = kind ? events.filter((e) => MILESTONE_KINDS.find((k) => k.key === kind)?.match(e.event_type)) : events;
  const months = groupByMonth(filtered, (e) => e.created_at);
  return (
    <section aria-labelledby="milestones">
      <Heading id="milestones" count={events.length}>
        Milestones
      </Heading>
      {events.length === 0 ? (
        <EmptyState icon={CalendarRange} title="No milestones yet" description={`Ideas, checkpoints, decisions and branches from the last ${RANGE_PHRASE[days]} will appear here.`} />
      ) : (
        <>
          {present.length > 1 && (
            <div className="-mx-1 mb-6 flex gap-1.5 overflow-x-auto px-1 pb-1" role="group" aria-label="Filter milestones">
              <ToggleChip pressed={kind === null} onClick={() => setKind(null)}>
                All
              </ToggleChip>
              {present.map((k) => (
                <ToggleChip key={k.key} pressed={kind === k.key} onClick={() => setKind(kind === k.key ? null : k.key)} tone={k.tone}>
                  {k.label} <span className="tabular-nums text-faint">{k.count}</span>
                </ToggleChip>
              ))}
            </div>
          )}
          <div className="space-y-10">
            {months.map((m) => (
              <div key={m.key} className="grid gap-x-8 gap-y-3 md:grid-cols-[160px_minmax(0,1fr)]">
                <div className="md:sticky md:top-6 md:self-start">
                  <h3 className="font-display text-[1.45rem] leading-tight text-fg">{m.label}</h3>
                  <p className="text-[12px] text-faint">{plural(m.items.length, "milestone")}</p>
                </div>
                <ol className="relative before:absolute before:bottom-3 before:left-[11.5px] before:top-3 before:w-px before:bg-border">
                  {m.items.map((e) => (
                    <MilestoneRow key={e.id} e={e} />
                  ))}
                </ol>
              </div>
            ))}
          </div>
        </>
      )}
    </section>
  );
}

function MilestoneRow({ e }: { e: ActivityEvent }) {
  const v = eventVisual(e);
  const Icon = v.icon;
  const href = e.entity_id && e.entity_type ? entityHref(e.entity_type, e.entity_id, e.idea_id) : null;
  const tag = milestoneTag(e.event_type);
  return (
    <li className="relative flex gap-3 pb-5 last:pb-0">
      <span className="relative z-[1] flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-border bg-surface" style={{ color: v.tone }} aria-hidden>
        <Icon className="h-3 w-3" />
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        <p className="break-words text-[14px] leading-snug text-fg">
          {tag && (
            <span className="mr-1.5 inline-flex h-[18px] items-center rounded-[3px] border border-border px-1 align-[1px] text-[10px] font-semibold uppercase tracking-wider text-muted">
              {tag}
            </span>
          )}
          {href && href !== "/" ? (
            <Link href={href} className="hover:underline hover:decoration-border-strong hover:underline-offset-2">
              {e.summary}
            </Link>
          ) : (
            e.summary
          )}
        </p>
        <div className="mt-1 flex min-w-0 items-center gap-2 text-[12px] text-faint">
          <time dateTime={e.created_at} className="shrink-0 tabular-nums">
            {format(new Date(e.created_at), "d MMM, HH:mm")}
          </time>
          {(e.idea_id || e.idea_title) && (
            <>
              <span aria-hidden>·</span>
              <IdeaLink id={e.idea_id} title={e.idea_title} />
            </>
          )}
        </div>
      </div>
    </li>
  );
}

function TimelineSkeleton() {
  return (
    <div className="space-y-10" aria-busy="true" aria-label="Loading">
      <div className="space-y-3">
        <Skeleton className="h-3 w-20" />
        <Skeleton className="h-4 w-2/3" />
        <Skeleton className="h-[120px] w-full max-w-3xl" />
      </div>
      <div className="space-y-3">
        <Skeleton className="h-3 w-32" />
        <Skeleton className="h-[220px] w-full" />
      </div>
    </div>
  );
}
