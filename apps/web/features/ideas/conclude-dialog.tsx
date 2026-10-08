"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowRight, FileText, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { ConclusionResult, Idea, KnowledgeItem, Outcome } from "@/lib/types";
import { cn, humanize } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Checkbox, Dialog, DialogContent } from "@/components/ui/overlay";
import { Field, Label, Textarea } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/ui/badges";
import { Markdown } from "@/components/markdown";
import { CheckpointPill, Select } from "./ui";
import { useElapsed, useRefreshIdea } from "./hooks";
import { canTransition, OUTCOME_INFO, outcomeInfo, suggestOutcome, validateConclude } from "./lib/lifecycle";
import { ALL_ARTIFACT_TYPES, artifactTypeName, ARTIFACT_INFO } from "./lib/artifacts";

export function ConcludeDialog({
  open,
  onOpenChange,
  idea,
  branchId,
  branchName,
  items,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  idea: Idea;
  branchId: string | null;
  branchName?: string;
  /** Live knowledge of the branch (for the suggested outcome). */
  items: KnowledgeItem[];
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && <ConcludeForm idea={idea} branchId={branchId} branchName={branchName} items={items} onClose={() => onOpenChange(false)} />}
    </Dialog>
  );
}

function ConcludeForm({ idea, branchId, branchName, items, onClose }: { idea: Idea; branchId: string | null; branchName?: string; items: KnowledgeItem[]; onClose: () => void }) {
  const refresh = useRefreshIdea();
  const suggestion = useMemo(() => suggestOutcome(items), [items]);
  const [outcome, setOutcome] = useState<Outcome | null>(canTransition(idea.status, outcomeInfo(suggestion.outcome)!.status) ? suggestion.outcome : null);
  const [note, setNote] = useState("");
  const [mergeInto, setMergeInto] = useState<string>("");
  const [artifacts, setArtifacts] = useState<string[]>(() => (outcome ? (outcomeInfo(outcome)?.artifacts ?? []) : []));
  const [showAllTypes, setShowAllTypes] = useState(false);
  const [touched, setTouched] = useState(false);
  const [result, setResult] = useState<ConclusionResult | null>(null);

  const ideas = useQuery({
    queryKey: ["ideas", { picker: true }],
    queryFn: () => api.get<{ ideas: Idea[]; total: number }>("/v1/ideas", { query: { limit: 200, sort: "title" } }),
    enabled: outcome === "MERGE",
  });
  const targets = (ideas.data?.ideas ?? []).filter((i) => i.id !== idea.id && i.status !== "MERGED");

  const v = validateConclude({ ideaId: idea.id, currentStatus: idea.status, outcome, mergeIntoIdeaId: mergeInto || null });

  const conclude = useMutation({
    mutationFn: () =>
      api.post<ConclusionResult>(`/v1/ideas/${idea.id}/conclude`, {
        outcome,
        branch_id: branchId ?? undefined,
        note: note.trim() || undefined,
        merge_into_idea_id: outcome === "MERGE" ? mergeInto : undefined,
        generate_artifacts: artifacts.length ? artifacts : undefined,
      }),
    onSuccess: (r) => {
      setResult(r);
      toast.success(`Concluded as ${outcomeInfo(outcome)?.label.toLowerCase()}`);
      void refresh(idea.id);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const elapsed = useElapsed(conclude.isPending);

  if (result) return <ConclusionResultView result={result} onClose={onClose} />;

  const pick = (o: Outcome) => {
    setOutcome(o);
    setArtifacts(outcomeInfo(o)?.artifacts ?? []);
  };
  const visibleTypes = showAllTypes ? ALL_ARTIFACT_TYPES : ALL_ARTIFACT_TYPES.filter((t) => artifacts.includes(t) || OUTCOME_INFO.some((o) => o.artifacts.includes(t)));

  return (
    <DialogContent
      title="Conclude this idea"
      description={
        <>
          Takes a final checkpoint of <span className="font-medium text-fg">{branchName ?? "this branch"}</span>, summarises what was learned and decided, and records the outcome.
          Concluding never deletes anything — you can reopen it any time.
        </>
      }
      wide
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setTouched(true);
          if (v.ok) conclude.mutate();
        }}
        className="space-y-5"
      >
        <div>
          <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
            <Label className="mb-0">Outcome</Label>
            {suggestion && (
              <button type="button" onClick={() => pick(suggestion.outcome)} className="inline-flex items-center gap-1 text-[12px] text-muted hover:text-fg">
                <Sparkles className="h-3 w-3 text-k-insight" /> Suggested: {outcomeInfo(suggestion.outcome)?.label} — {suggestion.reason}
              </button>
            )}
          </div>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2" role="radiogroup" aria-label="Outcome">
            {OUTCOME_INFO.map((o) => {
              const allowed = canTransition(idea.status, o.status);
              const on = outcome === o.outcome;
              return (
                <button
                  key={o.outcome}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  disabled={!allowed}
                  onClick={() => pick(o.outcome)}
                  title={allowed ? undefined : `A ${humanize(idea.status).toLowerCase()} idea can't become ${humanize(o.status).toLowerCase()} — reopen it first`}
                  className={cn(
                    "group rounded-[var(--radius-lg)] border px-3.5 py-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-40",
                    on ? "border-fg bg-surface-2 shadow-sm" : "border-border hover:border-border-strong hover:bg-surface-2/60",
                  )}
                >
                  <span className="flex items-center justify-between gap-2">
                    <span className="text-[14px] font-medium text-fg">{o.label}</span>
                    <StatusBadge status={o.status} className="opacity-80" />
                  </span>
                  <span className="mt-0.5 block text-[12.5px] leading-snug text-muted">{o.description}</span>
                </button>
              );
            })}
          </div>
          {touched && v.errors.outcome && <p className="mt-2 text-[13px] text-danger">{v.errors.outcome}</p>}
        </div>

        {outcome === "MERGE" && (
          <Field label="Merge into" htmlFor="merge-into" error={touched ? v.errors.merge : undefined}>
            <Select id="merge-into" value={mergeInto} onChange={(e) => setMergeInto(e.target.value)} disabled={ideas.isLoading}>
              <option value="">{ideas.isLoading ? "Loading ideas…" : "Choose an idea…"}</option>
              {targets.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.title} ({humanize(t.status).toLowerCase()})
                </option>
              ))}
            </Select>
          </Field>
        )}

        <Field label="Closing note" htmlFor="conclude-note" hint="Why you're concluding it now, in your words. Kept with the final checkpoint.">
          <Textarea id="conclude-note" value={note} onChange={(e) => setNote(e.target.value)} rows={3} placeholder="Optional" />
        </Field>

        <div>
          <div className="mb-1.5 flex items-baseline justify-between gap-2">
            <Label className="mb-0">Generate from the final checkpoint</Label>
            <button type="button" onClick={() => setShowAllTypes((v) => !v)} className="text-[12px] text-muted hover:text-fg">
              {showAllTypes ? "Fewer types" : "All 16 types"}
            </button>
          </div>
          <div className="grid gap-1 sm:grid-cols-2">
            {visibleTypes.map((t) => {
              const id = `gen-${t}`;
              const on = artifacts.includes(t);
              return (
                <label key={t} htmlFor={id} className="flex cursor-pointer items-start gap-2.5 rounded-[var(--radius-md)] px-2 py-1.5 hover:bg-surface-2">
                  <Checkbox id={id} checked={on} onCheckedChange={() => setArtifacts((a) => (on ? a.filter((x) => x !== t) : [...a, t]))} className="mt-0.5" />
                  <span className="min-w-0">
                    <span className="block text-[13px] text-fg">{artifactTypeName(t)}</span>
                    <span className="block text-[11.5px] leading-snug text-faint">{ARTIFACT_INFO[t].description}</span>
                  </span>
                </label>
              );
            })}
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-end gap-3 border-t border-border pt-4">
          {conclude.isPending && (
            <span className="mr-auto text-[12.5px] text-muted" aria-live="polite">
              {artifacts.length ? `Writing the final checkpoint and ${artifacts.length} artifact${artifacts.length > 1 ? "s" : ""}… ${elapsed}s` : "Writing the final checkpoint…"}
            </span>
          )}
          <Button type="button" variant="ghost" onClick={onClose} disabled={conclude.isPending}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={conclude.isPending} disabled={touched && !v.ok}>
            Conclude{outcome ? ` as ${outcomeInfo(outcome)?.label.toLowerCase()}` : ""}
          </Button>
        </div>
      </form>
    </DialogContent>
  );
}

