"use client";

import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { cn, shortDate } from "@/lib/format";
import type { Artifact, ArtifactResult } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent } from "@/components/ui/overlay";
import { Field, Spinner, Textarea } from "@/components/ui/primitives";
import { generatorInfo } from "./artifact-meta";
import { ProgressBar, Segmented, Select } from "./kit";
import { useIdeaBranches, useIdeaCheckpoints } from "./queries";

type Source = "branch" | "checkpoint";

/** Regenerate the artifact from current thinking (or a checkpoint) as a new version. */
export function RegenerateDialog({ artifact, open, onOpenChange }: { artifact: Artifact; open: boolean; onOpenChange: (o: boolean) => void }) {
  const qc = useQueryClient();
  const [instructions, setInstructions] = useState("");
  const [source, setSource] = useState<Source>(artifact.checkpoint_id ? "checkpoint" : "branch");
  const [branchId, setBranchId] = useState(artifact.branch_id ?? "");
  const [checkpointId, setCheckpointId] = useState(artifact.checkpoint_id ?? "");
  const [generator, setGenerator] = useState<"auto" | "template">("auto");
  const branches = useIdeaBranches(artifact.idea_id, open);
  const checkpoints = useIdeaCheckpoints(artifact.idea_id, open && source === "checkpoint");

  const cps = [...(checkpoints.data ?? [])].sort((a, b) => b.number - a.number);
  const branchValue = branchId || branches.data?.find((b) => b.is_default)?.id || "";
  const cpValue = checkpointId || cps.find((c) => c.branch_id === (artifact.branch_id ?? branchValue))?.id || cps[0]?.id || "";

  const regen = useMutation({
    mutationFn: () =>
      api.post<ArtifactResult>(`/v1/artifacts/${artifact.id}/regenerate`, {
        instructions: instructions.trim() || undefined,
        branch_id: source === "branch" ? branchValue || undefined : undefined,
        checkpoint_id: source === "checkpoint" ? cpValue || undefined : undefined,
        generator: generator === "template" ? "template" : undefined,
      }),
    onSuccess: (r) => {
      if (r.artifact) qc.setQueryData(["artifact", artifact.id], r.artifact);
      void qc.invalidateQueries({ queryKey: ["artifact", artifact.id] });
      void qc.invalidateQueries({ queryKey: ["artifacts"] });
      toast.success(`Regenerated as v${r.version?.version ?? artifact.current_version + 1}`, { description: generatorInfo(r.version?.generator).detail });
      setInstructions("");
      onOpenChange(false);
    },
    onError: (e) => toast.error("Regeneration failed", { description: errorMessage(e) }),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Regenerate artifact"
        description={`Rebuilds “${artifact.title}” from recorded thinking as v${artifact.current_version + 1}. The current version stays in history.`}
      >
        {regen.isPending ? (
          <RegenerateProgress nextVersion={artifact.current_version + 1} />
        ) : (
          <form
            className="space-y-5"
            onSubmit={(e) => {
              e.preventDefault();
              regen.mutate();
            }}
          >
            <Field label="Instructions (optional)" htmlFor="regen-instructions" hint={artifact.instructions ? `Leave empty to reuse: “${artifact.instructions}”` : "e.g. “Shorter, and focus on the first 30 days”"}>
              <Textarea id="regen-instructions" rows={3} value={instructions} onChange={(e) => setInstructions(e.target.value)} placeholder="What should be different this time?" />
            </Field>

            <div>
              <p className="mb-1.5 text-[13px] font-medium text-muted">Build from</p>
              <Segmented<Source>
                label="Build from"
                value={source}
                onChange={setSource}
                options={[
                  { value: "branch", label: "Latest thinking" },
                  { value: "checkpoint", label: "A checkpoint" },
                ]}
              />
              <div className="mt-3">
                {source === "branch" ? (
                  <Select aria-label="Branch" value={branchValue} onChange={(e) => setBranchId(e.target.value)} disabled={branches.isLoading}>
                    {branches.isLoading && <option>Loading branches…</option>}
                    {branches.data?.map((b) => (
                      <option key={b.id} value={b.id}>
                        {b.name}
                        {b.is_default ? " (default)" : ""} · {b.knowledge_count} items
                      </option>
                    ))}
                  </Select>
                ) : checkpoints.isLoading ? (
                  <div className="flex h-9 items-center gap-2 text-[13px] text-muted">
                    <Spinner className="h-3.5 w-3.5" /> Loading checkpoints…
                  </div>
                ) : cps.length ? (
                  <Select aria-label="Checkpoint" value={cpValue} onChange={(e) => setCheckpointId(e.target.value)}>
                    {cps.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.label} · {c.title} — {c.branch_name ?? "branch"}, {shortDate(c.created_at)}
                      </option>
                    ))}
                  </Select>
                ) : (
                  <p className="text-[13px] text-muted">This idea has no checkpoints yet — use the latest thinking instead.</p>
                )}
              </div>
            </div>

            <div>
              <p className="mb-1.5 text-[13px] font-medium text-muted">Generator</p>
              <Segmented<"auto" | "template">
                label="Generator"
                value={generator}
                onChange={setGenerator}
                options={[
                  { value: "auto", label: "Best available" },
                  { value: "template", label: "Template only (no AI)" },
                ]}
              />
              <p className="mt-1.5 text-[12px] text-faint">“Best available” uses your AI model when one is configured, and falls back to the deterministic template.</p>
            </div>

            <div className="flex justify-end gap-2 border-t border-border pt-4">
              <DialogClose asChild>
                <Button type="button" variant="ghost">
                  Cancel
                </Button>
              </DialogClose>
              <Button type="submit" variant="primary" disabled={source === "checkpoint" && !cpValue}>
                <RefreshCw className="h-3.5 w-3.5" /> Regenerate
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

const PHASES = [
  { at: 0, label: "Gathering the recorded thinking" },
  { at: 2, label: "Writing the document" },
  { at: 25, label: "Still writing — longer documents take a while" },
];

function RegenerateProgress({ nextVersion }: { nextVersion: number }) {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    const start = Date.now();
    const t = setInterval(() => setElapsed(Math.floor((Date.now() - start) / 1000)), 500);
    return () => clearInterval(t);
  }, []);
  const phase = [...PHASES].reverse().find((p) => elapsed >= p.at) ?? PHASES[0];
  const steps = [
    { label: "Gather recorded thinking", done: elapsed >= 2 },
    { label: "Write the document", done: false },
    { label: `Save as v${nextVersion} with provenance`, done: false },
  ];
  return (
    <div className="py-2" aria-live="polite">
      <div className="flex items-center gap-3">
        <Spinner />
        <p className="text-sm font-medium text-fg">{phase.label}…</p>
        <span className="ml-auto font-mono text-[12px] text-faint">{elapsed}s</span>
      </div>
      <ProgressBar className="mt-4" label="Regenerating" />
      <ol className="mt-5 space-y-2 text-[13px]">
        {steps.map((s, i) => {
          const active = !s.done && (i === 0 || steps[i - 1].done);
          return (
            <li key={s.label} className={cn("flex items-center gap-2.5", s.done ? "text-muted" : active ? "text-fg" : "text-faint")}>
              <span className={cn("flex h-4 w-4 items-center justify-center rounded-full border", s.done ? "border-success bg-success text-white" : active ? "border-accent" : "border-border-strong")}>
                {s.done ? <Check className="h-2.5 w-2.5" strokeWidth={3} /> : active ? <span className="h-1.5 w-1.5 animate-pulse-soft rounded-full bg-accent" /> : null}
              </span>
              {s.label}
            </li>
          );
        })}
      </ol>
      <p className="mt-5 text-[12px] leading-relaxed text-faint">With an AI model this can take up to a minute. You can close this dialog — the new version appears here when it&apos;s ready.</p>
    </div>
  );
}
