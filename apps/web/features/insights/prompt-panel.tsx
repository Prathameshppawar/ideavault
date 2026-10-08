"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, CircleCheck, CircleX, Copy, History, Lightbulb, Wand2 } from "lucide-react";
import { api, errorMessage } from "@/lib/api";
import type { Analysis, PromptAnalysis } from "@/lib/types";
import { cn, humanize, timeAgo, truncate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { ErrorState, Kbd, SectionTitle, SkeletonLines, Textarea } from "@/components/ui/primitives";
import { analyzerLabel, asPrompt } from "./insights-data";
import { OverallScore, RubricGrid } from "./prompt-score";

const historyKey = ["analyses", "prompt"] as const;

export function PromptPanel() {
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const [shown, setShown] = useState<{ id: string | null; result: PromptAnalysis } | null>(null);
  const history = useQuery({ queryKey: historyKey, queryFn: () => api.get<Analysis[]>("/v1/analysis", { query: { kind: "prompt" } }) });
  const analyze = useMutation({
    mutationFn: () => api.post<PromptAnalysis>("/v1/analysis/prompt", { text }),
    onSuccess: (r) => {
      setShown({ id: null, result: r });
      void qc.invalidateQueries({ queryKey: historyKey });
    },
  });
  const canRun = text.trim().length > 0 && !analyze.isPending;
  return (
    <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_260px]">
      <div className="min-w-0 space-y-8">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (canRun) analyze.mutate();
          }}
        >
          <label htmlFor="prompt-text" className="mb-2 block text-[14px] font-medium text-fg">
            Paste a prompt you wrote
          </label>
          <Textarea
            id="prompt-text"
            rows={7}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && canRun) {
                e.preventDefault();
                analyze.mutate();
              }
            }}
            placeholder="e.g. Help me plan a launch for my app…"
            className="text-[15px]"
          />
          {analyze.error ? (
            <p role="alert" className="mt-2 text-[13px] text-danger">
              {errorMessage(analyze.error)}
            </p>
          ) : null}
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <Button type="submit" variant="primary" loading={analyze.isPending} disabled={!canRun}>
              {!analyze.isPending && <Wand2 className="h-3.5 w-3.5" />} Analyze prompt
            </Button>
            <span className="text-[12px] text-faint">
              <Kbd>⌘</Kbd> <Kbd>↵</Kbd> · Scored on 8 dimensions. Your prompt is stored with the analysis.
            </span>
          </div>
        </form>
        {analyze.isPending ? (
          <div className="space-y-4" aria-busy="true">
            <SkeletonLines lines={2} />
            <SkeletonLines lines={4} />
          </div>
        ) : shown ? (
          <PromptAnalysisView key={shown.id ?? "new"} a={shown.result} onUse={(p) => setText(p)} />
        ) : null}
      </div>
      <aside aria-labelledby="prompt-history" className="min-w-0">
        <SectionTitle>
          <span id="prompt-history" className="inline-flex items-center gap-1.5">
            <History className="h-3.5 w-3.5" /> Past analyses
          </span>
        </SectionTitle>
        {history.isLoading ? (
          <SkeletonLines lines={4} />
        ) : history.error ? (
          <ErrorState error={history.error} onRetry={() => void history.refetch()} />
        ) : (history.data ?? []).length === 0 ? (
          <p className="text-[13px] leading-relaxed text-faint">Your analyses appear here, so you can see your prompting improve over time.</p>
        ) : (
          <ul className="-mx-2 space-y-0.5">
            {history.data!.map((a) => {
              const r = asPrompt(a)!;
              return (
                <li key={a.id}>
                  <button
                    type="button"
                    onClick={() => setShown({ id: a.id, result: r })}
                    aria-current={shown?.id === a.id ? "true" : undefined}
                    className={cn("flex w-full items-start gap-3 rounded-[var(--radius-md)] px-2 py-2 text-left hover:bg-surface-2", shown?.id === a.id && "bg-surface-3 hover:bg-surface-3")}
                  >
                    <span className="w-7 shrink-0 pt-px text-right text-[13px] font-semibold tabular-nums text-fg">{r.overall}</span>
                    <span className="min-w-0">
                      <span className="line-clamp-2 text-[12.5px] leading-snug text-muted">{truncate(r.prompt || "(empty)", 120)}</span>
                      <span className="text-[11px] text-faint">{timeAgo(a.created_at)}</span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </aside>
    </div>
  );
}

export function PromptAnalysisView({ a, onUse }: { a: PromptAnalysis; onUse?: (prompt: string) => void }) {
  const [copied, setCopied] = useState(false);
  return (
    <article className="animate-slide-up space-y-8 border-t border-border pt-8" aria-label="Prompt analysis">
      <div className="flex flex-wrap items-end justify-between gap-6">
        <OverallScore overall={a.overall} />
        <p className="text-[12px] text-faint">Analyzed with {analyzerLabel(a.analyzer)}</p>
      </div>
      <RubricGrid scores={a.scores} />
      <div className="grid gap-8 md:grid-cols-2">
        <FeedbackList title="What worked" items={a.good} icon={CircleCheck} tone="text-success" empty="Nothing stood out yet — see the suggestions." />
        <FeedbackList title="What was weak" items={a.weak} icon={CircleX} tone="text-danger" empty="No clear weaknesses." />
      </div>
      {(a.why_it_matters ?? []).length > 0 && (
        <div>
          <h3 className="mb-2 flex items-center gap-1.5 text-[13px] font-medium text-fg">
            <Lightbulb className="h-3.5 w-3.5 text-k-insight" /> Why it matters
          </h3>
          <ul className="space-y-1.5 text-[13.5px] leading-relaxed text-muted">
            {a.why_it_matters.map((w, i) => (
              <li key={i} className="border-l-2 border-k-insight/30 pl-3">
                {w}
              </li>
            ))}
          </ul>
        </div>
      )}
      {(a.missing_information ?? []).length > 0 && (
        <p className="flex flex-wrap items-center gap-1.5 text-[12.5px] text-muted">
          Missing:
          {a.missing_information.map((m) => (
            <span key={m} className="rounded-[4px] border border-dashed border-border-strong px-1.5 py-px text-fg">
              {humanize(m)}
            </span>
          ))}
        </p>
      )}
      {a.improved_prompt && (
        <div>
          <div className="mb-2 flex items-center justify-between gap-3">
            <h3 className="text-[13px] font-medium text-fg">Improved prompt</h3>
            <div className="flex gap-1">
              {onUse && (
                <Button size="xs" variant="ghost" onClick={() => onUse(a.improved_prompt)}>
                  Edit this
                </Button>
              )}
              <Button
                size="xs"
                variant="secondary"
                onClick={() => {
                  void navigator.clipboard.writeText(a.improved_prompt).then(() => {
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                  });
                }}
                aria-label="Copy improved prompt"
              >
                {copied ? <Check className="h-3 w-3 text-success" /> : <Copy className="h-3 w-3" />} {copied ? "Copied" : "Copy"}
              </Button>
            </div>
          </div>
          <pre className="whitespace-pre-wrap break-words rounded-[var(--radius-lg)] border border-border bg-surface px-4 py-3 font-sans text-[14px] leading-relaxed text-fg">{a.improved_prompt}</pre>
          <p className="mt-1.5 text-[12px] text-faint">[Bracketed parts] are for information only you know — fill them in before using it.</p>
        </div>
      )}
      <details className="text-[13px]">
        <summary className="cursor-pointer select-none text-muted hover:text-fg">Original prompt</summary>
        <pre className="mt-2 whitespace-pre-wrap break-words rounded-[var(--radius-md)] bg-surface-2 px-3 py-2 font-sans text-[13px] text-muted">{a.prompt}</pre>
      </details>
    </article>
  );
}

function FeedbackList({ title, items, icon: Icon, tone, empty }: { title: string; items: string[] | null; icon: React.ComponentType<{ className?: string }>; tone: string; empty: string }) {
  const list = items ?? [];
  return (
    <div>
      <h3 className="mb-2 text-[13px] font-medium text-fg">{title}</h3>
      {list.length === 0 ? (
        <p className="text-[13px] text-faint">{empty}</p>
      ) : (
        <ul className="space-y-1.5">
          {list.map((g, i) => (
            <li key={i} className="flex items-start gap-2 text-[13.5px] leading-relaxed text-fg">
              <Icon className={cn("mt-[3px] h-3.5 w-3.5 shrink-0", tone)} />
              {g}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
