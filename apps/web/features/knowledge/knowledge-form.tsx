"use client";

import { useState, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { KNOWLEDGE_KINDS, type KnowledgeItem, type KnowledgeKind } from "@/lib/types";
import { entityMeta } from "@/lib/entities";
import { cn, truncate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTrigger } from "@/components/ui/overlay";
import { Field, Input, Label, Textarea } from "@/components/ui/primitives";
import { Segmented, Select } from "@/features/ideas/ui";
import { useRefreshIdea } from "@/features/ideas/hooks";
import { buildKnowledgePayload, emptyKnowledgeForm, isLive, KIND_BLURB, type BuildResult, type KnowledgeFormState, type Level } from "./lib";

const LEVELS: { value: Level; label: string }[] = [
  { value: "LOW", label: "Low" },
  { value: "MEDIUM", label: "Medium" },
  { value: "HIGH", label: "High" },
];

const STATEMENT_HINT: Record<KnowledgeKind, string> = {
  decision: "We will… / We won't…",
  assumption: "We believe that…",
  evidence: "A fact, measurement or finding",
  insight: "What you now understand",
  question: "What still needs an answer?",
  action: "The next concrete step",
};

/** The editable fields of a knowledge item (kind-specific). Used by "Add knowledge" and "Supersede…". */
export function KnowledgeFields({
  form,
  set,
  errors,
  targets,
  kindLocked,
  supersede,
  idPrefix = "kf",
}: {
  form: KnowledgeFormState;
  set: (patch: Partial<KnowledgeFormState>) => void;
  errors: BuildResult["errors"];
  /** Items evidence can support/challenge. */
  targets?: KnowledgeItem[];
  kindLocked?: boolean;
  supersede?: boolean;
  idPrefix?: string;
}) {
  const id = (s: string) => `${idPrefix}-${s}`;
  return (
    <div className="space-y-4">
      {!kindLocked && (
        <div>
          <Label>Kind</Label>
          <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-3" role="radiogroup" aria-label="Kind">
            {KNOWLEDGE_KINDS.map((k) => {
              const on = form.kind === k;
              return (
                <button
                  key={k}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  onClick={() => set({ kind: k })}
                  className={cn(
                    "rounded-[var(--radius-md)] border px-2.5 py-2 text-left transition-colors",
                    on ? "border-transparent" : "border-border hover:border-border-strong hover:bg-surface-2",
                  )}
                  style={on ? { background: `var(--k-${k}-soft)`, boxShadow: `inset 0 0 0 1px var(--k-${k})` } : undefined}
                >
                  <span className="flex items-center gap-1.5 text-[13px] font-medium" style={{ color: on ? `var(--k-${k})` : undefined }}>
                    <span className="h-1.5 w-1.5 rounded-full" style={{ background: `var(--k-${k})` }} aria-hidden />
                    {entityMeta(k).label}
                  </span>
                  <span className="mt-0.5 block text-[11.5px] leading-snug text-faint">{KIND_BLURB[k]}</span>
                </button>
              );
            })}
          </div>
        </div>
      )}

      <Field label={supersede ? "New statement" : "Statement"} htmlFor={id("statement")} error={errors.statement}>
        <Textarea id={id("statement")} value={form.statement} onChange={(e) => set({ statement: e.target.value })} rows={2} placeholder={STATEMENT_HINT[form.kind]} autoFocus />
      </Field>

      {form.kind === "decision" && (
        <>
          <Field label={supersede ? "Why it changed" : "Rationale"} htmlFor={id("rationale")} hint="The reasoning is what makes a decision explainable later.">
            <Textarea id={id("rationale")} value={form.rationale} onChange={(e) => set({ rationale: e.target.value })} rows={2} placeholder="Because…" />
          </Field>
          <div>
            <Label>Alternatives considered</Label>
            <div className="space-y-2">
              {form.alternatives.map((a, i) => (
                <div key={i} className="flex items-start gap-2">
                  <div className="grid flex-1 gap-2 sm:grid-cols-2">
                    <Input
                      value={a.option}
                      onChange={(e) => set({ alternatives: form.alternatives.map((x, j) => (j === i ? { ...x, option: e.target.value } : x)) })}
                      placeholder="Option"
                      aria-label={`Alternative ${i + 1}`}
                    />
                    <Input
                      value={a.reason}
                      onChange={(e) => set({ alternatives: form.alternatives.map((x, j) => (j === i ? { ...x, reason: e.target.value } : x)) })}
                      placeholder="Why not"
                      aria-label={`Why alternative ${i + 1} was rejected`}
                    />
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => set({ alternatives: form.alternatives.filter((_, j) => j !== i) })}
                    aria-label={`Remove alternative ${i + 1}`}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
              <Button type="button" variant="ghost" size="sm" onClick={() => set({ alternatives: [...form.alternatives, { option: "", reason: "" }] })}>
                <Plus className="h-3.5 w-3.5" /> Add alternative
              </Button>
            </div>
          </div>
        </>
      )}

      {form.kind === "assumption" && (
        <div className="grid gap-4 sm:grid-cols-[auto_1fr]">
          <div>
            <Label>Risk if wrong</Label>
            <Segmented label="Risk" value={form.risk} onChange={(risk) => set({ risk })} options={LEVELS} />
          </div>
          <Field label="How to validate" htmlFor={id("vm")}>
            <Input id={id("vm")} value={form.validationMethod} onChange={(e) => set({ validationMethod: e.target.value })} placeholder="e.g. Interview 10 organisers" />
          </Field>
        </div>
      )}

      {form.kind === "evidence" && (
        <>
          <div className="flex flex-wrap gap-4">
            <div>
              <Label>Stance</Label>
              <Segmented
                label="Stance"
                value={form.stance}
                onChange={(stance) => set({ stance })}
                options={[
                  { value: "SUPPORTS", label: "Supports" },
                  { value: "CHALLENGES", label: "Challenges" },
                  { value: "NEUTRAL", label: "Neutral" },
                ]}
              />
            </div>
            <div>
              <Label>Strength</Label>
              <Segmented
                label="Strength"
                value={form.strength}
                onChange={(strength) => set({ strength })}
                options={[
                  { value: "WEAK", label: "Weak" },
                  { value: "MODERATE", label: "Moderate" },
                  { value: "STRONG", label: "Strong" },
                ]}
              />
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Link" htmlFor={id("url")} error={errors.url}>
              <Input id={id("url")} type="url" inputMode="url" value={form.url} onChange={(e) => set({ url: e.target.value })} placeholder="https://…" />
            </Field>
            <Field label="Supports or challenges" htmlFor={id("target")}>
              <Select id={id("target")} value={form.targetItemId} onChange={(e) => set({ targetItemId: e.target.value })}>
                <option value="">Nothing specific</option>
                {(targets ?? [])
                  .filter((t) => t.kind !== "evidence" && isLive(t))
                  .map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.label} · {truncate(t.statement, 70)}
                    </option>
                  ))}
              </Select>
            </Field>
          </div>
        </>
      )}

      {form.kind === "insight" && (
        <div>
          <Label>Importance</Label>
          <Segmented label="Importance" value={form.importance} onChange={(importance) => set({ importance })} options={LEVELS} />
        </div>
      )}

      {form.kind === "action" && (
        <div className="flex flex-wrap items-end gap-4">
          <div>
            <Label>Priority</Label>
            <Segmented label="Priority" value={form.priority} onChange={(priority) => set({ priority })} options={LEVELS} />
          </div>
          <Field label="Due" htmlFor={id("due")} error={errors.dueAt}>
            <Input id={id("due")} type="date" value={form.dueAt} onChange={(e) => set({ dueAt: e.target.value })} className="w-44" />
          </Field>
        </div>
      )}

      {supersede && form.kind !== "decision" && (
        <Field label="Why it changed" htmlFor={id("why")}>
          <Input id={id("why")} value={form.rationale} onChange={(e) => set({ rationale: e.target.value })} placeholder="Optional" />
        </Field>
      )}

      <Field label="Details" htmlFor={id("details")}>
        <Textarea id={id("details")} value={form.details} onChange={(e) => set({ details: e.target.value })} rows={2} placeholder="Optional context" />
      </Field>

      <div>
        <Label>Where it comes from</Label>
        <Segmented
          label="Origin"
          value={form.origin}
          onChange={(origin) => set({ origin })}
          options={[
            { value: "SOURCE", label: "Source — I'm stating it" },
            { value: "INTERPRETATION", label: "Interpretation — an inference" },
          ]}
        />
      </div>
    </div>
  );
}

