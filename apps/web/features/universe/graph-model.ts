// Pure transforms from the API graph (GET /v1/graph) to what the canvas renders:
// filtering by node type, sizing, edge styling, legend, neighbourhoods and bounds.
import type { SimulationLinkDatum, SimulationNodeDatum } from "d3-force";
import type { Graph, GraphEdge, GraphNode } from "@/lib/types";

/** Canonical order for type toggles and the legend. */
export const NODE_TYPE_ORDER = [
  "idea",
  "branch",
  "decision",
  "assumption",
  "evidence",
  "insight",
  "question",
  "action",
  "checkpoint",
  "artifact",
  "conversation",
] as const;

const DEAD = new Set(["SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED", "ARCHIVED"]);

export interface UNode extends SimulationNodeDatum {
  id: string;
  type: string;
  label: string;
  title: string;
  ideaId?: string;
  status?: string;
  origin?: string;
  weight: number;
  r: number;
  dead: boolean;
  degree: number;
}

export interface ULink extends SimulationLinkDatum<UNode> {
  id: string;
  type: string;
  structural: boolean;
  origin?: string;
  source: string | UNode;
  target: string | UNode;
}

export interface EdgeStyle {
  label: string;
  /** Wording when read from the target's side, e.g. "Superseded by". */
  inverse: string;
  /** Entity tone (CSS var suffix) or null for neutral. */
  tone: string | null;
  dash: number[] | null;
  directed: boolean;
}

export const EDGE_STYLES: Record<string, EdgeStyle> = {
  supports: { label: "Supports", inverse: "Supported by", tone: "evidence", dash: null, directed: true },
  challenges: { label: "Challenges", inverse: "Challenged by", tone: "question", dash: [5, 3], directed: true },
  contradicts: { label: "Contradicts", inverse: "Contradicted by", tone: "question", dash: [2, 3], directed: true },
  supersedes: { label: "Supersedes", inverse: "Superseded by", tone: "decision", dash: [6, 3], directed: true },
  answers: { label: "Answers", inverse: "Answered by", tone: "action", dash: null, directed: true },
  solves: { label: "Solves", inverse: "Solved by", tone: "action", dash: null, directed: true },
  depends_on: { label: "Depends on", inverse: "Needed by", tone: "checkpoint", dash: [4, 3], directed: true },
  similar_to: { label: "Similar to", inverse: "Similar to", tone: "idea", dash: [2, 4], directed: false },
  evolved_from: { label: "Evolved from", inverse: "Evolved into", tone: "branch", dash: null, directed: true },
  derived_from: { label: "Derived from", inverse: "Source of", tone: "branch", dash: [4, 3], directed: true },
  inspired_by: { label: "Inspired by", inverse: "Inspired", tone: "insight", dash: null, directed: true },
  reuses_lesson_from: { label: "Reuses lesson from", inverse: "Lesson reused by", tone: "insight", dash: [6, 3], directed: true },
  generated_from: { label: "Generated from", inverse: "Generated", tone: "artifact", dash: [2, 3], directed: true },
  merged_into: { label: "Merged into", inverse: "Absorbed", tone: "conversation", dash: null, directed: true },
  related_to: { label: "Related to", inverse: "Related to", tone: null, dash: [2, 3], directed: false },
};

export function edgeStyle(type: string): EdgeStyle {
  if (EDGE_STYLES[type]) return EDGE_STYLES[type];
  const label = type.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
  return { label, inverse: `${label} (from)`, tone: null, dash: [2, 3], directed: true };
}

export function nodeRadius(n: Pick<GraphNode, "type" | "weight">): number {
  const w = Math.max(1, n.weight || 1);
  switch (n.type) {
    case "idea":
      return Math.min(24, 8 + Math.sqrt(w) * 2.6);
    case "branch":
      return Math.min(9, 5 + Math.sqrt(w));
    case "artifact":
    case "conversation":
      return 4.5;
    default:
      return 3.75;
  }
}

