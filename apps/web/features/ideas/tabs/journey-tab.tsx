"use client";

import { useMemo, useRef, useState } from "react";
import Link from "next/link";
import { ChevronLeft, ChevronRight, GitBranch, MessagesSquare, Route } from "lucide-react";
import type { Branch, JourneyNode } from "@/lib/types";
import { cn, fullDate, humanize } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { EntityChip } from "@/components/ui/badges";
import { Dialog, SheetContent } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/primitives";
import { CheckpointPill, DetailRow, Interpretation, SourceQuote } from "../ui";
import { useJourney } from "../hooks";
import { formatProvenanceValue, JOURNEY_KIND, journeyKind, layoutJourney, splitRefTitle, type JourneyKind } from "../lib/journey";
import { RefLabel } from "@/components/domain/knowledge";
import { forkPath } from "../lib/branch-tree";

const COL = 164;

export function JourneyTab({ ideaId, branches, defaultBranchId }: { ideaId: string; branches: Branch[]; defaultBranchId?: string | null }) {
  const { data, isLoading, error, refetch } = useJourney(ideaId);
  const [hidden, setHidden] = useState<string[]>([]);
  const [selected, setSelected] = useState<JourneyNode | null>(null);
  const scroller = useRef<HTMLDivElement>(null);

  const nodes = useMemo(() => (data ?? []).filter((n) => n.kind === "origin" || n.kind === "current" || !hidden.includes(n.kind)), [data, hidden]);
  const layout = useMemo(() => layoutJourney(nodes, { branches, defaultBranchId, colWidth: COL, laneHeight: 136, padX: 76, labelHeight: 104 }), [nodes, branches, defaultBranchId]);
  const presentKinds = useMemo(() => {
    const s = new Set((data ?? []).map((n) => n.kind));
    return (Object.keys(JOURNEY_KIND) as JourneyKind[]).filter((k) => s.has(k) && k !== "origin" && k !== "current");
  }, [data]);
  const counts = useMemo(() => {
    const m: Record<string, number> = {};
    for (const n of data ?? []) m[n.kind] = (m[n.kind] ?? 0) + 1;
    return m;
  }, [data]);

  if (isLoading)
    return (
      <div className="space-y-4">
        <Skeleton className="h-7 w-2/3" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  if (error) return <ErrorState error={error} onRetry={() => void refetch()} />;
  if (!data || data.length <= 2)
    return (
      <EmptyState
        icon={Route}
        title="The journey starts here"
        description="As you decide things, take checkpoints, fork directions and generate artifacts, they appear here as one story from origin to now."
      />
    );

  const scrollBy = (dx: number) => scroller.current?.scrollBy({ left: dx, behavior: "smooth" });

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2">
        <p className="mr-auto text-[13px] text-muted">
          {data.length - 2} moments from origin to now{layout.lanes.length > 1 ? ` across ${layout.lanes.length} branches` : ""}. Select one to see what happened and why.
        </p>
        <div className="flex gap-1">
          <Button size="icon-sm" variant="ghost" onClick={() => scrollBy(-COL * 3)} aria-label="Scroll journey back">
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Button size="icon-sm" variant="ghost" onClick={() => scrollBy(COL * 3)} aria-label="Scroll journey forward">
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
      <div className="mb-3 flex flex-wrap gap-1.5" role="group" aria-label="Show kinds">
        {presentKinds.map((k) => {
          const on = !hidden.includes(k);
          return (
            <button
              key={k}
              type="button"
              aria-pressed={on}
              onClick={() => setHidden((h) => (on ? [...h, k] : h.filter((x) => x !== k)))}
              className={cn(
                "inline-flex h-6 items-center gap-1.5 rounded-full border px-2 text-[12px] transition-colors",
                on ? "border-border bg-surface text-fg" : "border-dashed border-border text-faint",
              )}
            >
              <span className="h-2 w-2 rounded-full" style={{ background: on ? `var(--k-${JOURNEY_KIND[k].tone})` : "var(--border-strong)" }} aria-hidden />
              {JOURNEY_KIND[k].label}
              <span className="tabular-nums text-faint">{counts[k]}</span>
            </button>
          );
        })}
      </div>

      <div ref={scroller} data-journey-scroller className="-mx-4 overflow-x-auto overscroll-x-contain px-4 pb-2 sm:mx-0 sm:rounded-[var(--radius-lg)] sm:border sm:border-border sm:bg-surface sm:px-0">
        <div className="relative" style={{ width: layout.width, height: layout.height }}>
          <svg className="absolute inset-0" width={layout.width} height={layout.height} aria-hidden>
            {layout.lanes.map((l) => (
              <g key={l.lane}>
                {l.fork && <path d={forkPath(l.fork.fromX, l.fork.fromY, l.startX, l.y)} fill="none" stroke="var(--k-branch)" strokeWidth={2} strokeOpacity={0.55} />}
                <line x1={l.startX} y1={l.y} x2={l.endX} y2={l.y} stroke={l.lane === 0 ? "var(--border-strong)" : "var(--k-branch)"} strokeOpacity={l.lane === 0 ? 1 : 0.55} strokeWidth={2} />
                {l.liveToX && <line x1={l.endX} y1={l.y} x2={l.liveToX} y2={l.y} stroke="var(--k-branch)" strokeOpacity={0.35} strokeWidth={2} strokeDasharray="3 5" />}
              </g>
            ))}
          </svg>
          {layout.lanes
            .filter((l) => l.lane > 0)
            .map((l) => (
              <span
                key={`label-${l.lane}`}
                className="absolute inline-flex max-w-[180px] -translate-y-[26px] items-center gap-1 whitespace-nowrap text-[11px] font-medium text-k-branch"
                style={{ left: l.startX - 6, top: l.y }}
              >
                <GitBranch className="h-3 w-3 shrink-0" aria-hidden /> <span className="truncate">{l.name}</span>
                {l.forkedFrom && <span className="shrink-0 font-normal text-faint">· from {l.forkedFrom}</span>}
              </span>
            ))}
          <ol>
            {layout.nodes.map((n) => {
              const k = journeyKind(n.node.kind);
              const big = n.node.kind === "origin" || n.node.kind === "current";
              const isSel = selected?.id === n.node.id && selected.kind === n.node.kind;
              return (
                <li key={`${n.node.kind}-${n.node.id}`} className="absolute" style={{ left: n.x - COL / 2, top: n.y - 10, width: COL }}>
                  {n.dayLabel && (
                    <span className="absolute left-1/2 -translate-x-1/2 -translate-y-[30px] whitespace-nowrap text-[11px] font-medium uppercase tracking-wide text-faint">{n.dayLabel}</span>
                  )}
                  <button
                    type="button"
                    onClick={() => setSelected(n.node)}
                    className="group flex w-full flex-col items-center rounded-[var(--radius-md)] px-2 pb-2 text-center focus-visible:outline-offset-0"
                    aria-label={`${k.label}: ${n.node.title}, ${fullDate(n.node.at)}`}
                  >
                    <span className="flex h-5 items-center justify-center">
                      <span
                        className={cn(
                          "block rounded-full border-2 transition-transform group-hover:scale-125",
                          big ? "h-5 w-5" : "h-3.5 w-3.5",
                          isSel && "scale-125 ring-4 ring-accent/20",
                          n.node.kind === "current" && "ring-4 ring-accent/15",
                        )}
                        style={{
                          borderColor: `var(--k-${k.tone})`,
                          background: n.node.kind === "checkpoint" || n.node.kind === "branch" ? "var(--surface)" : `var(--k-${k.tone})`,
                        }}
                      />
                    </span>
                    <span className="mt-2 text-[10.5px] font-semibold uppercase tracking-wider" style={{ color: `var(--k-${k.tone})` }}>
                      {k.label}
                    </span>
                    <NodeTitle node={n.node} selected={isSel} />
                    <span className="mt-0.5 text-[10.5px] tabular-nums text-faint">{n.node.kind === "current" ? "" : timeOf(n.node.at)}</span>
                  </button>
                </li>
              );
            })}
          </ol>
        </div>
      </div>

      <JourneyNodeDrawer node={selected} onClose={() => setSelected(null)} />
    </div>
  );
}

