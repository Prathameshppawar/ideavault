"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { cn, humanize } from "@/lib/format";
import { entityHref, entityMeta, tone } from "@/lib/entities";

export function Badge({ children, className, style }: { children: ReactNode; className?: string; style?: React.CSSProperties }) {
  return (
    <span
      style={style}
      className={cn("inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-[4px] border border-border bg-surface-2 px-1.5 text-[11px] font-medium text-muted", className)}
    >
      {children}
    </span>
  );
}

const statusTone: Record<string, string> = {
  EXPLORING: "insight",
  ACTIVE: "branch",
  DECIDED: "decision",
  READY_TO_IMPLEMENT: "action",
  CONCLUDED: "checkpoint",
  PARKED: "assumption",
  ABANDONED: "question",
  MERGED: "conversation",
};

/** Lifecycle status of an idea. */
export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const t = statusTone[status] ?? "checkpoint";
  return (
    <span
      className={cn("inline-flex h-5 items-center gap-1.5 whitespace-nowrap rounded-full px-2 text-[11px] font-medium", className)}
      style={{ background: `var(--k-${t}-soft)`, color: `var(--k-${t})` }}
    >
      <span className="h-1.5 w-1.5 rounded-full" style={{ background: `var(--k-${t})` }} />
      {humanize(status)}
    </span>
  );
}

const liveStatuses = new Set(["ACTIVE", "OPEN", "TODO", "IN_PROGRESS", "UNVALIDATED", "VALIDATING", "VALIDATED", "ANSWERED", "DONE", "PROPOSED"]);

/** Status of a knowledge item; non-live statuses (superseded, reversed…) render struck-through. */
export function ItemStatus({ status }: { status: string }) {
  const dead = !liveStatuses.has(status);
  return <span className={cn("text-[11px] font-medium uppercase tracking-wide", dead ? "text-faint line-through" : "text-muted")}>{humanize(status)}</span>;
}

/** Inline chip linking to any graph entity: type-coloured label, e.g. [D3]. */
export function EntityChip({ type, id, label, title, ideaId, className }: { type: string; id: string; label?: string; title?: string; ideaId?: string | null; className?: string }) {
  const meta = entityMeta(type);
  const Icon = meta.icon;
  return (
    <Link
      data-entity-chip=""
      href={entityHref(type, id, ideaId)}
      title={title}
      className={cn(
        "inline-flex max-w-full items-center gap-1 rounded-[4px] px-1.5 py-[1px] align-baseline text-[12px] font-medium no-underline transition-opacity hover:opacity-80",
        className,
      )}
      style={{ background: tone(type, true), color: tone(type) }}
    >
      <Icon className="h-3 w-3 shrink-0" aria-hidden />
      <span className="truncate">{label || title || meta.label}</span>
    </Link>
  );
}

/** Small coloured dot + kind label. */
export function KindTag({ kind, className }: { kind: string; className?: string }) {
  const meta = entityMeta(kind);
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide", className)} style={{ color: tone(kind) }}>
      <span className="h-1.5 w-1.5 rounded-full" style={{ background: tone(kind) }} />
      {meta.label}
    </span>
  );
}

/** SOURCE vs INTERPRETATION marker. */
export function OriginTag({ origin }: { origin: string }) {
  const isSource = origin === "SOURCE";
  return (
    <span
      title={isSource ? "Stated in the source conversation" : "Inferred by IdeaVault"}
      className={cn(
        "inline-flex h-[18px] items-center rounded-[3px] border px-1 text-[10px] font-semibold uppercase tracking-wider",
        isSource ? "border-border-strong text-muted" : "border-dashed border-k-insight/50 text-k-insight",
      )}
    >
      {isSource ? "Source" : "Interpretation"}
    </span>
  );
}
