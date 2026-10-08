// Layout for the horizontal idea journey ("Origin → … → Now").
// Nodes are placed in chronological order, one column each, on a lane per branch:
// the default branch (and idea-wide events) run along lane 0; each fork gets its own lane
// below, joined to its parent lane by a curve at the fork point.
import { format, isToday, isYesterday, parseISO } from "date-fns";
import { dayKey } from "@/lib/format";
import type { Branch, JourneyNode } from "@/lib/types";

export type JourneyKind = "origin" | "decision" | "checkpoint" | "branch" | "artifact" | "conclusion" | "import" | "status" | "current";

/** Tone (CSS var suffix) + label per journey node kind. */
export const JOURNEY_KIND: Record<JourneyKind, { tone: string; label: string }> = {
  origin: { tone: "idea", label: "Origin" },
  decision: { tone: "decision", label: "Decision" },
  checkpoint: { tone: "checkpoint", label: "Checkpoint" },
  branch: { tone: "branch", label: "Branch" },
  artifact: { tone: "artifact", label: "Artifact" },
  conclusion: { tone: "action", label: "Conclusion" },
  import: { tone: "conversation", label: "Import" },
  status: { tone: "assumption", label: "Status" },
  current: { tone: "idea", label: "Now" },
};

export function journeyKind(k: string): { tone: string; label: string } {
  return JOURNEY_KIND[k as JourneyKind] ?? { tone: "checkpoint", label: k };
}

export interface LaidOutNode {
  node: JourneyNode;
  index: number;
  x: number;
  y: number;
  lane: number;
  /** Day label shown above the node when the day changes (null otherwise). */
  dayLabel: string | null;
}

export interface JourneyLane {
  lane: number;
  branchId: string | null;
  name: string;
  y: number;
  startX: number;
  endX: number;
  /** Where the lane joins its parent lane (forks only). */
  fork: { fromX: number; fromY: number } | null;
  /** Active branches continue (dashed) to the "now" column. */
  liveToX: number | null;
  /** "CP2" when the branch was forked from a checkpoint. */
  forkedFrom: string | null;
}

export interface JourneyLayout {
  nodes: LaidOutNode[];
  lanes: JourneyLane[];
  width: number;
  height: number;
}

export interface JourneyLayoutOptions {
  branches?: Branch[];
  defaultBranchId?: string | null;
  colWidth?: number;
  laneHeight?: number;
  padX?: number;
  padTop?: number;
  labelHeight?: number;
}

export function layoutJourney(input: JourneyNode[], opts: JourneyLayoutOptions = {}): JourneyLayout {
  const { branches = [], colWidth = 172, laneHeight = 124, padX = 80, padTop = 44, labelHeight = 84 } = opts;
  const byId = new Map(branches.map((b) => [b.id, b]));
  const defaultId = opts.defaultBranchId ?? branches.find((b) => b.is_default)?.id ?? null;

  // Chronological, stable; "current" always last.
  const nodes = input
    .map((n, i) => ({ n, i }))
    .sort((a, b) => {
      if (a.n.kind === "current") return 1;
      if (b.n.kind === "current") return -1;
      return new Date(a.n.at).getTime() - new Date(b.n.at).getTime() || a.i - b.i;
    })
    .map((x) => x.n);

  // Lanes: default branch first, then forks in order of first appearance.
  const laneOf = new Map<string, number>();
  const laneBranch: (string | null)[] = [null];
  if (defaultId) laneOf.set(defaultId, 0);
  for (const n of nodes) {
    const b = n.branch_id;
    if (!b || laneOf.has(b)) continue;
    // A branch that is the default (even if the API didn't mark it) stays on lane 0.
    if (byId.get(b)?.is_default) {
      laneOf.set(b, 0);
      continue;
    }
    laneOf.set(b, laneBranch.length);
    laneBranch.push(b);
  }
  if (defaultId) laneBranch[0] = defaultId;

  const laneY = (lane: number) => padTop + lane * laneHeight;
  let prevDay = "";
  const laid: LaidOutNode[] = nodes.map((node, index) => {
    const lane = node.branch_id ? (laneOf.get(node.branch_id) ?? 0) : 0;
    const day = node.kind === "current" ? "now" : dayKey(node.at);
    const dayLabel = day !== prevDay ? (node.kind === "current" ? "Now" : dayName(node.at)) : null;
    prevDay = day;
    return { node, index, x: padX + index * colWidth, y: laneY(lane), lane, dayLabel };
  });

  const nowX = laid.length ? laid[laid.length - 1].x : padX;
  const lanes: JourneyLane[] = laneBranch.map((branchId, lane) => {
    const own = laid.filter((n) => n.lane === lane);
    const b = branchId ? byId.get(branchId) : undefined;
    const startX = lane === 0 ? padX : (own[0]?.x ?? padX);
    const endX = lane === 0 ? nowX : (own[own.length - 1]?.x ?? startX);
    let fork: JourneyLane["fork"] = null;
    if (lane > 0) {
      const parentLane = b?.parent_branch_id ? (laneOf.get(b.parent_branch_id) ?? 0) : 0;
      fork = { fromX: Math.max(padX, startX - colWidth * 0.65), fromY: laneY(parentLane) };
    }
    const active = lane === 0 || !b || b.status === "ACTIVE";
    return {
      lane,
      branchId,
      name: b?.name ?? (lane === 0 ? "Main" : "Branch"),
      y: laneY(lane),
      startX,
      endX,
      fork,
      liveToX: lane > 0 && active && endX < nowX ? nowX : null,
      forkedFrom: b?.forked_from_checkpoint_number ? `CP${b.forked_from_checkpoint_number}` : null,
    };
  });

  return {
    nodes: laid,
    lanes,
    width: padX * 2 + Math.max(0, laid.length - 1) * colWidth,
    height: padTop + (laneBranch.length - 1) * laneHeight + labelHeight + 16,
  };
}

/** "Today", "Yesterday", "9 Oct", "9 Oct 2024". */
export function dayName(at: string): string {
  const d = parseISO(at);
  if (Number.isNaN(d.getTime())) return "";
  if (isToday(d)) return "Today";
  if (isYesterday(d)) return "Yesterday";
  return format(d, d.getFullYear() === new Date().getFullYear() ? "d MMM" : "d MMM yyyy");
}

/** Split "D3 · Statement" / "CP2 · Title" into a ref label and the rest. */
export function splitRefTitle(title: string): { ref: string | null; text: string } {
  const m = /^([A-Z]{1,2}\d+) · ([\s\S]*)$/.exec(title);
  return m ? { ref: m[1], text: m[2] } : { ref: null, text: title };
}

/** Summarise a provenance value for display. */
export function formatProvenanceValue(v: unknown): string {
  if (v === null || v === undefined || v === "") return "—";
  if (typeof v === "string") return /^[A-Z][A-Z_]+$/.test(v) ? v.charAt(0) + v.slice(1).toLowerCase().replace(/_/g, " ") : v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (Array.isArray(v)) return v.map(formatProvenanceValue).join(", ");
  if (typeof v === "object")
    return Object.entries(v as Record<string, unknown>)
      .map(([k, x]) => `${k.replace(/_/g, " ")} ${formatProvenanceValue(x)}`)
      .join(" · ");
  return String(v);
}
