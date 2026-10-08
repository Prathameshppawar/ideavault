"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Info, Sparkles, Telescope } from "lucide-react";
import { api, errorMessage } from "@/lib/api";
import type { Analysis, Idea, ThinkingAnalysis } from "@/lib/types";
import { fullDate, timeAgo } from "@/lib/format";
import { tone } from "@/lib/entities";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { EntityChip } from "@/components/ui/badges";
import { EmptyState, ErrorState, SkeletonLines } from "@/components/ui/primitives";
import { Select } from "@/features/usage/controls";
import { DIMENSION_HINT, DIMENSION_LABEL, PATTERN_KIND_META, PROMPT_DIMENSIONS, analyzerLabel, asThinking, groupPatterns, latestFor, statsSentences, type ThinkingPattern } from "./insights-data";

export function ThinkingPanel() {
  const qc = useQueryClient();
  const [ideaId, setIdeaId] = useState("");
  const [fresh, setFresh] = useState<{ scope: string; at: string; result: ThinkingAnalysis } | null>(null);
  const ideas = useQuery({ queryKey: ["ideas", "insights-scope"], queryFn: () => api.get<{ ideas: Idea[]; total: number }>("/v1/ideas", { query: { limit: 200 } }), staleTime: 60_000 });
  const stored = useQuery({
    queryKey: ["analyses", "thinking", ideaId],
    queryFn: () => api.get<Analysis[]>("/v1/analysis", { query: { kind: "thinking", idea_id: ideaId || undefined } }),
  });
  const run = useMutation({
    mutationFn: () => api.post<ThinkingAnalysis>("/v1/analysis/thinking", ideaId ? { idea_id: ideaId } : {}),
    onSuccess: (r) => {
      setFresh({ scope: ideaId, at: new Date().toISOString(), result: r });
      void qc.invalidateQueries({ queryKey: ["analyses", "thinking"] });
    },
  });
  const latest = latestFor(stored.data, ideaId);
  const current = fresh && fresh.scope === ideaId ? { at: fresh.at, result: fresh.result } : latest ? { at: latest.created_at, result: asThinking(latest)! } : null;
  const ideaTitle = ideas.data?.ideas.find((i) => i.id === ideaId)?.title;

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <label htmlFor="thinking-scope" className="mb-1.5 block text-[13px] font-medium text-muted">
              Scope
            </label>
            <Select id="thinking-scope" value={ideaId} onChange={(e) => setIdeaId(e.target.value)} wrapperClassName="w-[280px] max-w-full">
              <option value="">Whole vault</option>
              {(ideas.data?.ideas ?? []).map((i) => (
                <option key={i.id} value={i.id}>
                  {i.title}
                </option>
              ))}
            </Select>
          </div>
          <Button variant="primary" onClick={() => run.mutate()} loading={run.isPending}>
            {!run.isPending && <Sparkles className="h-3.5 w-3.5" />} {current ? "Re-analyze" : "Analyze my thinking"}
          </Button>
        </div>
        {current && (
          <p className="text-[12px] text-faint" title={fullDate(current.at)}>
            Last analyzed {timeAgo(current.at)}
          </p>
        )}
      </div>
      {run.error ? (
        <p role="alert" className="text-[13px] text-danger">
          {errorMessage(run.error)}
        </p>
      ) : null}

      {stored.isLoading || run.isPending ? (
        <div className="space-y-6" aria-busy="true">
          <SkeletonLines lines={3} />
          <SkeletonLines lines={5} />
        </div>
      ) : stored.error && !current ? (
        <ErrorState error={stored.error} onRetry={() => void stored.refetch()} />
      ) : current ? (
        <ThinkingReport key={current.at} a={current.result} ideaTitle={ideaTitle} />
      ) : (
        <EmptyState
          icon={Telescope}
          title={ideaId ? "This idea hasn't been analyzed yet" : "No thinking analysis yet"}
          description="IdeaVault looks at how you decide, what you leave unvalidated and how you write prompts — computed from your own recorded thinking, with evidence you can click through."
          action={
            <Button size="sm" variant="secondary" onClick={() => run.mutate()}>
              Run the first analysis
            </Button>
          }
        />
      )}
    </div>
  );
}

