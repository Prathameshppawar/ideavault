"use client";

import { useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Check, GitBranch } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { KNOWLEDGE_KINDS, type Checkpoint, type ForkResult, type InheritanceRecord, type KnowledgeItem } from "@/lib/types";
import { entityMeta, tone } from "@/lib/entities";
import { cn, plural, shortDate, truncate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Checkbox, Dialog, DialogContent, Switch } from "@/components/ui/overlay";
import { Field, Input, Label, Spinner } from "@/components/ui/primitives";
import { RefLabel } from "@/components/domain/knowledge";
import { isLive } from "@/features/knowledge/lib";
import { Chip, CheckpointPill, Select } from "./ui";
import { useCheckpoint, useRefreshIdea } from "./hooks";
import { buildForkSelections, countSelected, laterCheckpoints, lineagesOf, RESEARCH_KINDS, sortCheckpointsDesc } from "./lib/fork";

export function ForkDialog({
  open,
  onOpenChange,
  ideaId,
  checkpoints,
  defaultSourceId,
  onForked,
  onDone,
  onCreateCheckpoint,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  ideaId: string;
  checkpoints: Checkpoint[];
  defaultSourceId: string | null;
  /** Called with the new branch once created (e.g. to switch to it). */
  onForked?: (r: ForkResult) => void;
  /** Called by the result view's primary button (defaults to closing). */
  onDone?: (r: ForkResult) => void;
  /** Offered when there is no checkpoint to fork from yet. */
  onCreateCheckpoint?: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && (
        <ForkForm
          key={defaultSourceId ?? "none"}
          ideaId={ideaId}
          checkpoints={checkpoints}
          defaultSourceId={defaultSourceId}
          onForked={onForked}
          onDone={onDone}
          onCreateCheckpoint={onCreateCheckpoint}
          onClose={() => onOpenChange(false)}
        />
      )}
    </Dialog>
  );
}

