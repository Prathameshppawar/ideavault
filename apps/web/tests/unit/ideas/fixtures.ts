import type { Branch, Checkpoint, JourneyNode, KnowledgeItem } from "@/lib/types";

let n = 0;
const uid = () => `00000000-0000-4000-8000-${String(++n).padStart(12, "0")}`;

export function item(p: Partial<KnowledgeItem> & { kind: string }): KnowledgeItem {
  const id = p.id ?? uid();
  const prefix: Record<string, string> = { decision: "D", assumption: "A", evidence: "E", insight: "I", question: "Q", action: "T" };
  const ref = p.ref_number ?? 1;
  return {
    id,
    idea_id: "idea-1",
    branch_id: "b-main",
    ref_number: ref,
    label: `${prefix[p.kind] ?? "K"}${ref}`,
    lineage_id: id,
    statement: `${p.kind} ${ref}`,
    details: "",
    status: "ACTIVE",
    origin: "SOURCE",
    review_state: "ACCEPTED",
    source_excerpt: "",
    created_by: "user",
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-01T10:00:00Z",
    ...p,
  } as KnowledgeItem;
}

export function branch(p: Partial<Branch> & { id: string }): Branch {
  return {
    idea_id: "idea-1",
    name: p.id,
    slug: p.id,
    description: "",
    status: "ACTIVE",
    is_default: false,
    checkpoint_count: 0,
    knowledge_count: 0,
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-01T10:00:00Z",
    ...p,
  } as Branch;
}

export function checkpoint(p: Partial<Checkpoint> & { id: string; number: number; branch_id: string }): Checkpoint {
  return {
    idea_id: "idea-1",
    label: `CP${p.number}`,
    title: `Checkpoint ${p.number}`,
    summary: "",
    kind: "manual",
    content_hash: "h",
    created_by: "user",
    created_at: `2026-10-0${Math.min(p.number, 9)}T10:00:00Z`,
    ...p,
  } as Checkpoint;
}

export function jnode(p: Partial<JourneyNode> & { id: string; kind: string; at: string }): JourneyNode {
  return { title: p.id, ...p } as JourneyNode;
}
