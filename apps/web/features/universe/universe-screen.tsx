"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { FileText, History, Layers, Maximize2, MessagesSquare, Minus, Orbit, Plus, X } from "lucide-react";
import { api } from "@/lib/api";
import type { Graph, Idea } from "@/lib/types";
import { entityMeta } from "@/lib/entities";
import { cn, plural } from "@/lib/format";
import { EmptyState, ErrorState, Spinner } from "@/components/ui/primitives";
import { Tooltip } from "@/components/ui/overlay";
import { useCssVars, useMediaQuery, useReducedMotion } from "@/features/dashboard/lib/hooks";
import { useParamSetter } from "@/features/dashboard/lib/url";
import { LinkButton, ToggleChip } from "@/features/dashboard/ui";
import { GraphCanvas, type GraphCanvasHandle } from "./graph-canvas";
import { buildModel, edgeStyle, legendFor, type UNode } from "./graph-model";
import { NodeFinder } from "./node-finder";
import { NodePanel } from "./node-panel";

const COLOR_VARS = [
  "--k-idea",
  "--k-branch",
  "--k-decision",
  "--k-assumption",
  "--k-evidence",
  "--k-insight",
  "--k-question",
  "--k-action",
  "--k-checkpoint",
  "--k-artifact",
  "--k-conversation",
  "--border",
  "--border-strong",
  "--text",
  "--text-muted",
  "--text-faint",
  "--bg",
  "--surface",
  "--accent",
  "--font-instrument-serif",
  "--font-geist-sans",
] as const;

/** Remembers node objects across rebuilds so filtering/toggling keeps the layout stable. */
class PositionCache {
  private map = new Map<string, UNode>();
  get(id: string) {
    return this.map.get(id);
  }
  track(nodes: UNode[]) {
    for (const n of nodes) this.map.set(n.id, n);
  }
}

