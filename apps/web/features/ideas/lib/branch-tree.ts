// Layout for the visual branch tree: one horizontal lane per branch, checkpoints as dots.
// X is the global checkpoint order (CP numbers are idea-wide), so a fork visibly leaves its
// source checkpoint and later checkpoints line up across lanes in time.
import type { Branch, Checkpoint } from "@/lib/types";

export interface TreeLane {
  branch: Branch;
  index: number;
  depth: number;
  y: number;
  startX: number;
  /** Last checkpoint x (or startX when the branch has none). */
  endX: number;
  /** Live head marker x (active branches only). */
  headX: number | null;
  fork: { fromX: number; fromY: number; checkpointId: string } | null;
}

export interface TreeDot {
  checkpoint: Checkpoint;
  x: number;
  y: number;
  lane: number;
}

export interface BranchTreeLayout {
  lanes: TreeLane[];
  dots: TreeDot[];
  width: number;
  height: number;
}

export interface BranchTreeOptions {
  colWidth?: number;
  laneHeight?: number;
  padLeft?: number;
  padRight?: number;
  padTop?: number;
}

/** Default branch first, then a depth-first walk so each fork sits just below its parent. */
export function orderBranches(branches: Branch[], checkpoints: Checkpoint[] = []): { branch: Branch; depth: number }[] {
  const cpNumber = new Map(checkpoints.map((c) => [c.id, c.number]));
  const forkOrder = (b: Branch) => (b.forked_from_checkpoint_id ? (cpNumber.get(b.forked_from_checkpoint_id) ?? b.forked_from_checkpoint_number ?? 0) : 0);
  const byCreated = (a: Branch, b: Branch) => forkOrder(a) - forkOrder(b) || new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
  const ids = new Set(branches.map((b) => b.id));
  const children = new Map<string, Branch[]>();
  const roots: Branch[] = [];
  for (const b of branches) {
    if (b.parent_branch_id && ids.has(b.parent_branch_id) && b.parent_branch_id !== b.id) {
      const list = children.get(b.parent_branch_id) ?? [];
      list.push(b);
      children.set(b.parent_branch_id, list);
    } else roots.push(b);
  }
  roots.sort((a, b) => Number(b.is_default) - Number(a.is_default) || byCreated(a, b));
  const out: { branch: Branch; depth: number }[] = [];
  const seen = new Set<string>();
  const walk = (b: Branch, depth: number) => {
    if (seen.has(b.id)) return;
    seen.add(b.id);
    out.push({ branch: b, depth });
    for (const c of (children.get(b.id) ?? []).sort(byCreated)) walk(c, depth + 1);
  };
  for (const r of roots) walk(r, 0);
  // Cycles / unreachable branches (defensive): append in creation order.
  for (const b of [...branches].sort(byCreated)) if (!seen.has(b.id)) walk(b, 0);
  return out;
}

export function layoutBranchTree(branches: Branch[], checkpoints: Checkpoint[], opts: BranchTreeOptions = {}): BranchTreeLayout {
  const { colWidth = 76, laneHeight = 56, padLeft = 28, padRight = 56, padTop = 28 } = opts;
  const ordered = orderBranches(branches, checkpoints);
  const laneIndex = new Map(ordered.map((o, i) => [o.branch.id, i]));
  const sortedCps = [...checkpoints].sort((a, b) => a.number - b.number);
  const col = new Map(sortedCps.map((c, i) => [c.id, i + 1]));
  const xOf = (c: number) => padLeft + c * colWidth;
  const yOf = (lane: number) => padTop + lane * laneHeight;

  const dots: TreeDot[] = [];
  for (const c of sortedCps) {
    const lane = laneIndex.get(c.branch_id);
    if (lane === undefined) continue;
    dots.push({ checkpoint: c, x: xOf(col.get(c.id)!), y: yOf(lane), lane });
  }

  const lanes: TreeLane[] = ordered.map(({ branch, depth }, index) => {
    const own = dots.filter((d) => d.lane === index);
    let fork: TreeLane["fork"] = null;
    let startX = xOf(0);
    const src = branch.forked_from_checkpoint_id;
    if (src && col.has(src)) {
      const srcDot = dots.find((d) => d.checkpoint.id === src);
      const fromX = xOf(col.get(src)!);
      const parentLane = branch.parent_branch_id ? laneIndex.get(branch.parent_branch_id) : undefined;
      const fromY = srcDot ? srcDot.y : yOf(parentLane ?? 0);
      fork = { fromX, fromY, checkpointId: src };
      startX = fromX + colWidth * 0.5;
    } else if (!branch.is_default && branch.parent_branch_id) {
      // Forked from a branch head (no checkpoint): start just before its first checkpoint.
      startX = own.length ? own[0].x - colWidth * 0.5 : xOf(0);
    }
    const endX = own.length ? Math.max(own[own.length - 1].x, startX) : startX;
    const headX = branch.status === "ACTIVE" ? endX + colWidth * 0.6 : null;
    return { branch, index, depth, y: yOf(index), startX, endX, headX, fork };
  });

  const maxX = Math.max(xOf(0), ...lanes.map((l) => l.headX ?? l.endX), ...dots.map((d) => d.x));
  return { lanes, dots, width: maxX + padRight, height: padTop * 2 + Math.max(0, lanes.length - 1) * laneHeight };
}

/** SVG path for a fork: leaves the source checkpoint, curves down into the child lane. */
export function forkPath(fromX: number, fromY: number, toX: number, toY: number): string {
  const mid = (fromX + toX) / 2;
  return `M ${fromX} ${fromY} C ${mid} ${fromY}, ${mid} ${toY}, ${toX} ${toY}`;
}
