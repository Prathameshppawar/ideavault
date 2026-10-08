import { describe, expect, it } from "vitest";
import { forkPath, layoutBranchTree, orderBranches } from "@/features/ideas/lib/branch-tree";
import { branch, checkpoint } from "./fixtures";

const main = branch({ id: "main", is_default: true, created_at: "2026-10-01T00:00:00Z" });
const fork = branch({ id: "fork", parent_branch_id: "main", forked_from_checkpoint_id: "cp2", created_at: "2026-10-03T00:00:00Z" });
const nested = branch({ id: "nested", parent_branch_id: "fork", forked_from_checkpoint_id: "cp4", created_at: "2026-10-05T00:00:00Z" });
const early = branch({ id: "early", parent_branch_id: "main", forked_from_checkpoint_id: "cp1", created_at: "2026-10-06T00:00:00Z", status: "ARCHIVED" });
const cps = [
  checkpoint({ id: "cp1", number: 1, branch_id: "main" }),
  checkpoint({ id: "cp2", number: 2, branch_id: "main" }),
  checkpoint({ id: "cp3", number: 3, branch_id: "main" }),
  checkpoint({ id: "cp4", number: 4, branch_id: "fork", kind: "fork_base" }),
  checkpoint({ id: "cp5", number: 5, branch_id: "nested" }),
];

describe("orderBranches", () => {
  it("puts the default branch first and nests forks depth-first under their parent, by fork point", () => {
    const order = orderBranches([nested, early, fork, main], cps);
    expect(order.map((o) => [o.branch.id, o.depth])).toEqual([
      ["main", 0],
      ["early", 1], // forked from CP1 → before the CP2 fork
      ["fork", 1],
      ["nested", 2],
    ]);
  });

  it("survives orphans and cycles", () => {
    const orphan = branch({ id: "orphan", parent_branch_id: "missing" });
    const a = branch({ id: "a", parent_branch_id: "b" });
    const b = branch({ id: "b", parent_branch_id: "a" });
    const ids = orderBranches([a, b, orphan, main]).map((o) => o.branch.id);
    expect(ids[0]).toBe("main");
    expect(new Set(ids)).toEqual(new Set(["main", "orphan", "a", "b"]));
  });
});

describe("layoutBranchTree", () => {
  const layout = layoutBranchTree([main, fork, nested], cps, { colWidth: 100, laneHeight: 50, padLeft: 0, padTop: 10, padRight: 0 });

  it("places checkpoints by global number on their branch's lane", () => {
    const pos = Object.fromEntries(layout.dots.map((d) => [d.checkpoint.id, [d.x, d.y]]));
    expect(pos).toEqual({ cp1: [100, 10], cp2: [200, 10], cp3: [300, 10], cp4: [400, 60], cp5: [500, 110] });
  });

  it("draws forks from the source checkpoint into the child lane", () => {
    const [m, f, n] = layout.lanes;
    expect(m.fork).toBeNull();
    expect(m.startX).toBe(0);
    expect(f.fork).toEqual({ fromX: 200, fromY: 10, checkpointId: "cp2" });
    expect(f.startX).toBe(250);
    expect(f.endX).toBe(400);
    expect(n.fork).toEqual({ fromX: 400, fromY: 60, checkpointId: "cp4" });
  });

  it("adds a live head after the last checkpoint of active branches only", () => {
    const l = layoutBranchTree([main, { ...fork, status: "CONCLUDED" }], cps.slice(0, 4), { colWidth: 100, padLeft: 0 });
    expect(l.lanes[0].headX).toBe(300 + 60);
    expect(l.lanes[1].headX).toBeNull();
  });

  it("handles a branch without checkpoints and an idea without any", () => {
    const empty = layoutBranchTree([main], [], { colWidth: 100, padLeft: 0, padRight: 0 });
    expect(empty.dots).toHaveLength(0);
    expect(empty.lanes[0]).toMatchObject({ startX: 0, endX: 0, headX: 60 });
    expect(empty.width).toBe(60);
  });

  it("builds a smooth fork path", () => {
    expect(forkPath(0, 0, 100, 50)).toBe("M 0 0 C 50 0, 50 50, 100 50");
  });
});
