"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { cn, plural, shortDate } from "@/lib/format";
import { entityMeta, tone } from "@/lib/entities";
import type { Idea, KnowledgeItem } from "@/lib/types";
import { ItemStatus, OriginTag, StatusBadge } from "@/components/ui/badges";

/** Ref label pill, e.g. D3, coloured by kind. */
export function RefLabel({ kind, label, className }: { kind: string; label: string; className?: string }) {
  return (
    <span
      className={cn("inline-flex h-5 min-w-[28px] shrink-0 items-center justify-center rounded-[4px] px-1 font-mono text-[11px] font-semibold", className)}
      style={{ background: tone(kind, true), color: tone(kind) }}
    >
      {label}
    </span>
  );
}

const DEAD = new Set(["SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED"]);

/**
 * One knowledge item as a row. Superseded/reversed items stay visible but muted:
 * history is never hidden, just de-emphasised.
 */
export function KnowledgeRow({ item, onClick, href, trailing, showKind, compact }: {
  item: KnowledgeItem;
  onClick?: () => void;
  href?: string;
  trailing?: ReactNode;
  showKind?: boolean;
  compact?: boolean;
}) {
  const dead = DEAD.has(item.status);
  const body = (
    <div className={cn("group flex items-start gap-3 rounded-[var(--radius-md)] px-2.5 transition-colors hover:bg-surface-2", compact ? "py-1.5" : "py-2.5")}>
      <RefLabel kind={item.kind} label={item.label} className={cn(dead && "opacity-50")} />
      <div className="min-w-0 flex-1">
        <p className={cn("text-[14px] leading-snug text-fg", dead && "text-faint line-through decoration-faint/60")}>{item.statement}</p>
        {!compact && item.kind === "decision" && item.decision?.rationale && <p className="mt-0.5 line-clamp-2 text-[13px] text-muted">because {item.decision.rationale}</p>}
        {!compact && (
          <div className="mt-1 flex flex-wrap items-center gap-x-2.5 gap-y-1">
            {showKind && <span className="text-[11px] font-medium uppercase tracking-wide" style={{ color: tone(item.kind) }}>{entityMeta(item.kind).label}</span>}
            <ItemStatus status={item.status} />
            {item.review_state === "PROPOSED" && <span className="text-[11px] font-medium uppercase tracking-wide text-warning">Proposed</span>}
            <OriginTag origin={item.origin} />
            {item.assumption?.risk === "HIGH" && <span className="text-[11px] font-medium text-k-question">High risk</span>}
            {item.evidence && item.evidence.stance !== "SUPPORTS" && <span className="text-[11px] text-muted">{item.evidence.stance.toLowerCase()}</span>}
            <span className="text-[11px] text-faint">{shortDate(item.created_at)}</span>
          </div>
        )}
      </div>
      {trailing}
    </div>
  );
  if (href) return <Link href={href}>{body}</Link>;
  if (onClick)
    return (
      <button type="button" onClick={onClick} className="block w-full text-left">
        {body}
      </button>
    );
  return body;
}

/** An idea as a list row: title (serif), summary, status, light stats. */
export function IdeaRow({ idea, className }: { idea: Idea; className?: string }) {
  const s = idea.stats;
  return (
    <Link href={`/ideas/${idea.id}`} className={cn("group block border-b border-border py-4 transition-colors last:border-0 hover:bg-surface-2/50", className)}>
      <div className="flex items-start justify-between gap-4 px-1">
        <div className="min-w-0">
          <h3 className="font-display text-[1.45rem] leading-tight text-fg group-hover:text-accent">{idea.title}</h3>
          {(idea.summary || idea.origin_text) && <p className="mt-1 line-clamp-2 max-w-2xl text-[14px] text-muted">{idea.summary || idea.origin_text}</p>}
          {s && (
            <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-faint">
              <span>{plural(s.decisions, "decision")}</span>
              <span>{plural(s.open_questions, "open question")}</span>
              <span>{plural(s.checkpoints, "checkpoint")}</span>
              {s.branches > 1 && <span>{plural(s.branches, "branch", "branches")}</span>}
              {s.artifacts > 0 && <span>{plural(s.artifacts, "artifact")}</span>}
              {s.proposed > 0 && <span className="text-warning">{s.proposed} to review</span>}
            </div>
          )}
        </div>
        <div className="flex shrink-0 flex-col items-end gap-2">
          <StatusBadge status={idea.status} />
          <span className="text-[12px] text-faint">{shortDate(idea.last_activity_at)}</span>
        </div>
      </div>
    </Link>
  );
}