/**
 * Build renderable nodes/links: drop hidden types, keep only edges whose endpoints survive,
 * compute radius, degree and "dead" (superseded/reversed…) flags. Previous positions are reused
 * by id so filtering doesn't scramble the layout.
 */
export function buildModel(
  graph: Graph | undefined,
  hidden: ReadonlySet<string>,
  previous?: { get(id: string): { x?: number; y?: number } | undefined },
): { nodes: UNode[]; links: ULink[] } {
  if (!graph) return { nodes: [], links: [] };
  const nodes: UNode[] = [];
  const byId = new Map<string, UNode>();
  for (const n of graph.nodes ?? []) {
    if (hidden.has(n.type)) continue;
    const prev = previous?.get(n.id);
    const u: UNode = {
      id: n.id,
      type: n.type,
      label: n.label,
      title: n.title,
      ideaId: n.idea_id,
      status: n.status,
      origin: n.origin,
      weight: n.weight,
      r: nodeRadius(n),
      dead: !!n.status && DEAD.has(n.status),
      degree: 0,
      ...(prev?.x !== undefined && prev?.y !== undefined ? { x: prev.x, y: prev.y } : {}),
    };
    nodes.push(u);
    byId.set(n.id, u);
  }
  const links: ULink[] = [];
  for (const e of graph.edges ?? []) {
    const s = byId.get(e.source);
    const t = byId.get(e.target);
    if (!s || !t || s === t) continue;
    s.degree++;
    t.degree++;
    links.push({ id: e.id, type: e.type, structural: e.structural, origin: e.origin, source: e.source, target: e.target });
  }
  return { nodes, links };
}

export interface Legend {
  nodes: { type: string; count: number }[];
  edges: { type: string; count: number; style: EdgeStyle }[];
}

/** Node types present (canonical order, unknown types last) and semantic edge types present. */
export function legendFor(graph: Graph | undefined): Legend {
  if (!graph) return { nodes: [], edges: [] };
  const nc = new Map<string, number>();
  for (const n of graph.nodes ?? []) nc.set(n.type, (nc.get(n.type) ?? 0) + 1);
  const order = (t: string) => {
    const i = (NODE_TYPE_ORDER as readonly string[]).indexOf(t);
    return i === -1 ? 999 : i;
  };
  const nodes = [...nc.entries()].map(([type, count]) => ({ type, count })).sort((a, b) => order(a.type) - order(b.type) || a.type.localeCompare(b.type));
  const ec = new Map<string, number>();
  for (const e of graph.edges ?? []) if (!e.structural) ec.set(e.type, (ec.get(e.type) ?? 0) + 1);
  const edges = [...ec.entries()]
    .map(([type, count]) => ({ type, count, style: edgeStyle(type) }))
    .sort((a, b) => b.count - a.count || a.type.localeCompare(b.type));
  return { nodes, edges };
}

/** Ids of a node and everything one hop away. */
export function neighbourhood(links: ULink[], id: string | null): Set<string> | null {
  if (!id) return null;
  const out = new Set<string>([id]);
  for (const l of links) {
    const s = typeof l.source === "string" ? l.source : l.source.id;
    const t = typeof l.target === "string" ? l.target : l.target.id;
    if (s === id) out.add(t);
    else if (t === id) out.add(s);
  }
  return out;
}

export interface Connection {
  edge: Pick<GraphEdge, "id" | "type" | "structural">;
  direction: "out" | "in";
  node: Pick<GraphNode, "id" | "type" | "label" | "title" | "idea_id">;
}

/** Semantic connections of a node (structural containment is implied by the idea and omitted). */
export function connectionsOf(graph: Graph | undefined, id: string): Connection[] {
  if (!graph) return [];
  const byId = new Map((graph.nodes ?? []).map((n) => [n.id, n]));
  const out: Connection[] = [];
  for (const e of graph.edges ?? []) {
    if (e.structural) continue;
    if (e.source === id && byId.has(e.target)) out.push({ edge: e, direction: "out", node: byId.get(e.target)! });
    else if (e.target === id && byId.has(e.source)) out.push({ edge: e, direction: "in", node: byId.get(e.source)! });
  }
  return out;
}

