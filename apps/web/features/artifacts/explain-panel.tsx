"use client";

import { useState } from "react";
import { ArrowUp, CircleHelp } from "lucide-react";
import { api, errorMessage } from "@/lib/api";
import { truncate } from "@/lib/format";
import type { ArtifactExplanation, ProvenanceEntry } from "@/lib/types";
import { Markdown } from "@/components/markdown";
import { KnowledgeRow } from "@/components/domain/knowledge";
import { Button } from "@/components/ui/button";
import { SkeletonLines, Textarea } from "@/components/ui/primitives";
import { analyzerLabel, linkifyLabels, type LabelRef } from "./artifact-meta";

export interface ExplainTurn {
  id: number;
  question: string;
  result?: ArtifactExplanation;
  error?: string;
}

/**
 * "Why does it say this?" — answers come only from the artifact's recorded provenance.
 * The thread lives in the parent so closing the sheet doesn't lose it.
 */
export function ExplainPanel({
  artifactId,
  cited,
  thread,
  setThread,
}: {
  artifactId: string;
  cited: ProvenanceEntry[];
  thread: ExplainTurn[];
  setThread: React.Dispatch<React.SetStateAction<ExplainTurn[]>>;
}) {
  const [q, setQ] = useState("");
  const pending = thread.some((t) => !t.result && !t.error);

  const ask = async (question: string) => {
    const text = question.trim();
    if (!text || pending) return;
    const id = Date.now();
    setThread((t) => [...t, { id, question: text }]);
    setQ("");
    try {
      const result = await api.post<ArtifactExplanation>(`/v1/artifacts/${artifactId}/explain`, { question: text });
      setThread((t) => t.map((x) => (x.id === id ? { ...x, result } : x)));
    } catch (e) {
      setThread((t) => t.map((x) => (x.id === id ? { ...x, error: errorMessage(e) } : x)));
    }
  };

  const suggestions = cited
    .filter((e) => e.entity_type === "decision" || e.entity_type === "assumption")
    .slice(0, 3)
    .map((e) => `Why does it say “${truncate(e.title ?? e.label, 70)}”?`);

  return (
    <div className="flex h-full flex-col">
      <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-5 py-5">
        {thread.length === 0 ? (
          <div>
            <p className="text-[13px] leading-relaxed text-muted">
              Ask about any statement in this artifact. IdeaVault answers from the recorded decisions, evidence and assumptions it was built from — and says so when nothing supports it.
            </p>
            {suggestions.length > 0 && (
              <div className="mt-4 space-y-1.5">
                {suggestions.map((s) => (
                  <button
                    key={s}
                    type="button"
                    onClick={() => void ask(s)}
                    className="block w-full rounded-[var(--radius-md)] border border-border px-3 py-2 text-left text-[13px] text-fg transition-colors hover:border-border-strong hover:bg-surface-2"
                  >
                    {s}
                  </button>
                ))}
              </div>
            )}
          </div>
        ) : (
          thread.map((t) => <Turn key={t.id} turn={t} />)
        )}
      </div>
      <form
        className="border-t border-border p-4"
        onSubmit={(e) => {
          e.preventDefault();
          void ask(q);
        }}
      >
        <label htmlFor="explain-q" className="sr-only">
          Your question
        </label>
        <div className="relative">
          <Textarea
            id="explain-q"
            rows={2}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                void ask(q);
              }
            }}
            placeholder="Why does it say…?"
            className="resize-none pr-12"
          />
          <Button type="submit" size="icon-sm" variant="primary" className="absolute bottom-2 right-2" disabled={!q.trim() || pending} aria-label="Ask">
            <ArrowUp className="h-4 w-4" />
          </Button>
        </div>
      </form>
    </div>
  );
}

function Turn({ turn: t }: { turn: ExplainTurn }) {
  const refs: LabelRef[] = (t.result?.items ?? []).map((i) => ({ label: i.label, type: i.kind, id: i.id }));
  return (
    <div className="animate-slide-up">
      <p className="mb-2 flex items-start gap-2 text-[14px] font-medium text-fg">
        <CircleHelp className="mt-0.5 h-4 w-4 shrink-0 text-faint" aria-hidden />
        {t.question}
      </p>
      {t.error ? (
        <p className="rounded-[var(--radius-md)] bg-danger-soft px-3 py-2 text-[13px] text-danger">{t.error}</p>
      ) : !t.result ? (
        <div className="pl-6">
          <p className="mb-2 text-[12px] text-faint">Tracing the provenance…</p>
          <SkeletonLines lines={3} />
        </div>
      ) : (
        <div className="pl-6">
          <Markdown className="text-[14px]">{linkifyLabels(t.result.answer || "No answer.", refs)}</Markdown>
          <div className="mt-2 flex flex-wrap items-center gap-x-2 text-[11px] text-faint">
            {t.result.analyzer && <span>{analyzerLabel(t.result.analyzer)}</span>}
            <span>· v{t.result.version}</span>
          </div>
          {t.result.items.length > 0 && (
            <details className="group mt-3">
              <summary className="cursor-pointer select-none text-[12px] font-medium text-muted hover:text-fg">Based on {t.result.items.length} recorded items</summary>
              <div className="-mx-2.5 mt-1">
                {t.result.items.map((i) => (
                  <KnowledgeRow key={i.id} item={i} href={`/knowledge/${i.id}`} compact />
                ))}
              </div>
            </details>
          )}
        </div>
      )}
    </div>
  );
}
