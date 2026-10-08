"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation } from "@tanstack/react-query";
import { FileText, GitBranch, Sparkles, Wand2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Artifact, ArtifactResult, Branch, Checkpoint } from "@/lib/types";
import { cn, fullDate, humanize, shortDate, truncate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badges";
import { Dialog, DialogContent } from "@/components/ui/overlay";
import { EmptyState, Field, Input, Label, Textarea } from "@/components/ui/primitives";
import { CheckpointPill, Segmented, Select } from "../ui";
import { useElapsed, useRefreshIdea } from "../hooks";
import { ALL_ARTIFACT_TYPES, ARTIFACT_INFO, artifactTypeName, generatorLabel } from "../lib/artifacts";
import { sortCheckpointsDesc } from "../lib/fork";

export function ArtifactsTab({
  ideaId,
  artifacts,
  branch,
  checkpoints,
  viewingCpId,
}: {
  ideaId: string;
  artifacts: Artifact[];
  branch: Branch | undefined;
  checkpoints: Checkpoint[];
  viewingCpId: string | null;
}) {
  const [open, setOpen] = useState(false);
  const sorted = [...artifacts].sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime());
  return (
    <div>
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <p className="min-w-0 flex-1 text-[13px] text-muted">Documents generated from this idea&apos;s recorded thinking — each traceable to the knowledge it used.</p>
        <Button size="sm" variant="primary" onClick={() => setOpen(true)}>
          <Wand2 className="h-3.5 w-3.5" /> Generate artifact
        </Button>
      </div>
      {sorted.length === 0 ? (
        <EmptyState
          icon={FileText}
          title="No artifacts yet"
          description="Turn your thinking into an action plan, a spec, a decision memo or a prompt for a coding agent."
          action={
            <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>
              <Wand2 className="h-3.5 w-3.5" /> Generate the first one
            </Button>
          }
        />
      ) : (
        <ul className="divide-y divide-border border-y border-border">
          {sorted.map((a) => {
            const g = generatorLabel(a.generator);
            return (
              <li key={a.id}>
                <Link href={`/artifacts/${a.id}`} className="group flex items-start gap-3 px-1 py-3.5 transition-colors hover:bg-surface-2/50">
                  <FileText className="mt-0.5 h-4 w-4 shrink-0 text-k-artifact" aria-hidden />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="min-w-0 text-[14.5px] font-medium text-fg group-hover:text-accent [overflow-wrap:anywhere]">{a.title}</span>
                      <span className="text-[11px] font-medium uppercase tracking-wide text-k-artifact">{artifactTypeName(a.type)}</span>
                    </div>
                    <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-faint">
                      <span className="font-mono text-muted">v{a.current_version}</span>
                      <span className="inline-flex items-center gap-1" title={g.detail}>
                        {g.ai && <Sparkles className="h-3 w-3 text-k-insight" aria-hidden />}
                        {g.label}
                        {g.ai && g.detail ? <span className="max-w-[160px] truncate">· {g.detail}</span> : null}
                      </span>
                      {a.checkpoint_label ? (
                        <span className="inline-flex items-center gap-1">
                          from <CheckpointPill label={a.checkpoint_label} />
                        </span>
                      ) : a.branch_name ? (
                        <span className="inline-flex items-center gap-1">
                          <GitBranch className="h-3 w-3" /> {a.branch_name}
                        </span>
                      ) : null}
                      {a.status !== "DRAFT" && <Badge>{humanize(a.status)}</Badge>}
                      <span title={fullDate(a.updated_at)}>updated {shortDate(a.updated_at)}</span>
                    </p>
                  </div>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
      <GenerateArtifactDialog open={open} onOpenChange={setOpen} ideaId={ideaId} branch={branch} checkpoints={checkpoints} defaultCheckpointId={viewingCpId} />
    </div>
  );
}

export function GenerateArtifactDialog({
  open,
  onOpenChange,
  ideaId,
  branch,
  checkpoints,
  defaultCheckpointId,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  ideaId: string;
  branch: Branch | undefined;
  checkpoints: Checkpoint[];
  defaultCheckpointId: string | null;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
      }}
    >
      {open && <GenerateForm ideaId={ideaId} branch={branch} checkpoints={checkpoints} defaultCheckpointId={defaultCheckpointId} onClose={() => onOpenChange(false)} />}
    </Dialog>
  );
}

function GenerateForm({
  ideaId,
  branch,
  checkpoints,
  defaultCheckpointId,
  onClose,
}: {
  ideaId: string;
  branch: Branch | undefined;
  checkpoints: Checkpoint[];
  defaultCheckpointId: string | null;
  onClose: () => void;
}) {
  const router = useRouter();
  const refresh = useRefreshIdea();
  const sortedCps = sortCheckpointsDesc(checkpoints);
  const [type, setType] = useState<string>("ACTION_PLAN");
  const [title, setTitle] = useState("");
  const [instructions, setInstructions] = useState("");
  const [source, setSource] = useState<"branch" | "checkpoint">(defaultCheckpointId ? "checkpoint" : "branch");
  const [cpId, setCpId] = useState<string>(defaultCheckpointId ?? sortedCps.find((c) => c.branch_id === branch?.id)?.id ?? sortedCps[0]?.id ?? "");
  const gen = useMutation({
    mutationFn: () =>
      api.post<ArtifactResult>("/v1/artifacts/generate", {
        idea_id: ideaId,
        type,
        title: title.trim() || undefined,
        instructions: instructions.trim() || undefined,
        branch_id: source === "branch" ? branch?.id : undefined,
        checkpoint_id: source === "checkpoint" ? cpId : undefined,
      }),
    onSuccess: (r) => {
      toast.success(`${artifactTypeName(type)} generated`);
      void refresh(ideaId);
      onClose();
      if (r.artifact) router.push(`/artifacts/${r.artifact.id}`);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const elapsed = useElapsed(gen.isPending);

  return (
    <DialogContent title="Generate artifact" description="Built only from recorded knowledge — every claim is traceable to the decisions, evidence and questions it came from." wide>
      <form
        className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault();
          if (type === "CUSTOM" && !instructions.trim()) {
            toast.error("Describe what the custom document should contain.");
            return;
          }
          gen.mutate();
        }}
      >
        <fieldset disabled={gen.isPending} className="space-y-5">
          <div>
            <Label>Type</Label>
            <div className="grid max-h-[300px] grid-cols-1 gap-1.5 overflow-y-auto pr-1 sm:grid-cols-2" role="radiogroup" aria-label="Artifact type">
              {ALL_ARTIFACT_TYPES.map((t) => {
                const on = t === type;
                return (
                  <button
                    key={t}
                    type="button"
                    role="radio"
                    aria-checked={on}
                    onClick={() => setType(t)}
                    className={cn(
                      "rounded-[var(--radius-md)] border px-3 py-2 text-left transition-colors",
                      on ? "border-k-artifact bg-k-artifact-soft" : "border-border hover:border-border-strong hover:bg-surface-2",
                    )}
                  >
                    <span className={cn("block text-[13px] font-medium", on ? "text-k-artifact" : "text-fg")}>{ARTIFACT_INFO[t].name}</span>
                    <span className="block text-[11.5px] leading-snug text-faint">{ARTIFACT_INFO[t].description}</span>
                  </button>
                );
              })}
            </div>
          </div>
          <div>
            <Label>Generate from</Label>
            <div className="flex flex-wrap items-center gap-3">
              <Segmented
                label="Source"
                value={source}
                onChange={setSource}
                options={[
                  { value: "branch", label: `Live ${branch?.name ?? "branch"}` },
                  { value: "checkpoint", label: "A checkpoint" },
                ]}
              />
              {source === "checkpoint" &&
                (sortedCps.length ? (
                  <Select value={cpId} onChange={(e) => setCpId(e.target.value)} className="min-w-[220px] flex-1" aria-label="Checkpoint">
                    {sortedCps.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.label} · {truncate(c.title, 50)} — {c.branch_name}
                      </option>
                    ))}
                  </Select>
                ) : (
                  <span className="text-[12.5px] text-faint">No checkpoints yet.</span>
                ))}
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-[1fr_1.4fr]">
            <Field label="Title" htmlFor="art-title" hint="Optional">
              <Input id="art-title" value={title} onChange={(e) => setTitle(e.target.value)} placeholder={ARTIFACT_INFO[type as keyof typeof ARTIFACT_INFO]?.name} />
            </Field>
            <Field label="Instructions" htmlFor="art-instr" hint={type === "CUSTOM" ? "Required for a custom document." : "Audience, tone, what to emphasise."}>
              <Textarea id="art-instr" value={instructions} onChange={(e) => setInstructions(e.target.value)} rows={3} placeholder="e.g. For a technical co-founder; keep it to one page." />
            </Field>
          </div>
        </fieldset>
        <div className="flex flex-wrap items-center justify-end gap-3 border-t border-border pt-4">
          {gen.isPending && (
            <span className="mr-auto text-[12.5px] text-muted" aria-live="polite">
              Generating {artifactTypeName(type).toLowerCase()}… {elapsed}s{elapsed > 8 ? " — this can take up to a minute" : ""}
            </span>
          )}
          <Button type="button" variant="ghost" onClick={onClose} disabled={gen.isPending}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={gen.isPending} disabled={source === "checkpoint" && !cpId}>
            <Wand2 className="h-4 w-4" /> Generate
          </Button>
        </div>
      </form>
    </DialogContent>
  );
}