function timeOf(at: string) {
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

/** Track label: ref pill (D3 / CP2) + short title. Titles repeat the kind ("Branch: X"), so trim that. */
function NodeTitle({ node, selected }: { node: JourneyNode; selected: boolean }) {
  if (node.kind === "current") {
    return <span className="mt-0.5 text-[12.5px] font-medium text-fg">{humanize(node.title.replace(/^Now · /, ""))}</span>;
  }
  const split = splitRefTitle(node.title.replace(/^Branch: /, ""));
  const ref = split.ref;
  // Status events read "Status ACTIVE → DECIDED"; humanise the enum names.
  const text = node.kind === "status" ? split.text.replace(/\b[A-Z][A-Z_]{2,}\b/g, (m) => humanize(m)) : split.text;
  return (
    <span className={cn("mt-1 line-clamp-3 text-[12.5px] leading-snug text-fg group-hover:text-accent", selected && "text-accent")}>
      {ref && (
        <>
          {ref.startsWith("CP") ? (
            <CheckpointPill label={ref} className="mr-1 h-[18px] align-[1px] text-[10px]" />
          ) : (
            <RefLabel kind={node.kind === "decision" ? "decision" : "checkpoint"} label={ref} className="mr-1 h-[18px] min-w-0 align-[1px] text-[10px]" />
          )}
        </>
      )}
      {text}
    </span>
  );
}

function JourneyNodeDrawer({ node, onClose }: { node: JourneyNode | null; onClose: () => void }) {
  return (
    <Dialog open={Boolean(node)} onOpenChange={(v) => !v && onClose()}>
      {node && (
        <SheetContent title={journeyKind(node.kind).label}>
          <JourneyNodeDetail node={node} />
        </SheetContent>
      )}
    </Dialog>
  );
}

function JourneyNodeDetail({ node }: { node: JourneyNode }) {
  const k = journeyKind(node.kind);
  const prov = Object.entries(node.provenance ?? {}).filter(([, v]) => v !== null && v !== undefined && v !== "");
  const src = node.source;
  return (
    <div className="space-y-6 p-5 animate-fade-in">
      <header>
        <div className="mb-2 flex flex-wrap items-center gap-2 text-[12px]">
          <span className="inline-flex items-center gap-1.5 font-semibold uppercase tracking-wider" style={{ color: `var(--k-${k.tone})` }}>
            <span className="h-2 w-2 rounded-full" style={{ background: `var(--k-${k.tone})` }} /> {k.label}
          </span>
          <span className="text-faint">{fullDate(node.at)}</span>
          {node.branch_name && (
            <span className="inline-flex items-center gap-1 text-muted">
              <GitBranch className="h-3 w-3" /> {node.branch_name}
            </span>
          )}
        </div>
        <h2 className="font-display text-[1.6rem] leading-tight text-fg [overflow-wrap:anywhere]">{node.title}</h2>
      </header>

      {src?.excerpt && (
        <SourceQuote untrusted={src.untrusted} label={node.kind === "checkpoint" || node.kind === "conclusion" ? "note captured with the snapshot" : "what was actually said"}>
          {src.excerpt}
        </SourceQuote>
      )}
      {node.interpretation && <Interpretation>{node.interpretation}</Interpretation>}
      {!src?.excerpt && !node.interpretation && <p className="text-[13px] text-faint">No source text or interpretation was recorded for this moment.</p>}

      {node.prompt && (
        <div>
          <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-wider text-faint">The prompt that triggered it</p>
          <p className="whitespace-pre-wrap rounded-[var(--radius-lg)] bg-surface-2 px-3.5 py-2.5 text-[13.5px] leading-relaxed text-fg">{node.prompt}</p>
        </div>
      )}

      {src?.conversation_id && (
        <Link
          href={`/conversations/${src.conversation_id}${src.message_id ? `#m-${src.message_id}` : ""}`}
          className="inline-flex items-center gap-1.5 text-[13px] text-muted hover:text-fg"
        >
          <MessagesSquare className="h-3.5 w-3.5" /> Open the source conversation
        </Link>
      )}

      {(node.refs?.length ?? 0) > 0 && (
        <div>
          <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-wider text-faint">Involves</p>
          <div className="flex flex-wrap gap-1.5">
            {node.refs!.map((r) => (
              <EntityChip key={`${r.type}-${r.id}`} type={r.type} id={r.id} label={r.label ? `${r.label}${r.title ? ` · ${r.title.slice(0, 40)}` : ""}` : r.title} title={r.title} />
            ))}
          </div>
        </div>
      )}

      {prov.length > 0 && (
        <div>
          <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-faint">Provenance</p>
          <dl className="divide-y divide-border">
            {prov.map(([key, v]) => (
              <DetailRow key={key} label={humanize(key)}>
                <span className={cn(key.includes("hash") || key.endsWith("_id") ? "break-all font-mono text-[12px]" : undefined)}>{formatProvenanceValue(v)}</span>
              </DetailRow>
            ))}
          </dl>
        </div>
      )}
    </div>
  );
}