function ForkForm({
  ideaId,
  checkpoints,
  defaultSourceId,
  onForked,
  onDone,
  onCreateCheckpoint,
  onClose,
}: {
  ideaId: string;
  checkpoints: Checkpoint[];
  defaultSourceId: string | null;
  onForked?: (r: ForkResult) => void;
  onDone?: (r: ForkResult) => void;
  onCreateCheckpoint?: () => void;
  onClose: () => void;
}) {
  const refresh = useRefreshIdea();
  const sorted = useMemo(() => sortCheckpointsDesc(checkpoints), [checkpoints]);
  const [sourceId, setSourceId] = useState<string>(defaultSourceId ?? sorted[0]?.id ?? "");
  const source = sorted.find((c) => c.id === sourceId) ?? null;
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [bringLater, setBringLater] = useState(false);
  const later = useMemo(() => laterCheckpoints(checkpoints, source), [checkpoints, source]);
  const [laterIdState, setLaterId] = useState<string>("");
  const laterId = later.some((c) => c.id === laterIdState) ? laterIdState : (later[0]?.id ?? "");
  const [kinds, setKinds] = useState<string[]>([...RESEARCH_KINDS]);
  const [itemIds, setItemIds] = useState<string[]>([]);
  const [result, setResult] = useState<ForkResult | null>(null);

  const sourceCp = useCheckpoint(sourceId || null);
  const laterCp = useCheckpoint(bringLater && laterId ? laterId : null);
  const sourceLineages = useMemo(() => lineagesOf(sourceCp.data?.snapshot?.items), [sourceCp.data]);
  const laterItems = useMemo(() => laterCp.data?.snapshot?.items ?? [], [laterCp.data]);
  const draft = { laterCheckpointId: bringLater ? laterId || null : null, kinds, itemIds, laterItems };
  const selections = buildForkSelections(draft);
  const selectedCount = bringLater ? countSelected(draft, sourceLineages) : 0;

  const fork = useMutation({
    mutationFn: () =>
      api.post<ForkResult>(`/v1/checkpoints/${sourceId}/fork`, {
        name: name.trim() || undefined,
        description: description.trim() || undefined,
        selections: selections.length ? selections : undefined,
      }),
    onSuccess: (r) => {
      setResult(r);
      toast.success(`Branch “${r.branch?.name ?? "new branch"}” created`);
      void refresh(ideaId);
      onForked?.(r);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  if (result) return <ForkResultView result={result} checkpoints={checkpoints} onClose={onDone ? () => onDone(result) : onClose} doneLabel={onDone ? "Open the new branch" : "Done"} />;

  if (!sorted.length) {
    return (
      <DialogContent title="Fork a new direction">
        <p className="text-[14px] text-muted">
          Forks start from an immutable checkpoint, so the original line of thinking is never touched. Create a checkpoint first, then fork from it.
        </p>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant={onCreateCheckpoint ? "ghost" : "secondary"} onClick={onClose}>
            Close
          </Button>
          {onCreateCheckpoint && (
            <Button variant="primary" onClick={onCreateCheckpoint}>
              Create checkpoint
            </Button>
          )}
        </div>
      </DialogContent>
    );
  }

  const toggleKind = (k: string) => setKinds((ks) => (ks.includes(k) ? ks.filter((x) => x !== k) : [...ks, k]));
  const toggleItem = (id: string) => setItemIds((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]));

  return (
    <DialogContent
      title="Fork a new direction"
      description="A branch starts from a checkpoint exactly as it was. The original branch is untouched — you can always compare or switch back."
      wide
    >
      <form
        className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault();
          if (sourceId) fork.mutate();
        }}
      >
        <Field label="Start from" htmlFor="fork-source">
          <Select
            id="fork-source"
            value={sourceId}
            onChange={(e) => {
              setSourceId(e.target.value);
              setItemIds([]);
            }}
          >
            {sorted.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label} · {truncate(c.title, 60)} — {c.branch_name ?? "branch"}, {shortDate(c.created_at)}
              </option>
            ))}
          </Select>
          <p className="mt-1.5 text-[12px] text-faint">
            {sourceCp.isLoading ? "Loading snapshot…" : sourceCp.data ? `Starts with ${plural(sourceCp.data.snapshot?.items?.length ?? 0, "item")} from ${sourceCp.data.label}.` : null}
          </p>
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Branch name" htmlFor="fork-name">
            <Input id="fork-name" value={name} onChange={(e) => setName(e.target.value)} placeholder={source ? `Fork of ${source.label}` : "New direction"} maxLength={200} autoFocus />
          </Field>
          <Field label="What are you exploring?" htmlFor="fork-desc">
            <Input id="fork-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />
          </Field>
        </div>

        <div className="rounded-[var(--radius-lg)] border border-border">
          <label className="flex cursor-pointer items-start justify-between gap-4 px-4 py-3">
            <span>
              <span className="block text-[14px] font-medium text-fg">Bring selected knowledge from later thinking</span>
              <span className="mt-0.5 block text-[12.5px] text-muted">
                e.g. keep the research from a later checkpoint while dropping the decisions that came after {source?.label ?? "it"}.
              </span>
            </span>
            <Switch checked={bringLater} onCheckedChange={setBringLater} disabled={!later.length} aria-label="Bring later knowledge" />
          </label>
          {!later.length && <p className="border-t border-border px-4 py-2.5 text-[12.5px] text-faint">There are no checkpoints after {source?.label ?? "this one"} yet.</p>}
          {bringLater && later.length > 0 && (
            <div className="space-y-4 border-t border-border px-4 py-4 animate-fade-in">
              <Field label="From checkpoint" htmlFor="fork-later">
                <Select
                  id="fork-later"
                  value={laterId}
                  onChange={(e) => {
                    setLaterId(e.target.value);
                    setItemIds([]);
                  }}
                >
                  {later.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.label} · {truncate(c.title, 60)} — {c.branch_name ?? "branch"}
                    </option>
                  ))}
                </Select>
              </Field>
              <div>
                <div className="mb-1.5 flex flex-wrap items-center justify-between gap-2">
                  <Label className="mb-0">Bring every live item of these kinds</Label>
                  <div className="flex gap-1">
                    <Button type="button" size="xs" variant="ghost" onClick={() => setKinds([...RESEARCH_KINDS])}>
                      Research
                    </Button>
                    <Button type="button" size="xs" variant="ghost" onClick={() => setKinds([])}>
                      None
                    </Button>
                  </div>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {KNOWLEDGE_KINDS.map((k) => (
                    <Chip key={k} active={kinds.includes(k)} tone={k} onClick={() => toggleKind(k)}>
                      {entityMeta(k).plural}
                    </Chip>
                  ))}
                </div>
              </div>
              <div>
                <Label>Or pick specific items</Label>
                {laterCp.isLoading ? (
                  <div className="flex items-center gap-2 py-3 text-[13px] text-muted">
                    <Spinner /> Loading {later.find((c) => c.id === laterId)?.label}…
                  </div>
                ) : laterItems.length === 0 ? (
                  <p className="py-2 text-[13px] text-faint">That checkpoint has no knowledge items.</p>
                ) : (
                  <ul className="max-h-64 divide-y divide-border overflow-y-auto rounded-[var(--radius-md)] border border-border">
                    {[...laterItems]
                      .sort((a, b) => KNOWLEDGE_KINDS.indexOf(a.kind as never) - KNOWLEDGE_KINDS.indexOf(b.kind as never) || a.ref_number - b.ref_number)
                      .map((it) => (
                        <PickRow
                          key={it.id}
                          item={it}
                          included={sourceLineages.has(it.lineage_id)}
                          viaKind={kinds.includes(it.kind) && isLive(it)}
                          checked={itemIds.includes(it.id)}
                          onToggle={() => toggleItem(it.id)}
                          sourceLabel={source?.label}
                        />
                      ))}
                  </ul>
                )}
              </div>
              <p className="text-[12.5px] text-muted" aria-live="polite">
                {selectedCount ? `Will bring ${plural(selectedCount, "item")} from ${later.find((c) => c.id === laterId)?.label}.` : "Nothing selected from later thinking yet."}
              </p>
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-border pt-4">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={fork.isPending} disabled={!sourceId}>
            <GitBranch className="h-4 w-4" /> Fork from {source?.label ?? "checkpoint"}
          </Button>
        </div>
      </form>
    </DialogContent>
  );
}