/** "Add knowledge" dialog → POST /v1/knowledge. */
export function AddKnowledgeDialog({
  ideaId,
  branchId,
  branchName,
  targets,
  trigger,
  onCreated,
}: {
  ideaId: string;
  branchId: string | null;
  branchName?: string;
  targets: KnowledgeItem[];
  trigger: ReactNode;
  onCreated?: (item: KnowledgeItem) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      {open && (
        <DialogContent title="Add knowledge" description={branchName ? `Recorded on ${branchName}. Knowledge is never rewritten — later changes supersede it.` : undefined} wide>
          <AddKnowledgeForm
            ideaId={ideaId}
            branchId={branchId}
            targets={targets}
            onDone={(it) => {
              setOpen(false);
              onCreated?.(it);
            }}
          />
        </DialogContent>
      )}
    </Dialog>
  );
}

function AddKnowledgeForm({ ideaId, branchId, targets, onDone }: { ideaId: string; branchId: string | null; targets: KnowledgeItem[]; onDone: (it: KnowledgeItem) => void }) {
  const [form, setForm] = useState<KnowledgeFormState>(() => emptyKnowledgeForm("decision"));
  const [errors, setErrors] = useState<BuildResult["errors"]>({});
  const refresh = useRefreshIdea();
  const save = useMutation({
    mutationFn: (body: unknown) => api.post<KnowledgeItem>("/v1/knowledge", body),
    onSuccess: (it) => {
      toast.success(`${it.label} recorded`);
      void refresh(ideaId);
      onDone(it);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        const r = buildKnowledgePayload(form, { ideaId, branchId });
        setErrors(r.errors);
        if (r.payload) save.mutate(r.payload);
      }}
    >
      <KnowledgeFields form={form} set={(p) => setForm((f) => ({ ...f, ...p }))} errors={errors} targets={targets} />
      <div className="mt-6 flex justify-end gap-2 border-t border-border pt-4">
        <Button type="submit" variant="primary" loading={save.isPending}>
          Record {entityMeta(form.kind).label.toLowerCase()}
        </Button>
      </div>
    </form>
  );
}
