"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, ArrowRightLeft, Ban, Bookmark, ChevronDown, Circle, CircleCheck, Equal, GitMerge, MessagesSquare, Plus, X } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api, errorMessage } from "@/lib/api";
import { cn, fullDate, plural, timeAgo } from "@/lib/format";
import type { Checkpoint, Conversation, Delta, DeltaItem, KnowledgeItem, MergeDeltaResult, Message } from "@/lib/types";
import { EntityChip, KindTag } from "@/components/ui/badges";
import { KnowledgeRow, RefLabel } from "@/components/domain/knowledge";
import { Button } from "@/components/ui/button";
import { Checkbox, Dialog, DialogClose, DialogContent } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Skeleton, SkeletonLines } from "@/components/ui/primitives";
import { analyzerLabel } from "@/features/artifacts/artifact-meta";
import { BackLink, Callout, LinkButton, Pill, TriCheckbox, toneVars } from "@/features/artifacts/kit";
import {
  DELTA_CLASS_META,
  deltaCounts,
  deltaStatusMeta,
  groupDeltaItems,
  groupSelectionState,
  initialSelection,
  isPendingDelta,
  mergeEffect,
  mergePayload,
  setGroupSelection,
  toggleItem,
  type DeltaClass,
} from "./delta-logic";

const CLASS_ICON: Record<DeltaClass, React.ComponentType<{ className?: string }>> = { NEW: Plus, CHANGED: ArrowRightLeft, REJECTED: Ban, UNCHANGED: Equal };
const PROVIDERS: Record<string, string> = { chatgpt: "ChatGPT", claude: "Claude", gemini: "Gemini", external: "External AI" };

interface ConvDetail {
  conversation: Conversation;
  messages: Message[];
}