function PickRow({
  item,
  included,
  viaKind,
  checked,
  onToggle,
  sourceLabel,
}: {
  item: KnowledgeItem;
  included: boolean;
  viaKind: boolean;
  checked: boolean;
  onToggle: () => void;
  sourceLabel?: string;
}) {
  const disabled = included || viaKind;
  const id = `pick-${item.id}`;
  return (
    <li className={cn("flex items-start gap-3 px-3 py-2", disabled && "opacity-60")}>
      <Checkbox id={id} checked={included || viaKind || checked} disabled={disabled} onCheckedChange={onToggle} className="mt-0.5" />
      <label htmlFor={id} className={cn("flex min-w-0 flex-1 items-start gap-2.5", !disabled && "cursor-pointer")}>
        <RefLabel kind={item.kind} label={item.label} />
        <span className="min-w-0 flex-1">
          <span className={cn("block text-[13.5px] leading-snug text-fg", !isLive(item) && "text-faint line-through")}>{item.statement}</span>
          {(included || viaKind) && (
            <span className="mt-0.5 block text-[11px] text-faint">{included ? `Already in ${sourceLabel ?? "the source checkpoint"}` : `Included via ${entityMeta(item.kind).plural.toLowerCase()}`}</span>
          )}
        </span>
      </label>
    </li>
  );
}

function ForkResultView({ result, checkpoints, onClose, doneLabel }: { result: ForkResult; checkpoints: Checkpoint[]; onClose: () => void; doneLabel: string }) {
  const cpLabel = new Map(checkpoints.map((c) => [c.id, c.label]));
  const groups = new Map<string, InheritanceRecord[]>();
  for (const r of result.inherited ?? []) {
    const list = groups.get(r.via) ?? [];
    list.push(r);
    groups.set(r.via, list);
  }
  const VIA: Record<string, string> = {
    checkpoint: `From ${result.from_checkpoint?.label ?? "the checkpoint"} (as it was then)`,
    selection: "Selected from later thinking",
    merge: "Merged in",
  };
  return (
    <DialogContent title={`“${result.branch?.name ?? "Branch"}” is ready`} description="Here's exactly what the new branch inherited — nothing else was copied." wide>
      <div className="flex flex-wrap gap-x-6 gap-y-2 text-[13px]">
        <span className="inline-flex items-center gap-1.5 text-muted">
          <Check className="h-3.5 w-3.5 text-success" /> {plural(result.from_checkpoint_items, "item")} from {result.from_checkpoint?.label}
        </span>
        <span className="inline-flex items-center gap-1.5 text-muted">
          <Check className="h-3.5 w-3.5 text-success" /> {plural(result.selected_items, "selected item")}
        </span>
        {result.linked_entities > 0 && (
          <span className="inline-flex items-center gap-1.5 text-muted">
            <Check className="h-3.5 w-3.5 text-success" /> {plural(result.linked_entities, "linked conversation/artifact", "linked conversations/artifacts")}
          </span>
        )}
        {result.base_checkpoint && (
          <span className="inline-flex items-center gap-1.5 text-muted">
            Base snapshot <CheckpointPill label={result.base_checkpoint.label} />
          </span>
        )}
      </div>
      <div className="mt-5 max-h-[42vh] space-y-5 overflow-y-auto">
        {[...groups.entries()].map(([via, recs]) => (
          <section key={via}>
            <h3 className="mb-1.5 text-[12px] font-semibold uppercase tracking-wider text-muted">{VIA[via] ?? via}</h3>
            <ul className="space-y-1">
              {recs.map((r) => (
                <li key={r.id} className="flex items-start gap-2.5 text-[13.5px]">
                  {r.label ? <RefLabel kind={r.entity_type} label={r.label} /> : <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: tone(r.entity_type) }} />}
                  <span className="min-w-0 flex-1 text-fg">{r.title || entityMeta(r.entity_type).label}</span>
                  {r.source_checkpoint_id && <CheckpointPill label={cpLabel.get(r.source_checkpoint_id) ?? "CP"} muted />}
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      <div className="mt-5 flex justify-end border-t border-border pt-4">
        <Button variant="primary" onClick={onClose}>
          {doneLabel}
        </Button>
      </div>
    </DialogContent>
  );
}
