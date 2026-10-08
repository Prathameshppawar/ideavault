// Pure logic for reviewing an external-AI delta: grouping, selection and merge semantics.
import type { DeltaItem } from "@/lib/types";
import type { Tone } from "@/features/artifacts/artifact-meta";

export const DELTA_CLASSES = ["NEW", "CHANGED", "REJECTED", "UNCHANGED"] as const;
export type DeltaClass = (typeof DELTA_CLASSES)[number];

export interface DeltaGroup {
  cls: DeltaClass;
  items: DeltaItem[];
}

export const DELTA_CLASS_META: Record<DeltaClass, { title: string; blurb: string; tone: Tone }> = {
  NEW: { title: "New", blurb: "Not in your recorded thinking yet.", tone: "success" },
  CHANGED: { title: "Changed", blurb: "Revises an item you already recorded.", tone: "warning" },
  REJECTED: { title: "Rejected", blurb: "Argues an existing item is wrong or should be dropped.", tone: "danger" },
  UNCHANGED: { title: "Unchanged", blurb: "Restates what you already have.", tone: "faint" },
};

function normalizeClass(c: string): DeltaClass {
  const up = (c || "").toUpperCase();
  return (DELTA_CLASSES as readonly string[]).includes(up) ? (up as DeltaClass) : "NEW";
}

/** Always returns the four groups in review order (some may be empty), items by position. */
export function groupDeltaItems(items: DeltaItem[] | undefined | null): DeltaGroup[] {
  const sorted = [...(items ?? [])].sort((a, b) => a.position - b.position);
  return DELTA_CLASSES.map((cls) => ({ cls, items: sorted.filter((i) => normalizeClass(i.classification) === cls) }));
}

export function deltaCounts(items: DeltaItem[] | undefined | null): Record<DeltaClass, number> {
  const out: Record<DeltaClass, number> = { NEW: 0, CHANGED: 0, REJECTED: 0, UNCHANGED: 0 };
  for (const i of items ?? []) out[normalizeClass(i.classification)]++;
  return out;
}

/** Selection defaults come from the analyzer (`selected`): everything except UNCHANGED. */
export function initialSelection(items: DeltaItem[] | undefined | null): Set<string> {
  return new Set((items ?? []).filter((i) => i.selected).map((i) => i.id));
}

export function toggleItem(sel: ReadonlySet<string>, id: string, on?: boolean): Set<string> {
  const next = new Set(sel);
  const want = on ?? !next.has(id);
  if (want) next.add(id);
  else next.delete(id);
  return next;
}

export type GroupState = "all" | "some" | "none";

export function groupSelectionState(sel: ReadonlySet<string>, items: DeltaItem[]): GroupState {
  if (!items.length) return "none";
  const n = items.filter((i) => sel.has(i.id)).length;
  return n === 0 ? "none" : n === items.length ? "all" : "some";
}

export function setGroupSelection(sel: ReadonlySet<string>, items: DeltaItem[], on: boolean): Set<string> {
  const next = new Set(sel);
  for (const i of items) {
    if (on) next.add(i.id);
    else next.delete(i.id);
  }
  return next;
}

/** Item ids to send to the merge endpoint, in review order. */
export function mergePayload(items: DeltaItem[], sel: ReadonlySet<string>): { item_ids: string[] } {
  return { item_ids: groupDeltaItems(items).flatMap((g) => g.items.filter((i) => sel.has(i.id)).map((i) => i.id)) };
}

const REJECTION_EFFECT: Record<string, string> = {
  decision: "reverses",
  question: "answers",
  assumption: "invalidates",
  insight: "retracts",
  evidence: "disputes",
  action: "drops",
};

/** What merging this item will do — mirrors the server's never-rewrite-history merge rules. */
export function mergeEffect(item: DeltaItem): string {
  const target = item.target_label ? item.target_label : "the original";
  switch (normalizeClass(item.classification)) {
    case "NEW":
      return `Records a new ${item.kind}.`;
    case "CHANGED":
      return item.target_item_id ? `Records a new version that supersedes ${target}; ${target} stays in history.` : `Records a new ${item.kind}.`;
    case "REJECTED": {
      if (!item.target_item_id) return `Records a new ${item.kind}.`;
      const verb = REJECTION_EFFECT[item.kind] ?? "challenges";
      return item.kind === "decision" || item.kind === "question"
        ? `${capitalize(verb)} ${target}.`
        : `Records challenging evidence and ${verb} ${target}.`;
    }
    case "UNCHANGED":
      return item.target_item_id ? `Adds supporting evidence to ${target}.` : "Nothing new to record.";
  }
}

function capitalize(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

export const DELTA_STATUS_META: Record<string, { label: string; tone: Tone }> = {
  PENDING_REVIEW: { label: "Awaiting review", tone: "warning" },
  MERGED: { label: "Merged", tone: "success" },
  PARTIALLY_MERGED: { label: "Partially merged", tone: "success" },
  DISCARDED: { label: "Discarded", tone: "faint" },
};

export function deltaStatusMeta(status: string): { label: string; tone: Tone } {
  return DELTA_STATUS_META[status] ?? { label: status.toLowerCase().replace(/_/g, " "), tone: "muted" };
}

export const isPendingDelta = (status: string) => status === "PENDING_REVIEW";
