"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown, Inbox, X } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { KnowledgeItem } from "@/lib/types";
import { entityMeta, tone } from "@/lib/entities";
import { cn, plural } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { OriginTag } from "@/components/ui/badges";
import { RefLabel } from "@/components/domain/knowledge";
import { UntrustedLabel } from "@/features/ideas/ui";

/**
 * Proposed knowledge (from imports / extraction) awaiting review. Nothing proposed counts as
 * knowledge until it's accepted — accept or reject individually or all at once.
 */
export function ProposalsReview({ ideaId, items, onOpenItem }: { ideaId: string; items: KnowledgeItem[]; onOpenItem: (id: string) => void }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [pendingIds, setPendingIds] = useState<string[]>([]);
  const review = useMutation({
    mutationFn: (v: { ids: string[]; accept: boolean }) => api.post<{ items: KnowledgeItem[] }>("/v1/knowledge/review", v),
    onMutate: (v) => setPendingIds(v.ids),
    onSuccess: (_, v) => {
      toast.success(`${plural(v.ids.length, "item")} ${v.accept ? "accepted" : "rejected"}`);
      void qc.invalidateQueries({ queryKey: ["idea", ideaId] });
      void qc.invalidateQueries({ queryKey: ["ideas"] });
      void qc.invalidateQueries({ queryKey: ["knowledge"] });
    },
    onError: (e) => toast.error(errorMessage(e)),
    onSettled: () => setPendingIds([]),
  });
  if (!items.length) return null;
  const all = items.map((i) => i.id);
  const busy = (id: string) => review.isPending && pendingIds.includes(id);

  return (
    <section className="overflow-hidden rounded-[var(--radius-lg)] border border-warning/30 bg-warning-soft/60" aria-label="Proposed knowledge">
      <div className="flex flex-wrap items-center gap-3 px-4 py-3">
        <Inbox className="h-4 w-4 shrink-0 text-warning" />
        <p className="min-w-0 flex-1 text-[13.5px] text-fg">
          <span className="font-medium">{plural(items.length, "item")} extracted from imports await review.</span>{" "}
          <span className="text-muted">They don&apos;t count as knowledge until you accept them.</span>
        </p>
        <Button size="sm" variant={open ? "ghost" : "secondary"} onClick={() => setOpen((v) => !v)} aria-expanded={open}>
          {open ? "Hide" : "Review"} <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-180")} />
        </Button>
      </div>
      {open && (
        <div className="border-t border-warning/20 bg-surface animate-fade-in">
          <div className="flex flex-wrap items-center justify-end gap-2 border-b border-border px-4 py-2">
            <Button size="xs" variant="ghost" className="text-danger" onClick={() => review.mutate({ ids: all, accept: false })} disabled={review.isPending}>
              Reject all
            </Button>
            <Button size="xs" variant="primary" onClick={() => review.mutate({ ids: all, accept: true })} loading={review.isPending && pendingIds.length === all.length} disabled={review.isPending}>
              <Check className="h-3 w-3" /> Accept all
            </Button>
          </div>
          <ul className="divide-y divide-border">
            {items.map((it) => (
              <li key={it.id} className={cn("flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-start", busy(it.id) && "opacity-50")}>
                <button type="button" onClick={() => onOpenItem(it.id)} className="flex min-w-0 flex-1 items-start gap-3 text-left">
                  <RefLabel kind={it.kind} label={it.label} />
                  <div className="min-w-0 flex-1">
                    <p className="text-[14px] leading-snug text-fg hover:text-accent">{it.statement}</p>
                    <div className="mt-1 flex flex-wrap items-center gap-2">
                      <span className="text-[11px] font-medium uppercase tracking-wide" style={{ color: tone(it.kind) }}>
                        {entityMeta(it.kind).label}
                      </span>
                      <OriginTag origin={it.origin} />
                      {it.source_conversation_id && <UntrustedLabel />}
                      {typeof it.confidence === "number" && <span className="text-[11px] text-faint">{Math.round(it.confidence * 100)}% confident</span>}
                    </div>
                    {it.source_excerpt && it.source_excerpt !== it.statement && (
                      <p className="mt-1.5 line-clamp-2 border-l-2 border-border-strong pl-2.5 text-[12.5px] text-muted">{it.source_excerpt}</p>
                    )}
                  </div>
                </button>
                <div className="flex shrink-0 gap-1.5 self-end sm:self-start">
                  <Button size="sm" variant="secondary" onClick={() => review.mutate({ ids: [it.id], accept: false })} disabled={review.isPending} aria-label={`Reject ${it.label}`}>
                    <X className="h-3.5 w-3.5" /> Reject
                  </Button>
                  <Button size="sm" variant="primary" onClick={() => review.mutate({ ids: [it.id], accept: true })} disabled={review.isPending} aria-label={`Accept ${it.label}`}>
                    <Check className="h-3.5 w-3.5" /> Accept
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