/** Bounding box of positioned nodes (including radius), or null. */
export function bounds(nodes: UNode[]): { x0: number; y0: number; x1: number; y1: number } | null {
  let x0 = Infinity,
    y0 = Infinity,
    x1 = -Infinity,
    y1 = -Infinity;
  for (const n of nodes) {
    if (n.x === undefined || n.y === undefined) continue;
    x0 = Math.min(x0, n.x - n.r);
    y0 = Math.min(y0, n.y - n.r);
    x1 = Math.max(x1, n.x + n.r);
    y1 = Math.max(y1, n.y + n.r);
  }
  return Number.isFinite(x0) ? { x0, y0, x1, y1 } : null;
}

/** Zoom transform {k,x,y} that fits bounds into a viewport with padding, capped at maxScale. */
export function fitTransform(b: { x0: number; y0: number; x1: number; y1: number }, width: number, height: number, padding = 48, maxScale = 2.2, minScale = 0.1) {
  const w = Math.max(1, b.x1 - b.x0);
  const h = Math.max(1, b.y1 - b.y0);
  const k = Math.max(minScale, Math.min(maxScale, Math.min((width - padding * 2) / w, (height - padding * 2) / h)));
  const cx = (b.x0 + b.x1) / 2;
  const cy = (b.y0 + b.y1) / 2;
  return { k, x: width / 2 - cx * k, y: height / 2 - cy * k };
}

/** Find nodes by label/title for the "find" box: label prefix matches first, then substring. */
export function searchNodes<T extends Pick<GraphNode, "label" | "title" | "type">>(nodes: T[], q: string, limit = 8): T[] {
  const s = q.trim().toLowerCase();
  if (!s) return [];
  const scored: { n: T; score: number }[] = [];
  for (const n of nodes) {
    const label = (n.label || "").toLowerCase();
    const title = (n.title || "").toLowerCase();
    let score = -1;
    if (label === s) score = 0;
    else if (label.startsWith(s)) score = 1;
    else if (label.includes(s)) score = 2;
    else if (title.includes(s)) score = 3;
    if (score >= 0) scored.push({ n, score: score * 10 + (n.type === "idea" ? 0 : 1) });
  }
  return scored.sort((a, b) => a.score - b.score).slice(0, limit).map((x) => x.n);
}

/**
 * Give unpositioned nodes a starting point next to their idea (when the idea already has one),
 * so toggling a layer doesn't fling new nodes across the canvas. Mutates the nodes (d3 style).
 */
export function seedNearIdeas(nodes: UNode[], jitter = 40, rand: () => number = Math.random): void {
  const ideas = new Map<string, UNode>();
  for (const n of nodes) if (n.type === "idea" && n.x !== undefined && n.y !== undefined) ideas.set(n.id, n);
  for (const n of nodes) {
    if (n.x !== undefined) continue;
    const a = n.ideaId ? ideas.get(n.ideaId) : undefined;
    if (a && a.x !== undefined && a.y !== undefined) {
      n.x = a.x + (rand() - 0.5) * jitter;
      n.y = a.y + (rand() - 0.5) * jitter;
    }
  }
}

/**
 * Bounds including idea labels drawn ~13px tall at screen scale `k` (≈6.4px per character,
 * max 32 characters), so fitting doesn't clip titles at the edges.
 */
export function labelBounds(nodes: UNode[], k: number): { x0: number; y0: number; x1: number; y1: number } | null {
  const b = bounds(nodes);
  if (!b) return null;
  const s = 1 / Math.max(k, 0.05);
  for (const n of nodes) {
    if (n.type !== "idea" || n.x === undefined || n.y === undefined) continue;
    const half = (Math.min(32, (n.label || "Untitled").length) * 6.4 * s) / 2;
    b.x0 = Math.min(b.x0, n.x - half);
    b.x1 = Math.max(b.x1, n.x + half);
    b.y1 = Math.max(b.y1, n.y + n.r + 22 * s);
  }
  return b;
}
