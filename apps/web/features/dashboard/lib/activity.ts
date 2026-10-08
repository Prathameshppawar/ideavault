// Pure helpers for activity feeds: categorising event types, grouping by day/month,
// and folding noisy "*.proposed" bursts (imports, extraction) into one line.
import { differenceInCalendarDays, format, parseISO, startOfDay } from "date-fns";
import type { ActivityEvent } from "@/lib/types";
import { entityMeta } from "@/lib/entities";
import { plural } from "@/lib/format";

/** Coarse categories used by the timeline chart. Kept to four so the hues stay distinguishable. */
export type EventCategory = "knowledge" | "ideas" | "decisions" | "structure";

/** Stack order (bottom → top) and colour token for each category; validated for CVD separation. */
export const EVENT_CATEGORIES: { key: EventCategory; label: string; tone: string }[] = [
  { key: "knowledge", label: "Knowledge", tone: "evidence" },
  { key: "ideas", label: "Ideas & sources", tone: "idea" },
  { key: "decisions", label: "Decisions", tone: "decision" },
  { key: "structure", label: "Checkpoints & outputs", tone: "artifact" },
];

const KNOWLEDGE_PREFIXES = new Set(["assumption", "evidence", "insight", "question", "action", "knowledge", "relationship"]);
const STRUCTURE_PREFIXES = new Set(["checkpoint", "branch", "delta", "artifact", "context_pack"]);

export function eventCategory(eventType: string): EventCategory {
  const prefix = eventType.split(".")[0] ?? "";
  if (prefix === "decision") return "decisions";
  if (KNOWLEDGE_PREFIXES.has(prefix)) return "knowledge";
  if (STRUCTURE_PREFIXES.has(prefix)) return "structure";
  return "ideas";
}

/** Entity kind behind an event: explicit entity_type, else the event_type prefix. */
export function eventKind(e: Pick<ActivityEvent, "event_type" | "entity_type">): string {
  return e.entity_type || e.event_type.split(".")[0] || "idea";
}

export const isProposal = (eventType: string) => eventType.endsWith(".proposed");

function toDate(d: string | Date): Date {
  return typeof d === "string" ? parseISO(d) : d;
}

/** "Today", "Yesterday", "Monday" (this week), "5 Oct" (this year) or "5 Oct 2025". */
export function dayLabel(d: string | Date, now: Date): string {
  const date = toDate(d);
  const diff = differenceInCalendarDays(now, date);
  if (diff === 0) return "Today";
  if (diff === 1) return "Yesterday";
  if (diff > 1 && diff < 7) return format(date, "EEEE");
  return format(date, date.getFullYear() === now.getFullYear() ? "EEE d MMM" : "d MMM yyyy");
}

export interface DayGroup<T> {
  key: string;
  label: string;
  items: T[];
}

/** Groups items by local calendar day, preserving input order within and across groups. */
export function groupByDay<T>(items: T[], at: (item: T) => string, now: Date): DayGroup<T>[] {
  const groups: DayGroup<T>[] = [];
  const index = new Map<string, DayGroup<T>>();
  for (const item of items) {
    const date = toDate(at(item));
    if (Number.isNaN(date.getTime())) continue;
    const key = format(startOfDay(date), "yyyy-MM-dd");
    let g = index.get(key);
    if (!g) {
      g = { key, label: dayLabel(date, now), items: [] };
      index.set(key, g);
      groups.push(g);
    }
    g.items.push(item);
  }
  return groups;
}

/** Groups items by calendar month ("October 2026"), newest month first; items newest first within a month. */
export function groupByMonth<T>(items: T[], at: (item: T) => string): DayGroup<T>[] {
  const sorted = [...items].sort((a, b) => toDate(at(b)).getTime() - toDate(at(a)).getTime());
  const groups: DayGroup<T>[] = [];
  const index = new Map<string, DayGroup<T>>();
  for (const item of sorted) {
    const date = toDate(at(item));
    if (Number.isNaN(date.getTime())) continue;
    const key = format(date, "yyyy-MM");
    let g = index.get(key);
    if (!g) {
      g = { key, label: format(date, "MMMM yyyy"), items: [] };
      index.set(key, g);
      groups.push(g);
    }
    g.items.push(item);
  }
  return groups;
}

export type FeedItem =
  | { type: "event"; key: string; event: ActivityEvent }
  | { type: "proposals"; key: string; ideaId?: string; ideaTitle?: string; at: string; counts: Record<string, number>; total: number };

/**
 * Turns a list of events into feed items, folding every "*.proposed" event of the same idea
 * into one summary placed where the first (newest) of them appeared.
 */
export function collapseProposals(events: ActivityEvent[]): FeedItem[] {
  const out: FeedItem[] = [];
  const byIdea = new Map<string, Extract<FeedItem, { type: "proposals" }>>();
  for (const e of events) {
    if (!isProposal(e.event_type)) {
      out.push({ type: "event", key: e.id, event: e });
      continue;
    }
    const ideaKey = e.idea_id ?? "none";
    let agg = byIdea.get(ideaKey);
    if (!agg) {
      agg = { type: "proposals", key: `proposals:${ideaKey}:${e.id}`, ideaId: e.idea_id, ideaTitle: e.idea_title, at: e.created_at, counts: {}, total: 0 };
      byIdea.set(ideaKey, agg);
      out.push(agg);
    }
    const kind = eventKind(e);
    agg.counts[kind] = (agg.counts[kind] ?? 0) + 1;
    agg.total += 1;
  }
  return out;
}

/** "3 evidence, 2 questions" — largest first. */
export function describeCounts(counts: Record<string, number>): string {
  return Object.entries(counts)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([kind, n]) => {
      const m = entityMeta(kind);
      return plural(n, m.label.toLowerCase(), m.plural.toLowerCase());
    })
    .join(", ");
}