export function UniverseScreen() {
  const params = useSearchParams();
  const setParams = useParamSetter();
  const ideaId = params.get("idea") || "";
  const withConversations = params.get("conversations") === "1";
  const withArtifacts = params.get("artifacts") === "1";
  const withHistory = params.get("history") === "1";

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["graph", { ideaId, withConversations, withArtifacts, withHistory }],
    queryFn: () =>
      api.get<Graph>("/v1/graph", {
        query: { idea_id: ideaId || undefined, conversations: withConversations || undefined, artifacts: withArtifacts || undefined, history: withHistory || undefined },
      }),
    placeholderData: keepPreviousData,
  });
  const { data: ideaList } = useQuery({
    queryKey: ["ideas", { sort: "title", limit: 300 }],
    queryFn: () => api.get<{ ideas: Idea[]; total: number }>("/v1/ideas", { query: { sort: "title", limit: 300 } }),
    staleTime: 60_000,
  });

  const [hidden, setHidden] = useState<Set<string>>(() => new Set());
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const isDesktop = useMediaQuery("(min-width: 768px)", true);
  const [legendPref, setLegendPref] = useState<boolean | null>(null);
  const legendOpen = legendPref ?? isDesktop;
  const [cache] = useState(() => new PositionCache());
  const canvas = useRef<GraphCanvasHandle>(null);
  const reducedMotion = useReducedMotion();
  const colors = useCssVars(COLOR_VARS);

  const model = useMemo(() => {
    const m = buildModel(data, hidden, cache);
    cache.track(m.nodes);
    return m;
  }, [data, hidden, cache]);
  const legend = useMemo(() => legendFor(data), [data]);
  const selected = useMemo(() => (selectedId ? (data?.nodes ?? []).find((n) => n.id === selectedId) ?? null : null), [data, selectedId]);

  // Escape closes the detail panel.
  useEffect(() => {
    if (!selectedId) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !(e.target as HTMLElement | null)?.closest?.("[role=dialog],[role=listbox]")) setSelectedId(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedId]);

  const ideas = (data?.nodes ?? []).filter((n) => n.type === "idea").length;
  const semantic = (data?.edges ?? []).filter((e) => !e.structural).length;
  const total = data?.nodes?.length ?? 0;
  const visible = model.nodes.length;
  const focusIdea = ideaList?.ideas.find((i) => i.id === ideaId);

  const toggleType = (t: string) =>
    setHidden((h) => {
      const next = new Set(h);
      if (next.has(t)) next.delete(t);
      else next.add(t);
      return next;
    });

  const choose = (id: string) => {
    setSelectedId(id);
    requestAnimationFrame(() => canvas.current?.centerOn(id));
  };

  return (
    <div className="flex h-app min-h-[480px] flex-col">
      <header className="shrink-0 border-b border-border bg-bg px-4 pb-3 pt-5 sm:px-6">
        <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
          <div className="min-w-0">
            <h1 className="font-display text-[2rem] leading-none text-fg">Universe</h1>
            <p className="mt-1.5 text-[13px] text-muted">
              {isLoading ? (
                "Mapping your ideas…"
              ) : (
                <>
                  {focusIdea ? <>Focused on <span className="text-fg">{focusIdea.title}</span> · </> : null}
                  {plural(ideas, "idea")} · {plural(total, "node")} · {plural(semantic, "relationship")}
                  {visible < total && <span className="text-faint"> · {total - visible} hidden</span>}
                </>
              )}
              {isFetching && !isLoading && <Spinner className="ml-2 inline h-3 w-3 align-[-2px]" />}
            </p>
          </div>
          <NodeFinder nodes={data?.nodes ?? []} onChoose={choose} />
        </div>
        <div className="-mx-1 mt-3 flex items-center gap-2 overflow-x-auto px-1 pb-0.5">
          <label className="sr-only" htmlFor="universe-idea">
            Focus on one idea
          </label>
          <select
            id="universe-idea"
            value={ideaId}
            onChange={(e) => {
              setSelectedId(null);
              setParams({ idea: e.target.value || null });
            }}
            className="h-7 max-w-[14rem] shrink-0 truncate rounded-full border border-border bg-surface px-2.5 text-[12px] font-medium text-fg hover:border-border-strong focus:border-accent focus:outline-none"
          >
            <option value="">All ideas</option>
            {(ideaList?.ideas ?? []).map((i) => (
              <option key={i.id} value={i.id}>
                {i.title}
              </option>
            ))}
          </select>
          <span className="mx-1 h-4 w-px shrink-0 bg-border" aria-hidden />
          <ToggleChip pressed={withConversations} onClick={() => setParams({ conversations: withConversations ? null : "1" })} title="Include conversations">
            <MessagesSquare className="h-3.5 w-3.5" aria-hidden /> Conversations
          </ToggleChip>
          <ToggleChip pressed={withArtifacts} onClick={() => setParams({ artifacts: withArtifacts ? null : "1" })} title="Include artifacts">
            <FileText className="h-3.5 w-3.5" aria-hidden /> Artifacts
          </ToggleChip>
          <ToggleChip pressed={withHistory} onClick={() => setParams({ history: withHistory ? null : "1" })} title="Include superseded, reversed and rejected items">
            <History className="h-3.5 w-3.5" aria-hidden /> History
          </ToggleChip>
        </div>
      </header>

      <div className="relative min-h-0 flex-1 overflow-hidden bg-bg">
        {error && !data ? (
          <div className="flex h-full items-center justify-center p-6">
            <ErrorState error={error} onRetry={() => void refetch()} className="max-w-md" />
          </div>
        ) : isLoading ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-[13px] text-muted">
            <Spinner />
            Mapping your universe…
          </div>
        ) : total === 0 ? (
          <div className="flex h-full items-center justify-center p-6">
            <EmptyState
              icon={Orbit}
              title={ideaId ? "This idea has nothing to map yet" : "Your universe is empty"}
              description={ideaId ? "Decisions, assumptions and evidence will appear here as you think it through." : "Every idea you start becomes a star here, with its decisions, assumptions and evidence orbiting it."}
              action={
                <LinkButton href={ideaId ? `/?idea=${ideaId}` : "/"} variant="primary" size="sm">
                  {ideaId ? "Continue in chat" : "Start a new idea"}
                </LinkButton>
              }
              className="max-w-md bg-bg"
            />
          </div>
        ) : (
          <>
            <GraphCanvas
              ref={canvas}
              nodes={model.nodes}
              links={model.links}
              colors={colors}
              serif={colors["--font-instrument-serif"]}
              sans={colors["--font-geist-sans"]}
              selectedId={selectedId}
              onSelect={setSelectedId}
              reducedMotion={reducedMotion}
              insets={{ left: legendOpen && isDesktop ? 248 : 0, right: selected && isDesktop ? 360 : 0, bottomRatio: selected && !isDesktop ? 0.6 : 0 }}
              label={`Knowledge graph with ${plural(visible, "node")}. Use the find box to select a node; plus and minus zoom, arrow keys pan, 0 fits the view.`}
            />
            {visible === 0 && (
              <div className="pointer-events-none absolute inset-0 flex items-center justify-center p-6">
                <div className="pointer-events-auto rounded-[var(--radius-lg)] border border-dashed border-border bg-bg px-6 py-5 text-center">
                  <p className="text-sm font-medium text-fg">Everything is filtered out</p>
                  <button type="button" onClick={() => setHidden(new Set())} className="mt-2 text-[13px] text-muted underline decoration-border-strong underline-offset-2 hover:text-fg">
                    Show all types
                  </button>
                </div>
              </div>
            )}

            {/* Legend — doubles as the type filter */}
            <div className="pointer-events-none absolute bottom-3 left-3 flex flex-col items-start gap-2 sm:bottom-4 sm:left-4">
              {!legendOpen ? (
                <button
                  type="button"
                  onClick={() => setLegendPref(true)}
                  aria-expanded={false}
                  className="pointer-events-auto inline-flex h-8 items-center gap-1.5 rounded-[var(--radius-md)] border border-border bg-surface px-2.5 text-[12px] font-medium text-muted shadow-sm hover:text-fg"
                >
                  <Layers className="h-3.5 w-3.5" aria-hidden /> Legend
                  {hidden.size > 0 && <span className="text-faint">· {hidden.size} hidden</span>}
                </button>
              ) : (
                <div className="pointer-events-auto max-h-[min(60vh,520px)] w-[14.5rem] overflow-y-auto rounded-[var(--radius-lg)] border border-border bg-surface p-3 shadow-sm">
                  <div className="mb-1.5 flex items-center justify-between">
                    <p className="text-[11px] font-semibold uppercase tracking-[0.07em] text-faint">Show</p>
                    <button type="button" onClick={() => setLegendPref(false)} aria-label="Hide legend" aria-expanded className="rounded p-0.5 text-faint hover:bg-surface-2 hover:text-fg">
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </div>
                  <ul className="space-y-px" role="group" aria-label="Toggle node types">
                    {legend.nodes.map((n) => {
                      const on = !hidden.has(n.type);
                      const t = `var(--k-${entityMeta(n.type).tone})`;
                      return (
                        <li key={n.type}>
                          <button
                            type="button"
                            aria-pressed={on}
                            onClick={() => toggleType(n.type)}
                            className={cn("flex w-full items-center gap-2 rounded-[5px] px-1.5 py-[3px] text-left text-[12px] transition-colors hover:bg-surface-2", on ? "text-fg" : "text-faint")}
                          >
                            <span className={cn("shrink-0 rounded-full border-2", n.type === "idea" ? "h-3 w-3" : "h-2.5 w-2.5")} style={{ background: on ? t : "transparent", borderColor: t }} aria-hidden />
                            <span className={cn("flex-1", !on && "line-through decoration-faint/60")}>{entityMeta(n.type).plural}</span>
                            <span className="tabular-nums text-faint">{n.count}</span>
                          </button>
                        </li>
                      );
                    })}
                  </ul>
                  {legend.edges.length > 0 && (
                    <>
                      <p className="mb-1 mt-3 text-[11px] font-semibold uppercase tracking-[0.07em] text-faint">Relationships</p>
                      <ul className="space-y-0.5">
                        {legend.edges.map((e) => (
                          <li key={e.type} className="flex items-center gap-2 px-1.5 py-px text-[12px] text-muted">
                            <svg width="22" height="8" aria-hidden className="shrink-0">
                              <line
                                x1="1"
                                y1="4"
                                x2="21"
                                y2="4"
                                strokeWidth="1.75"
                                strokeLinecap="round"
                                style={{ stroke: e.style.tone ? `var(--k-${e.style.tone})` : "var(--text-faint)" }}
                                strokeDasharray={e.style.dash?.join(" ")}
                              />
                            </svg>
                            <span className="flex-1">{edgeStyle(e.type).label}</span>
                            <span className="tabular-nums text-faint">{e.count}</span>
                          </li>
                        ))}
                      </ul>
                    </>
                  )}
                  <p className="mt-2.5 flex items-center gap-2 px-1.5 text-[11px] leading-tight text-faint">
                    <span className="h-2.5 w-2.5 shrink-0 rounded-full border-[1.5px] border-faint" aria-hidden /> Hollow: superseded or reversed
                  </p>
                </div>
              )}
            </div>

            {/* Zoom controls */}
            <div className={cn("absolute bottom-3 flex flex-col overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface shadow-sm transition-[right] sm:bottom-4", selected ? "right-3 sm:right-[calc(min(360px,100%)+1rem)]" : "right-3 sm:right-4")}>
              <Tooltip content="Zoom in (+)" side="left">
                <button type="button" aria-label="Zoom in" onClick={() => canvas.current?.zoomBy(1.4)} className="flex h-8 w-8 items-center justify-center text-muted hover:bg-surface-2 hover:text-fg">
                  <Plus className="h-4 w-4" />
                </button>
              </Tooltip>
              <Tooltip content="Zoom out (−)" side="left">
                <button type="button" aria-label="Zoom out" onClick={() => canvas.current?.zoomBy(1 / 1.4)} className="flex h-8 w-8 items-center justify-center border-t border-border text-muted hover:bg-surface-2 hover:text-fg">
                  <Minus className="h-4 w-4" />
                </button>
              </Tooltip>
              <Tooltip content="Fit to view (0)" side="left">
                <button type="button" aria-label="Fit to view" onClick={() => canvas.current?.fit(true)} className="flex h-8 w-8 items-center justify-center border-t border-border text-muted hover:bg-surface-2 hover:text-fg">
                  <Maximize2 className="h-3.5 w-3.5" />
                </button>
              </Tooltip>
            </div>

            {selected && data && (
              <NodePanel
                key={selected.id}
                node={selected}
                graph={data}
                focusedIdeaId={ideaId}
                onClose={() => setSelectedId(null)}
                onChoose={choose}
                onFocusIdea={(id) => {
                  setSelectedId(null);
                  setParams({ idea: id });
                }}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}
