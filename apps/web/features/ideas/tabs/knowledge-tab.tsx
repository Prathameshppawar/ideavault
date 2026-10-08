"use client";

import { useMemo, useState } from "react";
import { Plus, Search, X } from "lucide-react";
import { KNOWLEDGE_KINDS, type Checkpoint, type KnowledgeItem, type KnowledgeKind } from "@/lib/types";
import { entityMeta } from "@/lib/entities";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/overlay";
import { EmptyState, Input } from "@/components/ui/primitives";
import { KnowledgeSections } from "@/features/knowledge/knowledge-sections";
import { AddKnowledgeDialog } from "@/features/knowledge/knowledge-form";
import { isLive } from "@/features/knowledge/lib";
import { Chip } from "../ui";

export function KnowledgeTab({
  ideaId,
  branchId,
  branchName,
  items,
  viewingCp,
  onSelectItem,
  canEdit,
}: {
  ideaId: string;
  branchId: string | null;
  branchName?: string;
  items: KnowledgeItem[];
  viewingCp: Checkpoint | null;
  onSelectItem: (id: string) => void;
  canEdit: boolean;
}) {
  const [showHistory, setShowHistory] = useState(false);
  const [kind, setKind] = useState<KnowledgeKind | "all">("all");
  const [q, setQ] = useState("");
  const accepted = useMemo(() => items.filter((i) => i.review_state !== "PROPOSED" && i.review_state !== "REJECTED"), [items]);
  const filtered = useMemo(() => {
    const t = q.trim().toLowerCase();
    if (!t) return accepted;
    return accepted.filter((i) => i.statement.toLowerCase().includes(t) || i.label.toLowerCase() === t || i.details?.toLowerCase().includes(t));
  }, [accepted, q]);
  const liveCount = accepted.filter(isLive).length;
  const historyCount = accepted.length - liveCount;
  const counts = useMemo(() => {
    const m: Record<string, number> = {};
    for (const i of accepted) if (showHistory || isLive(i)) m[i.kind] = (m[i.kind] ?? 0) + 1;
    return m;
  }, [accepted, showHistory]);

  return (
    <div>
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <div className="relative min-w-[180px] flex-1 sm:max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" aria-hidden />
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filter knowledge…" className="h-8 pl-8 pr-8" aria-label="Filter knowledge" />
          {q && (
            <button type="button" onClick={() => setQ("")} className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-faint hover:text-fg" aria-label="Clear filter">
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-[13px] text-muted">
          <Switch checked={showHistory} onCheckedChange={setShowHistory} aria-label="Show history" />
          Show history{historyCount > 0 && <span className="text-faint">({historyCount})</span>}
        </label>
        {canEdit && !viewingCp && (
          <div className="ml-auto">
            <AddKnowledgeDialog
              ideaId={ideaId}
              branchId={branchId}
              branchName={branchName}
              targets={accepted}
              onCreated={(it) => onSelectItem(it.id)}
              trigger={
                <Button size="sm" variant="primary">
                  <Plus className="h-3.5 w-3.5" /> Add knowledge
                </Button>
              }
            />
          </div>
        )}
      </div>

      <div className="-mx-4 mb-7 flex gap-1.5 overflow-x-auto px-4 pb-1 sm:mx-0 sm:flex-wrap sm:px-0" role="toolbar" aria-label="Filter by kind">
        <Chip active={kind === "all"} onClick={() => setKind("all")} count={showHistory ? accepted.length : liveCount}>
          All
        </Chip>
        {KNOWLEDGE_KINDS.map((k) => (
          <Chip key={k} active={kind === k} tone={k} onClick={() => setKind(kind === k ? "all" : k)} count={counts[k] ?? 0}>
            {entityMeta(k).plural}
          </Chip>
        ))}
      </div>

      {accepted.length === 0 ? (
        <EmptyState
          title={viewingCp ? `${viewingCp.label} captured no knowledge` : "No knowledge yet"}
          description={
            viewingCp
              ? "This snapshot was taken before anything was recorded on its branch."
              : "Decisions, assumptions, evidence, insights, questions and actions appear here as you think in chat — or add one yourself."
          }
        />
      ) : filtered.length === 0 ? (
        <EmptyState icon={Search} title="Nothing matches" description={`No knowledge mentions “${q.trim()}”.`} />
      ) : (
        <KnowledgeSections
          items={filtered}
          showHistory={showHistory}
          onSelect={(it) => onSelectItem(it.id)}
          kinds={kind === "all" ? KNOWLEDGE_KINDS : [kind]}
          hideEmpty={Boolean(q.trim())}
        />
      )}
    </div>
  );
}
