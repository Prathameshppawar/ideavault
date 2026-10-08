// Pure helpers that turn the server's daily activity buckets into a contribution-style
// heatmap and a continuous (gap-filled) series for the stacked bar chart.
import { addDays, differenceInCalendarDays, format, parseISO, startOfDay, startOfWeek } from "date-fns";
import type { TimelineView } from "@/lib/types";
import { EVENT_CATEGORIES, eventCategory, type EventCategory } from "./activity";

type Bucket = TimelineView["buckets"][number];

/**
 * Calendar day ("yyyy-MM-dd") a bucket belongs to. The API truncates to midnight in the
 * database's time zone (UTC by default): if the instant is exactly UTC midnight we use the
 * UTC date, otherwise the instant's local date.
 */
export function bucketDayKey(day: string): string {
  const d = parseISO(day);
  if (Number.isNaN(d.getTime())) return "";
  if (d.getUTCHours() === 0 && d.getUTCMinutes() === 0 && d.getUTCSeconds() === 0) return d.toISOString().slice(0, 10);
  return format(d, "yyyy-MM-dd");
}

/** Parse "yyyy-MM-dd" as a local date (no time-zone shift). */
export function keyToDate(key: string): Date {
  const [y, m, d] = key.split("-").map(Number);
  return new Date(y, (m ?? 1) - 1, d ?? 1);
}

export interface DayTotals {
  total: number;
  ideas: number;
  byCategory: Record<EventCategory, number>;
}

const emptyCats = (): Record<EventCategory, number> => ({ knowledge: 0, ideas: 0, decisions: 0, structure: 0 });

/** Index buckets by calendar day, folding raw event types into the chart categories. */
export function indexBuckets(buckets: Bucket[]): Map<string, DayTotals> {
  const m = new Map<string, DayTotals>();
  for (const b of buckets) {
    const key = bucketDayKey(b.day);
    if (!key) continue;
    const cur = m.get(key) ?? { total: 0, ideas: 0, byCategory: emptyCats() };
    cur.total += b.total;
    cur.ideas = Math.max(cur.ideas, b.ideas);
    for (const [type, n] of Object.entries(b.by_type ?? {})) cur.byCategory[eventCategory(type)] += n;
    m.set(key, cur);
  }
  return m;
}

/** Intensity level 0–4 for a day's count relative to the busiest day. */
export function heatLevel(count: number, max: number): 0 | 1 | 2 | 3 | 4 {
  if (count <= 0 || max <= 0) return 0;
  const r = count / max;
  if (r <= 0.25) return 1;
  if (r <= 0.5) return 2;
  if (r <= 0.75) return 3;
  return 4;
}

export interface HeatCell {
  key: string;
  date: Date;
  count: number;
  level: 0 | 1 | 2 | 3 | 4;
  /** Outside the selected range (padding to complete the first/last week). */
  outside: boolean;
  totals?: DayTotals;
}

export interface Heatmap {
  /** Columns of 7 cells (Monday → Sunday). */
  weeks: HeatCell[][];
  /** Month label at the first column whose week contains the 1st of a month (or the first column). */
  months: { col: number; label: string }[];
  max: number;
  total: number;
  activeDays: number;
  busiest?: { key: string; count: number; ideas: number };
}

/** Builds a week-column grid covering the last `days` days ending at `today` (inclusive). */
export function buildHeatmap(buckets: Bucket[], days: number, today: Date = new Date()): Heatmap {
  const end = startOfDay(today);
  const start = addDays(end, -(Math.max(1, days) - 1));
  const gridStart = startOfWeek(start, { weekStartsOn: 1 });
  const idx = indexBuckets(buckets);
  let max = 0;
  let total = 0;
  let activeDays = 0;
  let busiest: Heatmap["busiest"];
  for (const [key, t] of idx) {
    const d = keyToDate(key);
    if (d < start || d > end) continue;
    total += t.total;
    if (t.total > 0) activeDays++;
    if (t.total > max) {
      max = t.total;
      busiest = { key, count: t.total, ideas: t.ideas };
    }
  }
  const weeks: HeatCell[][] = [];
  const months: Heatmap["months"] = [];
  const nWeeks = Math.floor(differenceInCalendarDays(end, gridStart) / 7) + 1;
  for (let w = 0; w < nWeeks; w++) {
    const col: HeatCell[] = [];
    for (let i = 0; i < 7; i++) {
      const date = addDays(gridStart, w * 7 + i);
      const key = format(date, "yyyy-MM-dd");
      const outside = date < start || date > end;
      const totals = outside ? undefined : idx.get(key);
      const count = totals?.total ?? 0;
      col.push({ key, date, count, level: outside ? 0 : heatLevel(count, max), outside, totals });
      if (!outside && (date.getDate() === 1 || (w === 0 && months.length === 0))) {
        if (!months.some((m) => m.col === w)) months.push({ col: w, label: format(date, "MMM") });
      }
    }
    weeks.push(col);
  }
  return { weeks, months, max, total, activeDays, busiest };
}