export function DeltaScreen() {
  const { id } = useParams<{ id: string }>();
  const qc = useQueryClient();
  const { data: delta, isLoading, error, refetch } = useQuery({ queryKey: ["delta", id], queryFn: () => api.get<Delta>(`/v1/deltas/${id}`) });
  const conv = useQuery({
    queryKey: ["conversation-meta", delta?.conversation_id],
    queryFn: () => api.get<ConvDetail>(`/v1/conversations/${delta?.conversation_id}`, { query: { limit: 1 } }),
    enabled: !!delta?.conversation_id,
  });
  const [selection, setSelection] = useState<Set<string> | null>(null);
  const [mergeResult, setMergeResult] = useState<MergeDeltaResult | null>(null);
  const [showUnchanged, setShowUnchanged] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);

  const items = useMemo(() => delta?.items ?? [], [delta?.items]);
  const groups = useMemo(() => groupDeltaItems(items), [items]);
  const counts = deltaCounts(items);
  const sel = selection ?? initialSelection(items);
  const pending = !!delta && isPendingDelta(delta.status);

  const hasMerged = items.some((i) => i.merged_item_id);
  const mergeCp = useQuery({
    queryKey: ["checkpoint-meta", delta?.merge_checkpoint_id],
    queryFn: () => api.get<Checkpoint>(`/v1/checkpoints/${delta?.merge_checkpoint_id}`),
    enabled: !!delta?.merge_checkpoint_id && !mergeResult,
    staleTime: 5 * 60_000,
  });
  const mergedLookup = useQuery({
    queryKey: ["knowledge", { idea_id: delta?.idea_id, branch_id: delta?.branch_id, include_history: true, limit: 1000 }],
    queryFn: () => api.get<{ items: KnowledgeItem[] }>("/v1/knowledge", { query: { idea_id: delta?.idea_id, branch_id: delta?.branch_id, include_history: true, limit: 1000 } }),
    enabled: !!delta && !pending && hasMerged && !mergeResult,
    select: (d) => new Map(d.items.map((k) => [k.id, k])),
  });
  const mergedById = useMemo(() => {
    if (mergeResult) return new Map(mergeResult.merged.map((k) => [k.id, k]));
    return mergedLookup.data ?? new Map<string, KnowledgeItem>();
  }, [mergeResult, mergedLookup.data]);

  const invalidateIdea = () => {
    void qc.invalidateQueries({ queryKey: ["deltas"] });
    if (delta) {
      void qc.invalidateQueries({ queryKey: ["idea", delta.idea_id] });
      void qc.invalidateQueries({ queryKey: ["knowledge"] });
    }
  };
  const merge = useMutation({
    mutationFn: () => api.post<MergeDeltaResult>(`/v1/deltas/${id}/merge`, mergePayload(items, sel)),
    onSuccess: (r) => {
      setMergeResult(r);
      if (r.delta) qc.setQueryData(["delta", id], r.delta);
      invalidateIdea();
      toast.success(`Merged ${plural(r.merged.length, "change")}${r.checkpoint ? ` at ${r.checkpoint.label}` : ""}`);
      window.scrollTo({ top: 0, behavior: "smooth" });
    },
    onError: (e) => toast.error("Merge failed", { description: errorMessage(e) }),
  });
  const discard = useMutation({
    mutationFn: () => api.post<Delta>(`/v1/deltas/${id}/discard`),
    onSuccess: (d) => {
      qc.setQueryData(["delta", id], d);
      setConfirmDiscard(false);
      invalidateIdea();
      toast.success("Discarded — nothing was merged");
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  if (isLoading) return <DeltaSkeleton />;
  if (error || !delta) {
    return (
      <div className="mx-auto max-w-3xl px-5 py-16 sm:px-8">
        {error instanceof ApiError && error.status === 404 ? (
          <EmptyState title="This review doesn't exist" action={<LinkButton href="/deltas">All external conversations</LinkButton>} />
        ) : (
          <ErrorState error={error} onRetry={() => void refetch()} />
        )}
      </div>
    );
  }

  const c = conv.data?.conversation;
  const providerName = c ? (PROVIDERS[c.provider] ?? c.provider) : "";
  const status = deltaStatusMeta(delta.status);
  const selectedCount = items.filter((i) => sel.has(i.id)).length;
  const mergedItems = mergeResult?.merged ?? items.filter((i) => i.merged_item_id).map((i) => mergedById.get(i.merged_item_id!)).filter((k): k is KnowledgeItem => !!k);
  const mergeCheckpointId = mergeResult?.checkpoint?.id ?? delta.merge_checkpoint_id;
  const mergeCheckpointLabel = mergeResult?.checkpoint?.label ?? mergeCp.data?.label;
  const title = c?.title?.replace(/^External:\s*/i, "") || "External conversation";

  return (
    <div className="mx-auto max-w-4xl px-5 pb-10 pt-8 sm:px-8 sm:pt-10">
      <BackLink href="/deltas">External conversations</BackLink>

      <header className="mt-5">
        <div className="mb-2 flex flex-wrap items-center gap-2.5">
          <span className="text-[11px] font-semibold uppercase tracking-wider text-faint">{providerName ? `${providerName} conversation` : "External conversation"}</span>
          <Pill tone={status.tone}>{status.label}</Pill>
        </div>
        <h1 className="font-display text-[2.1rem] leading-[1.08] text-fg sm:text-[2.5rem]">Review what changed</h1>
        <div className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1.5 text-[13px] text-muted">
          {conv.isLoading ? (
            <Skeleton className="h-4 w-64" />
          ) : (
            <>
              {delta.conversation_id && <EntityChip type="conversation" id={delta.conversation_id} label={title} title={title} className="max-w-[18rem]" />}
              <span className="text-faint">against</span>
              <EntityChip type="idea" id={delta.idea_id} label={c?.idea_title || "Idea"} />
              {c?.branch_name && <EntityChip type="branch" id={delta.branch_id} ideaId={delta.idea_id} label={c.branch_name} />}
              {delta.context_pack_id && <EntityChip type="context_pack" id={delta.context_pack_id} label="Context pack" />}
            </>
          )}
          <span className="text-faint" aria-hidden>
            ·
          </span>
          <span title={fullDate(delta.created_at)}>{timeAgo(delta.created_at)}</span>
        </div>
      </header>

      <div className="mt-6 rounded-[var(--radius-lg)] border border-dashed border-k-insight/40 px-4 py-3">
        <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-k-insight">Interpretation · {analyzerLabel(delta.analyzer) || "analysis"}</p>
        <p className="text-[14px] leading-relaxed text-fg">{delta.summary}</p>
        <p className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[12px]">
          {(Object.keys(counts) as DeltaClass[]).map((k) => (
            <span key={k} style={{ color: counts[k] ? toneVars(DELTA_CLASS_META[k].tone).fg : "var(--text-faint)" }}>
              {counts[k]} {DELTA_CLASS_META[k].title.toLowerCase()}
            </span>
          ))}
        </p>
      </div>

      {mergedItems.length > 0 || mergeResult ? (
        <MergedSummary items={mergedItems} checkpointId={mergeCheckpointId} checkpointLabel={mergeCheckpointLabel} ideaId={delta.idea_id} />
      ) : delta.status === "DISCARDED" ? (
        <Callout tone="info" className="mt-6" title={`Discarded${delta.resolved_at ? ` ${timeAgo(delta.resolved_at)}` : ""} — nothing was merged.`}>
          The conversation stays stored as an external source{delta.conversation_id ? " and can still be read" : ""}.
        </Callout>
      ) : (
        <Callout tone="untrusted" className="mt-6" title="From an external conversation — untrusted.">
          Excerpts are quoted verbatim from {providerName || "the other AI"}. Nothing is merged until you select changes and merge them; history is never rewritten.
          {delta.conversation_id && (
            <>
              {" "}
              <Link href={`/conversations/${delta.conversation_id}`} className="text-fg underline decoration-border-strong underline-offset-2 hover:decoration-fg">
                Read the full conversation
              </Link>
            </>
          )}
        </Callout>
      )}

      {items.length === 0 ? (
        <EmptyState
          className="mt-8"
          icon={MessagesSquare}
          title="No changes found"
          description="Nothing in this conversation looked like a new or revised decision, assumption, insight, question or action."
          action={
            pending && (
              <Button variant="secondary" size="sm" onClick={() => discard.mutate()} loading={discard.isPending}>
                Close this review
              </Button>
            )
          }
        />
      ) : (
        <div className="mt-8 space-y-10">
          {groups
            .filter((g) => g.items.length > 0)
            .map((g) => {
              const meta = DELTA_CLASS_META[g.cls];
              const Icon = CLASS_ICON[g.cls];
              const tone = toneVars(meta.tone);
              const collapsed = g.cls === "UNCHANGED" && !showUnchanged;
              const state = groupSelectionState(sel, g.items);
              return (
                <section key={g.cls} aria-labelledby={`grp-${g.cls}`}>
                  <div className="flex flex-wrap items-center gap-3 border-b border-border pb-2.5">
                    {pending && !collapsed && <TriCheckbox state={state} onChange={(on) => setSelection(setGroupSelection(sel, g.items, on))} label={`Select all ${meta.title.toLowerCase()} items`} />}
                    <span className="flex h-6 w-6 items-center justify-center rounded-full" style={{ background: tone.bg, color: tone.fg }}>
                      <Icon className="h-3.5 w-3.5" />
                    </span>
                    <h2 id={`grp-${g.cls}`} className="text-[15px] font-semibold text-fg">
                      {meta.title} <span className="ml-0.5 font-normal text-faint">{g.items.length}</span>
                    </h2>
                    <span className="hidden text-[13px] text-muted sm:inline">{meta.blurb}</span>
                    {g.cls === "UNCHANGED" && (
                      <button type="button" onClick={() => setShowUnchanged((v) => !v)} className="ml-auto inline-flex items-center gap-1 text-[12px] font-medium text-muted hover:text-fg" aria-expanded={!collapsed}>
                        {collapsed ? "Show" : "Hide"} <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", !collapsed && "rotate-180")} />
                      </button>
                    )}
                  </div>
                  {!collapsed && (
                    <ul className="animate-fade-in">
                      {g.items.map((it) => (
                        <DeltaItemRow
                          key={it.id}
                          item={it}
                          pending={pending}
                          checked={sel.has(it.id)}
                          onToggle={() => setSelection(toggleItem(sel, it.id))}
                          merged={it.merged_item_id ? mergedById.get(it.merged_item_id) : undefined}
                          resolvedMerged={!!it.merged_item_id}
                        />
                      ))}
                    </ul>
                  )}
                </section>
              );
            })}
        </div>
      )}

      {pending && items.length > 0 && (
        <div className="sticky bottom-0 z-30 -mx-5 mt-8 border-t border-border bg-bg sm:-mx-8">
          <div className="flex flex-wrap items-center gap-3 px-5 py-3 sm:px-8">
            <p className="text-[13px] text-muted" aria-live="polite">
              <span className="font-medium text-fg">{selectedCount}</span> of {items.length} selected
            </p>
            <div className="ml-auto flex items-center gap-2">
              <Button variant="ghost" onClick={() => setConfirmDiscard(true)} disabled={merge.isPending}>
                Discard
              </Button>
              <Button variant="primary" onClick={() => merge.mutate()} disabled={selectedCount === 0} loading={merge.isPending}>
                <GitMerge className="h-4 w-4" /> Merge {selectedCount} selected
              </Button>
            </div>
          </div>
        </div>
      )}

      <Dialog open={confirmDiscard} onOpenChange={setConfirmDiscard}>
        <DialogContent title="Discard this review?" description="Nothing will be merged. The external conversation stays stored as a source, so you can read it later.">
          <div className="flex justify-end gap-2">
            <DialogClose asChild>
              <Button variant="ghost">Keep reviewing</Button>
            </DialogClose>
            <Button variant="danger" onClick={() => discard.mutate()} loading={discard.isPending}>
              <X className="h-3.5 w-3.5" /> Discard
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Confidence({ value }: { value?: number }) {
  if (value === undefined || value === null) return null;
  const pct = Math.round(Math.max(0, Math.min(1, value)) * 100);
  const low = pct < 50;
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-[11px]", low ? "text-warning" : "text-faint")} title="How sure the analyzer is about this classification">
      <span className="h-1 w-10 overflow-hidden rounded-full bg-surface-3">
        <span className="block h-full rounded-full bg-current" style={{ width: `${pct}%` }} />
      </span>
      {pct}% confidence
    </span>
  );
}

function DeltaItemRow({
  item: it,
  pending,
  checked,
  onToggle,
  merged,
  resolvedMerged,
}: {
  item: DeltaItem;
  pending: boolean;
  checked: boolean;
  onToggle: () => void;
  merged?: KnowledgeItem;
  resolvedMerged: boolean;
}) {
  const cls = it.classification.toUpperCase() as DeltaClass;
  const cbId = `di-${it.id}`;
  return (
    <li className={cn("flex gap-3 border-b border-border py-4 last:border-0", !pending && !resolvedMerged && "opacity-70")}>
      <div className="pt-0.5">
        {pending ? (
          <Checkbox id={cbId} checked={checked} onCheckedChange={onToggle} aria-label={`Include: ${it.statement}`} />
        ) : resolvedMerged ? (
          <CircleCheck className="h-4 w-4 text-success" aria-label="Merged" />
        ) : (
          <Circle className="h-4 w-4 text-faint" aria-label="Not merged" />
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="mb-1.5 flex flex-wrap items-center gap-x-3 gap-y-1">
          <KindTag kind={it.kind} />
          <Confidence value={it.confidence} />
        </div>

        {cls === "CHANGED" && it.target_label ? (
          <div className="overflow-hidden rounded-[var(--radius-md)] border border-border text-[14px]">
            <div className="flex items-start gap-2.5 bg-surface-2 px-3 py-2">
              <span className="w-11 shrink-0 pt-0.5 text-[10px] font-semibold uppercase tracking-wider text-faint">Before</span>
              <RefLabel kind={it.kind} label={it.target_label} />
              <span className="min-w-0 text-muted">{it.target_statement}</span>
            </div>
            <div className="flex items-start gap-2.5 border-t border-border bg-surface px-3 py-2" style={{ boxShadow: "inset 2px 0 0 var(--warning)" }}>
              <span className="w-11 shrink-0 pt-0.5 text-[10px] font-semibold uppercase tracking-wider text-warning">After</span>
              <label htmlFor={pending ? cbId : undefined} className="min-w-0 cursor-pointer text-fg">
                {it.statement}
              </label>
            </div>
          </div>
        ) : cls === "REJECTED" && it.target_label ? (
          <div className="text-[14px]">
            <p className="flex items-start gap-2.5">
              <RefLabel kind={it.kind} label={it.target_label} className="opacity-70" />
              <span className="min-w-0 text-muted line-through decoration-danger/60">{it.target_statement}</span>
            </p>
            <p className="mt-2 text-fg">
              <span className="font-medium text-danger">Rejected because </span>
              <label htmlFor={pending ? cbId : undefined} className="cursor-pointer">
                {it.statement}
              </label>
            </p>
          </div>
        ) : cls === "UNCHANGED" && it.target_label ? (
          <div className="text-[14px]">
            <label htmlFor={pending ? cbId : undefined} className="cursor-pointer text-muted">
              {it.statement}
            </label>
            <p className="mt-1.5 flex items-start gap-2 text-[12.5px] text-faint">
              <span className="pt-0.5">Matches</span> <RefLabel kind={it.kind} label={it.target_label} /> <span className="min-w-0">{it.target_statement}</span>
            </p>
          </div>
        ) : (
          <label htmlFor={pending ? cbId : undefined} className="block cursor-pointer text-[15px] leading-snug text-fg">
            {it.statement}
          </label>
        )}

        {it.rationale && <p className="mt-1.5 text-[13px] text-muted">because {it.rationale}</p>}
        {it.details && <p className="mt-1 text-[13px] text-muted">{it.details}</p>}

        {it.source_excerpt && it.source_excerpt !== it.statement && (
          <blockquote className="mt-2.5 border-l-2 border-warning/40 pl-3 text-[13px] leading-relaxed text-muted">
            <span className="mb-0.5 block text-[10px] font-semibold uppercase not-italic tracking-wider text-warning">Excerpt · untrusted</span>
            <span className="italic">“{it.source_excerpt}”</span>
          </blockquote>
        )}
        {it.source_excerpt && it.source_excerpt === it.statement && (
          <p className="mt-1.5 text-[10px] font-semibold uppercase tracking-wider text-warning">Quoted verbatim · untrusted</p>
        )}

        <p className="mt-2 text-[12px] text-faint">
          {pending ? (
            mergeEffect(it)
          ) : resolvedMerged ? (
            <span className="inline-flex flex-wrap items-center gap-1.5 text-success">
              Merged
              {merged ? (
                <>
                  {" "}
                  as <EntityChip type={merged.kind} id={merged.id} label={merged.label} title={merged.statement} />
                </>
              ) : it.merged_item_id ? (
                <Link href={`/knowledge/${it.merged_item_id}`} className="inline-flex items-center gap-0.5 underline underline-offset-2">
                  view recorded item <ArrowRight className="h-3 w-3" />
                </Link>
              ) : null}
            </span>
          ) : (
            "Not merged"
          )}
        </p>
      </div>
    </li>
  );
}

function MergedSummary({ items, checkpointId, checkpointLabel, ideaId }: { items: KnowledgeItem[]; checkpointId?: string; checkpointLabel?: string; ideaId: string }) {
  return (
    <section className="mt-6 animate-slide-up rounded-[var(--radius-lg)] border border-success/30 bg-success-soft px-4 py-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <CircleCheck className="h-4 w-4 text-success" aria-hidden />
        <p className="text-[14px] font-medium text-fg">Merged {plural(items.length, "change")} into your thinking</p>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {checkpointId && (
            <LinkButton href={`/checkpoints/${checkpointId}`} size="sm">
              <Bookmark className="h-3.5 w-3.5" /> {checkpointLabel ? `Checkpoint ${checkpointLabel}` : "Merge checkpoint"}
            </LinkButton>
          )}
          <LinkButton href={`/ideas/${ideaId}`} size="sm">
            Open idea <ArrowRight className="h-3.5 w-3.5" />
          </LinkButton>
        </div>
      </div>
      {items.length > 0 && (
        <div className="-mx-2 mt-3 rounded-[var(--radius-md)] bg-surface/70">
          {items.map((k) => (
            <KnowledgeRow key={k.id} item={k} href={`/knowledge/${k.id}`} showKind />
          ))}
        </div>
      )}
      <p className="mt-3 text-[12px] text-muted">Earlier versions stay in history — merged changes supersede, never overwrite.</p>
    </section>
  );
}

function DeltaSkeleton() {
  return (
    <div className="mx-auto max-w-4xl px-5 pt-8 sm:px-8 sm:pt-10" aria-busy="true" aria-label="Loading review">
      <Skeleton className="h-3.5 w-40" />
      <Skeleton className="mt-6 h-10 w-1/2" />
      <Skeleton className="mt-4 h-4 w-2/3" />
      <Skeleton className="mt-6 h-20 w-full" />
      <div className="mt-8">
        <SkeletonLines lines={6} />
      </div>
    </div>
  );
}