export function ThinkingReport({ a, ideaTitle }: { a: ThinkingAnalysis; ideaTitle?: string }) {
  const stats = statsSentences(a.stats, a.scope);
  const groups = groupPatterns(a.patterns);
  const habits = a.prompt_habits ?? null;
  return (
    <article className="animate-fade-in space-y-10" aria-label="Thinking analysis">
      <p className="flex items-start gap-2 rounded-[var(--radius-md)] bg-surface-2 px-3 py-2 text-[12.5px] leading-relaxed text-muted">
        <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-faint" />
        {a.disclaimer || "These are observable patterns in your recorded thinking and prompts — not a psychological or medical assessment."}
      </p>

      {stats.length > 0 && (
        <section aria-label="Overview">
          {a.scope === "idea" && ideaTitle && <p className="mb-2 text-[12px] uppercase tracking-wide text-faint">{ideaTitle}</p>}
          <p className="max-w-3xl text-[1.2rem] leading-relaxed text-fg">{stats.join(" ")}</p>
        </section>
      )}

      <section aria-labelledby="patterns-title">
        <h2 id="patterns-title" className="mb-4 text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">
          Patterns
        </h2>
        {groups.length === 0 ? (
          <p className="text-[14px] text-muted">No clear patterns yet. Record a few more decisions, assumptions and questions, then re-run the analysis.</p>
        ) : (
          <div className="space-y-8">
            {groups.map((g) => (
              <PatternGroup key={g.kind} kind={g.kind} patterns={g.patterns} />
            ))}
          </div>
        )}
      </section>

      <div className="grid gap-10 lg:grid-cols-2">
        <section aria-labelledby="habits-title">
          <h2 id="habits-title" className="mb-1 text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">
            Prompt-habit profile
          </h2>
          <p className="mb-4 text-[12.5px] text-faint">How often your own prompts include each element (0–100).</p>
          {habits && Object.keys(habits).length > 0 ? <HabitBars habits={habits} /> : <p className="text-[13px] text-faint">Not enough prompts you typed yourself to build a profile yet.</p>}
        </section>
        <section aria-labelledby="narrative-title">
          <h2 id="narrative-title" className="mb-3 flex items-center gap-2 text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">
            Reflection
            <span className="inline-flex h-[18px] items-center rounded-[3px] border border-dashed border-k-insight/50 px-1 text-[10px] font-semibold tracking-wider text-k-insight">Interpretation</span>
          </h2>
          {a.narrative ? <Markdown className="text-[14.5px]">{a.narrative}</Markdown> : <p className="text-[13px] text-faint">No reflection generated.</p>}
          <p className="mt-3 text-[12px] text-faint">Computed with {analyzerLabel(a.analyzer)}.</p>
        </section>
      </div>
    </article>
  );
}

function PatternGroup({ kind, patterns }: { kind: string; patterns: ThinkingPattern[] }) {
  const meta = PATTERN_KIND_META[kind] ?? { label: kind, tone: "checkpoint", blurb: "" };
  const color = `var(--k-${meta.tone})`;
  return (
    <div>
      <div className="mb-2 flex items-baseline gap-2">
        <span className="h-2 w-2 translate-y-[-1px] rounded-full" style={{ background: color }} aria-hidden />
        <h3 className="text-[14px] font-medium text-fg">{meta.label}</h3>
        <span className="text-[12px] text-faint">{meta.blurb}</span>
      </div>
      <ul className="space-y-3 border-l-2 pl-4" style={{ borderColor: `color-mix(in srgb, ${color} 35%, transparent)` }}>
        {patterns.map((p) => (
          <li key={p.id}>
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
              <p className="text-[14.5px] font-medium text-fg">{p.title}</p>
              {p.metric && <p className="text-[12.5px] tabular-nums text-muted">{p.metric}</p>}
            </div>
            {p.description && <p className="mt-0.5 max-w-3xl text-[13.5px] leading-relaxed text-muted">{p.description}</p>}
            {(p.evidence ?? []).length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1.5" aria-label="Evidence">
                {(p.evidence ?? []).map((e) => (
                  <EntityChip key={`${e.type}-${e.id}`} type={e.type} id={e.id} label={e.label || e.title} title={e.title || e.label} className="max-w-[260px]" />
                ))}
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function HabitBars({ habits }: { habits: Record<string, number> }) {
  return (
    <ul className="space-y-2">
      {PROMPT_DIMENSIONS.map((d) => {
        const v = Math.max(0, Math.min(100, Math.round(habits[d] ?? 0)));
        return (
          <li key={d} className="grid grid-cols-[120px_minmax(0,1fr)_32px] items-center gap-3 text-[13px]" title={DIMENSION_HINT[d]}>
            <span className="truncate text-muted">{DIMENSION_LABEL[d]}</span>
            <span className="h-2 overflow-hidden rounded-full bg-surface-3" role="img" aria-label={`${DIMENSION_LABEL[d]} ${v} out of 100`}>
              <span className="block h-full rounded-full" style={{ width: `${v}%`, background: tone("insight") }} />
            </span>
            <span className="text-right tabular-nums text-fg">{v}</span>
          </li>
        );
      })}
    </ul>
  );
}
