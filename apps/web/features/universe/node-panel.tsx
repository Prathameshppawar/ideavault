"use client";

import { useEffect, useMemo, useRef } from "react";
import { ArrowLeft, ArrowRight, Crosshair, MessageSquare, ShieldAlert, X } from "lucide-react";
import type { Graph, GraphNode } from "@/lib/types";
import { entityHref, entityMeta, isKnowledge, tone } from "@/lib/entities";
import { plural, truncate } from "@/lib/format";
import { RefLabel } from "@/components/domain/knowledge";
import { ItemStatus, KindTag, OriginTag, StatusBadge } from "@/components/ui/badges";
import { IdeaLink, LinkButton } from "@/features/dashboard/ui";
import { connectionsOf, edgeStyle, NODE_TYPE_ORDER } from "./graph-model";

/** Right-hand detail panel for the selected node. */
export function NodePanel({ node, graph, focusedIdeaId, onClose, onChoose, onFocusIdea }: {
  node: GraphNode;
  graph: Graph;
  focusedIdeaId: string;
  onClose: () => void;
  onChoose: (id: string) => void;
  onFocusIdea: (id: string) => void;
}) {
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    closeRef.current?.focus({ preventScroll: true });
  }, []);

  const meta = entityMeta(node.type);
  const connections = useMemo(() => connectionsOf(graph, node.id), [graph, node.id]);
  const ideaTitle = useMemo(() => (node.idea_id ? graph.nodes.find((n) => n.id === node.idea_id)?.label : undefined), [graph, node.idea_id]);
  const contents = useMemo(() => {
    if (node.type !== "idea") return [];
    const counts = new Map<string, number>();
    for (const n of graph.nodes) if (n.idea_id === node.id && n.id !== node.id) counts.set(n.type, (counts.get(n.type) ?? 0) + 1);
    return NODE_TYPE_ORDER.filter((t) => counts.has(t)).map((t) => ({ type: t, count: counts.get(t)! }));
  }, [graph, node]);
  const href = entityHref(node.type, node.id, node.idea_id);
  const imported = node.type === "conversation" && node.status && node.status !== "native";

  return (
    <aside
      aria-label={`${meta.label} details`}
      className="absolute inset-x-0 bottom-0 z-20 flex max-h-[70%] flex-col border-t border-border bg-surface shadow-lg animate-slide-up sm:inset-y-0 sm:left-auto sm:right-0 sm:max-h-none sm:w-[min(360px,100%)] sm:border-l sm:border-t-0"
    >
      <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-2.5">
        <KindTag kind={node.type} />
        <button ref={closeRef} type="button" onClick={onClose} aria-label="Close details (Esc)" className="rounded p-1 text-faint hover:bg-surface-2 hover:text-fg">
          <X className="h-4 w-4" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
        {node.type === "idea" ? (
          <>
            <h2 className="font-display text-[1.65rem] leading-tight text-fg">{node.label || "Untitled idea"}</h2>
            {node.status && <StatusBadge status={node.status} className="mt-2" />}
            {node.title && <p className="mt-3 text-[14px] leading-relaxed text-muted">{node.title}</p>}
          </>
        ) : isKnowledge(node.type) ? (
          <>
            <div className="flex items-start gap-2.5">
              <RefLabel kind={node.type} label={node.label} className="mt-0.5" />
              <p className={node.status && ["SUPERSEDED", "REVERSED", "REJECTED", "INVALIDATED"].includes(node.status) ? "text-[15px] leading-snug text-faint line-through" : "text-[15px] leading-snug text-fg"}>
                {node.title}
              </p>
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-2.5">
              {node.status && <ItemStatus status={node.status} />}
              {node.origin && <OriginTag origin={node.origin} />}
            </div>
          </>
        ) : (
          <>
            <h2 className="text-[16px] font-medium leading-snug text-fg">{node.label || node.title || meta.label}</h2>
            {node.title && node.title !== node.label && <p className="mt-1 text-[13px] text-muted">{node.title}</p>}
            {imported && (
              <p className="mt-3 inline-flex items-center gap-1.5 rounded-[4px] border border-warning/30 bg-warning-soft px-2 py-1 text-[12px] text-warning">
                <ShieldAlert className="h-3.5 w-3.5" aria-hidden /> Imported from {node.status} · untrusted source
              </p>
            )}
            {node.status && !imported && node.type !== "conversation" && <p className="mt-2 text-[12px] uppercase tracking-wide text-faint">{node.status.toLowerCase().replace(/_/g, " ")}</p>}
          </>
        )}

        {node.type !== "idea" && node.idea_id && (
          <p className="mt-4 flex items-center gap-2 text-[12px] text-faint">
            Part of <IdeaLink id={node.idea_id} title={ideaTitle} />
          </p>
        )}

        {contents.length > 0 && (
          <div className="mt-5">
            <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-[0.07em] text-faint">In this view</h3>
            <ul className="flex flex-wrap gap-x-4 gap-y-1.5">
              {contents.map((c) => (
                <li key={c.type} className="flex items-center gap-1.5 text-[13px] text-muted">
                  <span className="h-2 w-2 rounded-full" style={{ background: tone(c.type) }} aria-hidden />
                  {plural(c.count, entityMeta(c.type).label.toLowerCase(), entityMeta(c.type).plural.toLowerCase())}
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="mt-5">
          <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-[0.07em] text-faint">Relationships {connections.length > 0 && <span className="font-normal">{connections.length}</span>}</h3>
          {connections.length === 0 ? (
            <p className="text-[13px] text-faint">No relationships beyond its idea.</p>
          ) : (
            <ul className="-mx-2 space-y-0.5">
              {connections.map((c) => {
                const st = edgeStyle(c.edge.type);
                return (
                  <li key={`${c.edge.id}-${c.direction}`}>
                    <button type="button" onClick={() => onChoose(c.node.id)} className="flex w-full items-start gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left hover:bg-surface-2">
                      <span className="mt-0.5 inline-flex w-[7.5rem] shrink-0 items-center gap-1 text-[11px] font-medium" style={{ color: st.tone ? `var(--k-${st.tone})` : "var(--text-muted)" }}>
                        {c.direction === "in" && <ArrowLeft className="h-3 w-3 shrink-0" aria-hidden />}
                        <span className="truncate">{c.direction === "in" ? st.inverse : st.label}</span>
                        {c.direction === "out" && <ArrowRight className="h-3 w-3 shrink-0" aria-hidden />}
                      </span>
                      <span className="min-w-0 flex-1 text-[13px] leading-snug text-fg">
                        <span className="font-mono text-[11px] font-semibold" style={{ color: tone(c.node.type) }}>
                          {c.node.type === "idea" ? "" : `${c.node.label} `}
                        </span>
                        {truncate(c.node.type === "idea" ? c.node.label : c.node.title || c.node.label, 90)}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
      <div className="flex flex-wrap gap-2 border-t border-border px-4 py-3">
        {href !== "/" && (
          <LinkButton href={href} variant="primary" size="sm">
            Open {meta.label.toLowerCase()} <ArrowRight className="h-3.5 w-3.5" aria-hidden />
          </LinkButton>
        )}
        {node.type === "idea" && node.id !== focusedIdeaId && (
          <button
            type="button"
            onClick={() => onFocusIdea(node.id)}
            className="inline-flex h-8 items-center gap-1.5 rounded-[var(--radius-md)] border border-border bg-surface px-2.5 text-[13px] font-medium text-fg shadow-sm hover:border-border-strong hover:bg-surface-2"
          >
            <Crosshair className="h-3.5 w-3.5" aria-hidden /> Focus
          </button>
        )}
        {(node.idea_id || node.type === "idea") && (
          <LinkButton href={`/?idea=${node.type === "idea" ? node.id : node.idea_id}`} variant="ghost" size="sm">
            <MessageSquare className="h-3.5 w-3.5" aria-hidden /> Ask about it
          </LinkButton>
        )}
      </div>
    </aside>
  );
}
