// Fork dialog logic: source checkpoint defaults and the `selections` payload for
// POST /v1/checkpoints/{id}/fork. The API applies `kinds` only when a selection has no
// `item_ids`, so kinds and hand-picked items must be sent as separate selections.
import type { Checkpoint, KnowledgeItem, Selection } from "@/lib/types";
import { isLive } from "@/features/knowledge/lib";

/** "Research" = evidence + insights — the usual thing to carry into a fork from later thinking. */
export const RESEARCH_KINDS = ["evidence", "insight"] as const;

export function sortCheckpointsDesc(cps: Checkpoint[]): Checkpoint[] {
  return [...cps].sort((a, b) => b.number - a.number);
}

/** Default fork source: the checkpoint being viewed, else the latest on the current branch, else the latest overall. */
export function defaultForkSource(cps: Checkpoint[], ctx: { viewingCheckpointId?: string | null; branchId?: string | null }): Checkpoint | null {
  if (ctx.viewingCheckpointId) {
    const v = cps.find((c) => c.id === ctx.viewingCheckpointId);
    if (v) return v;
  }
  const sorted = sortCheckpointsDesc(cps);
  return sorted.find((c) => c.branch_id === ctx.branchId) ?? sorted[0] ?? null;
}

/** Checkpoints taken after the source (any branch) — candidates for "bring later knowledge". */
export function laterCheckpoints(cps: Checkpoint[], source: Checkpoint | null): Checkpoint[] {
  if (!source) return [];
  return sortCheckpointsDesc(cps).filter((c) => c.number > source.number);
}

/** Items whose lineage is already in the source snapshot would be skipped by the API. */
export function lineagesOf(items: KnowledgeItem[] | null | undefined): Set<string> {
  return new Set((items ?? []).map((i) => i.lineage_id));
}

export interface ForkSelectionDraft {
  laterCheckpointId: string | null;
  kinds: string[];
  itemIds: string[];
  /** Snapshot items of the later checkpoint (to drop items already covered by a selected kind). */
  laterItems?: KnowledgeItem[];
}

export function buildForkSelections(d: ForkSelectionDraft): Selection[] {
  if (!d.laterCheckpointId) return [];
  const kinds = [...new Set(d.kinds)];
  const byId = new Map((d.laterItems ?? []).map((i) => [i.id, i]));
  const covered = (id: string) => {
    const it = byId.get(id);
    // Kind selections bring every *live* item of that kind; non-live items must be sent explicitly.
    return Boolean(it && kinds.includes(it.kind) && isLive(it));
  };
  const itemIds = [...new Set(d.itemIds)].filter((id) => !covered(id));
  const out: Selection[] = [];
  if (kinds.length) out.push({ checkpoint_id: d.laterCheckpointId, kinds });
  if (itemIds.length) out.push({ checkpoint_id: d.laterCheckpointId, item_ids: itemIds });
  return out;
}

/** How many items a draft would bring (approximate: excludes lineages already in the source). */
export function countSelected(d: ForkSelectionDraft, sourceLineages: Set<string>): number {
  const items = d.laterItems ?? [];
  const ids = new Set<string>();
  for (const it of items) {
    if (sourceLineages.has(it.lineage_id)) continue;
    if ((d.kinds.includes(it.kind) && isLive(it)) || d.itemIds.includes(it.id)) ids.add(it.id);
  }
  return ids.size;
}
