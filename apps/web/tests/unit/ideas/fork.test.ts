import { describe, expect, it } from "vitest";
import { buildForkSelections, countSelected, defaultForkSource, laterCheckpoints, lineagesOf } from "@/features/ideas/lib/fork";
import { checkpoint, item } from "./fixtures";

const cps = [
  checkpoint({ id: "cp1", number: 1, branch_id: "main" }),
  checkpoint({ id: "cp2", number: 2, branch_id: "main" }),
  checkpoint({ id: "cp3", number: 3, branch_id: "main" }),
  checkpoint({ id: "cp4", number: 4, branch_id: "fork" }),
];

describe("fork source", () => {
  it("prefers the checkpoint being viewed", () => {
    expect(defaultForkSource(cps, { viewingCheckpointId: "cp2", branchId: "main" })?.id).toBe("cp2");
  });
  it("falls back to the latest checkpoint on the current branch, then overall", () => {
    expect(defaultForkSource(cps, { branchId: "main" })?.id).toBe("cp3");
    expect(defaultForkSource(cps, { branchId: "fork" })?.id).toBe("cp4");
    expect(defaultForkSource(cps, { branchId: "other" })?.id).toBe("cp4");
    expect(defaultForkSource([], { branchId: "main" })).toBeNull();
  });
  it("lists only later checkpoints as sources of future knowledge", () => {
    expect(laterCheckpoints(cps, cps[1]).map((c) => c.id)).toEqual(["cp4", "cp3"]);
    expect(laterCheckpoints(cps, cps[3])).toEqual([]);
    expect(laterCheckpoints(cps, null)).toEqual([]);
  });
});

describe("buildForkSelections", () => {
  const e2 = item({ kind: "evidence", ref_number: 2 });
  const i2 = item({ kind: "insight", ref_number: 2 });
  const d3 = item({ kind: "decision", ref_number: 3 });
  const oldE = item({ kind: "evidence", ref_number: 3, status: "RETRACTED" });
  const laterItems = [e2, i2, d3, oldE];

  it("returns nothing without a later checkpoint", () => {
    expect(buildForkSelections({ laterCheckpointId: null, kinds: ["evidence"], itemIds: [d3.id] })).toEqual([]);
  });

  it("sends kinds and hand-picked items as separate selections (the API ignores kinds when item_ids are present)", () => {
    expect(buildForkSelections({ laterCheckpointId: "cp3", kinds: ["evidence", "insight"], itemIds: [d3.id], laterItems })).toEqual([
      { checkpoint_id: "cp3", kinds: ["evidence", "insight"] },
      { checkpoint_id: "cp3", item_ids: [d3.id] },
    ]);
  });

  it("drops picked items already covered by a selected kind, but keeps non-live ones", () => {
    const sel = buildForkSelections({ laterCheckpointId: "cp3", kinds: ["evidence"], itemIds: [e2.id, oldE.id, e2.id], laterItems });
    expect(sel).toEqual([
      { checkpoint_id: "cp3", kinds: ["evidence"] },
      { checkpoint_id: "cp3", item_ids: [oldE.id] },
    ]);
  });

  it("handles kinds-only and items-only drafts", () => {
    expect(buildForkSelections({ laterCheckpointId: "cp3", kinds: ["insight", "insight"], itemIds: [] })).toEqual([{ checkpoint_id: "cp3", kinds: ["insight"] }]);
    expect(buildForkSelections({ laterCheckpointId: "cp3", kinds: [], itemIds: [d3.id] })).toEqual([{ checkpoint_id: "cp3", item_ids: [d3.id] }]);
    expect(buildForkSelections({ laterCheckpointId: "cp3", kinds: [], itemIds: [] })).toEqual([]);
  });

  it("counts what a draft would actually bring, skipping lineages already in the source", () => {
    const source = lineagesOf([item({ kind: "evidence", lineage_id: e2.lineage_id })]);
    expect(countSelected({ laterCheckpointId: "cp3", kinds: ["evidence", "insight"], itemIds: [d3.id], laterItems }, source)).toBe(2); // i2 + d3 (e2 already present, oldE not live)
  });
});