function ConclusionResultView({ result, onClose }: { result: ConclusionResult; onClose: () => void }) {
  const c = result.conclusion;
  return (
    <DialogContent title="Concluded" description="Nothing was deleted. Everything below is also saved in the idea's conclusion history." wide>
      <div className="flex flex-wrap items-center gap-2 text-[13px] text-muted">
        {c && (
          <>
            <StatusBadge status={c.previous_status} />
            <ArrowRight className="h-3.5 w-3.5 text-faint" />
            <StatusBadge status={c.new_status} />
          </>
        )}
        {result.checkpoint && (
          <Link href={`/checkpoints/${result.checkpoint.id}`} className="ml-2 inline-flex items-center gap-1.5 hover:text-fg">
            Final snapshot <CheckpointPill label={result.checkpoint.label} />
          </Link>
        )}
      </div>
      {c && (
        <div className="mt-5 space-y-5">
          <ConclusionSection title="What was learned" md={c.learned} />
          <ConclusionSection title="Decisions" md={c.decisions} />
          <ConclusionSection title="Still unresolved" md={c.unresolved} />
        </div>
      )}
      {(result.artifacts?.length ?? 0) > 0 && (
        <div className="mt-5">
          <h3 className="mb-2 text-[12px] font-semibold uppercase tracking-wider text-muted">Generated</h3>
          <ul className="space-y-1">
            {result.artifacts.map((a) =>
              a.artifact ? (
                <li key={a.artifact.id}>
                  <Link href={`/artifacts/${a.artifact.id}`} className="flex items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-[13.5px] hover:bg-surface-2">
                    <FileText className="h-3.5 w-3.5 text-k-artifact" /> <span className="text-fg">{a.artifact.title}</span>
                    <span className="text-[12px] text-faint">{artifactTypeName(a.artifact.type)}</span>
                  </Link>
                </li>
              ) : null,
            )}
          </ul>
        </div>
      )}
      <div className="mt-6 flex justify-end border-t border-border pt-4">
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      </div>
    </DialogContent>
  );
}

export function ConclusionSection({ title, md }: { title: string; md: string }) {
  if (!md) return null;
  return (
    <section>
      <h3 className="mb-1 text-[12px] font-semibold uppercase tracking-wider text-muted">{title}</h3>
      <div className="text-[14px]">
        <Markdown>{md}</Markdown>
      </div>
    </section>
  );
}