export interface SeriesRow extends Record<EventCategory, number> {
  key: string;
  label: string;
  start: string;
  total: number;
}

/**
 * A continuous series for the stacked bar chart: one row per day for ranges up to 90 days,
 * one row per ISO week (Monday start) beyond that, zero-filled so the axis stays honest.
 */
export function buildSeries(buckets: Bucket[], days: number, today: Date = new Date()): { rows: SeriesRow[]; unit: "day" | "week" } {
  const unit = days > 90 ? "week" : "day";
  const end = startOfDay(today);
  const start = addDays(end, -(Math.max(1, days) - 1));
  const idx = indexBuckets(buckets);
  const rows: SeriesRow[] = [];
  const rowFor = new Map<string, SeriesRow>();
  const first = unit === "week" ? startOfWeek(start, { weekStartsOn: 1 }) : start;
  const step = unit === "week" ? 7 : 1;
  for (let d = first; d <= end; d = addDays(d, step)) {
    const key = format(d, "yyyy-MM-dd");
    const row: SeriesRow = {
      key,
      start: key,
      label: unit === "week" ? `Week of ${format(d, "d MMM")}` : format(d, "EEE d MMM"),
      total: 0,
      knowledge: 0,
      ideas: 0,
      decisions: 0,
      structure: 0,
    };
    rows.push(row);
    rowFor.set(key, row);
  }
  for (const [key, t] of idx) {
    const d = keyToDate(key);
    if (d < start || d > end) continue;
    const anchor = unit === "week" ? format(startOfWeek(d, { weekStartsOn: 1 }), "yyyy-MM-dd") : key;
    const row = rowFor.get(anchor);
    if (!row) continue;
    row.total += t.total;
    for (const c of EVENT_CATEGORIES) row[c.key] += t.byCategory[c.key];
  }
  return { rows, unit };
}

/**
 * Re-bucket raw activity events by the viewer's local calendar day, in the same shape as the
 * server's buckets (day = local midnight with offset), so the heatmap agrees with the feed.
 */
export function localBuckets(events: { event_type: string; created_at: string; idea_id?: string }[]): Bucket[] {
  const map = new Map<string, { day: string; total: number; by_type: Record<string, number>; ideaSet: Set<string> }>();
  for (const e of events) {
    const d = parseISO(e.created_at);
    if (Number.isNaN(d.getTime())) continue;
    const mid = startOfDay(d);
    const key = format(mid, "yyyy-MM-dd");
    let b = map.get(key);
    if (!b) {
      b = { day: format(mid, "yyyy-MM-dd'T'HH:mm:ssxxx"), total: 0, by_type: {}, ideaSet: new Set() };
      map.set(key, b);
    }
    b.total++;
    b.by_type[e.event_type] = (b.by_type[e.event_type] ?? 0) + 1;
    if (e.idea_id) b.ideaSet.add(e.idea_id);
  }
  return [...map.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([, b]) => ({ day: b.day, total: b.total, by_type: b.by_type, ideas: b.ideaSet.size }));
}

/** True when a newest-first event list (fetched with a limit) reaches back past the range start. */
export function coversRange(events: { created_at: string }[], days: number, today: Date, limit = 5000): boolean {
  if (events.length < limit) return true;
  const oldest = events[events.length - 1];
  const start = addDays(startOfDay(today), -(Math.max(1, days) - 1));
  return !!oldest && parseISO(oldest.created_at) < start;
}
