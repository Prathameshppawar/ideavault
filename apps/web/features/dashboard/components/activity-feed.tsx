"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { format } from "date-fns";
import {
  ArrowRightLeft,
  Flag,
  GitMerge,
  Import,
  Inbox,
  PencilLine,
  Repeat2,
  Undo2,
  type LucideIcon,
} from "lucide-react";
import type { ActivityEvent } from "@/lib/types";
import { entityHref, entityMeta, tone } from "@/lib/entities";
import { cn } from "@/lib/format";
import { collapseProposals, describeCounts, eventKind, groupByDay, type FeedItem } from "../lib/activity";
import { IdeaLink } from "../ui";

/** Icon + colour for an activity event. */
export function eventVisual(e: Pick<ActivityEvent, "event_type" | "entity_type">): { icon: LucideIcon; tone: string } {
  const kind = eventKind(e);
  const t = e.event_type;
  if (t === "decision.reversed") return { icon: Undo2, tone: tone("decision") };
  if (t === "decision.superseded") return { icon: Repeat2, tone: tone("decision") };
  if (t === "idea.concluded") return { icon: Flag, tone: tone("checkpoint") };
  if (t === "idea.status_changed") return { icon: ArrowRightLeft, tone: tone("idea") };
  if (t === "idea.updated") return { icon: PencilLine, tone: tone("idea") };
  if (t === "conversation.imported") return { icon: Import, tone: tone("conversation") };
  if (t === "delta.merged") return { icon: GitMerge, tone: tone("branch") };
  return { icon: entityMeta(kind).icon, tone: tone(kind) };
}

function eventHref(e: ActivityEvent): string | null {
  if (!e.entity_id || !e.entity_type) return null;
  const href = entityHref(e.entity_type, e.entity_id, e.idea_id);
  return href === "/" ? null : href;
}

function actorText(actor: string): string {
  if (!actor || actor === "user") return "";
  if (actor === "agent" || actor === "system") return "by IdeaVault";
  if (actor === "import") return "via import";
  return `by ${actor}`;
}

function Dot({ icon: Icon, color, muted }: { icon: LucideIcon; color: string; muted?: boolean }) {
  return (
    <span
      className={cn("relative z-[1] flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-border bg-surface", muted && "border-dashed")}
      style={{ color }}
      aria-hidden
    >
      <Icon className="h-3 w-3" />
    </span>
  );
}

function FeedRow({ item }: { item: FeedItem }) {
  if (item.type === "proposals") {
    return (
      <li className="relative flex gap-3 pb-4">
        <Dot icon={Inbox} color="var(--warning)" muted />
        <div className="min-w-0 flex-1 pt-0.5">
          <p className="text-[14px] leading-snug text-fg">
            {item.ideaId ? (
              <Link href={`/ideas/${item.ideaId}?tab=review`} className="hover:underline hover:decoration-border-strong hover:underline-offset-2">
                Proposed for review: {describeCounts(item.counts)}
              </Link>
            ) : (
              <>Proposed for review: {describeCounts(item.counts)}</>
            )}
          </p>
          <div className="mt-1 flex min-w-0 items-center gap-2 text-[12px] text-faint">
            <IdeaLink id={item.ideaId} title={item.ideaTitle} />
            <span aria-hidden>·</span>
            <time dateTime={item.at} className="shrink-0 tabular-nums">{format(new Date(item.at), "HH:mm")}</time>
          </div>
        </div>
      </li>
    );
  }
  const e = item.event;
  const v = eventVisual(e);
  const href = eventHref(e);
  return (
    <li className="relative flex gap-3 pb-4">
      <Dot icon={v.icon} color={v.tone} />
      <div className="min-w-0 flex-1 pt-0.5">
        <p className="break-words text-[14px] leading-snug text-fg">
          {href ? (
            <Link href={href} className="hover:underline hover:decoration-border-strong hover:underline-offset-2">
              {e.summary}
            </Link>
          ) : (
            e.summary
          )}
        </p>
        <div className="mt-1 flex min-w-0 items-center gap-2 text-[12px] text-faint">
          {(e.idea_id || e.idea_title) && (
            <>
              <IdeaLink id={e.idea_id} title={e.idea_title} />
              <span aria-hidden>·</span>
            </>
          )}
          {actorText(e.actor) && (
            <>
              <span className="shrink-0">{actorText(e.actor)}</span>
              <span aria-hidden>·</span>
            </>
          )}
          <time dateTime={e.created_at} className="shrink-0 tabular-nums">{format(new Date(e.created_at), "HH:mm")}</time>
        </div>
      </div>
    </li>
  );
}

/** Activity grouped by day with a quiet vertical rule; proposal bursts are folded. */
export function ActivityFeed({ events, now, limitPerDay }: { events: ActivityEvent[]; now: Date; limitPerDay?: number }) {
  const groups = useMemo(() => groupByDay(collapseProposals(events), (i) => (i.type === "event" ? i.event.created_at : i.at), now), [events, now]);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  return (
    <div className="space-y-6">
      {groups.map((g) => (
        <section key={g.key} aria-label={g.label}>
          <h3 className="mb-3 text-[12px] font-medium text-muted">{g.label}</h3>
          <ol className="relative before:absolute before:bottom-4 before:left-[11.5px] before:top-3 before:w-px before:bg-border">
            {(limitPerDay && !expanded.has(g.key) ? g.items.slice(0, limitPerDay) : g.items).map((item) => (
              <FeedRow key={item.key} item={item} />
            ))}
          </ol>
          {limitPerDay && g.items.length > limitPerDay && !expanded.has(g.key) && (
            <button
              type="button"
              onClick={() => setExpanded((s) => new Set(s).add(g.key))}
              className="ml-9 rounded text-[12px] text-muted underline decoration-border-strong underline-offset-2 hover:text-fg"
            >
              Show {g.items.length - limitPerDay} more from {g.label.toLowerCase() === "today" || g.label === "Yesterday" ? g.label.toLowerCase() : g.label}
            </button>
          )}
        </section>
      ))}
    </div>
  );
}
