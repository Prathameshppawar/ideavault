import { describe, expect, it } from "vitest";
import {
  deltaCounts,
  groupDeltaItems,
  groupSelectionState,
  initialSelection,
  mergeEffect,
  mergePayload,
  setGroupSelection,
  toggleItem,
} from "@/features/deltas/delta-logic";
import type { DeltaItem } from "@/lib/types";

function item(id: string, classification: string, extra: Partial<DeltaItem> = {}): DeltaItem {
  return {
    id,
    delta_id: "d",
    classification,
    kind: "decision",
    statement: `statement ${id}`,
    details: "",
    rationale: "",
    source_excerpt: "",
    position: Number(id.replace(/\D/g, "")) || 0,
    selected: classification !== "UNCHANGED",
    ...extra,
  };
}

const items = [item("i3", "CHANGED"), item("i1", "NEW"), item("i2", "UNCHANGED"), item("i4", "REJECTED"), item("i5", "NEW"), item("i6", "weird")];

describe("delta grouping", () => {
  it("groups into the four review sections in order, sorted by position", () => {
    const g = groupDeltaItems(items);
    expect(g.map((x) => x.cls)).toEqual(["NEW", "CHANGED", "REJECTED", "UNCHANGED"]);
    expect(g[0].items.map((i) => i.id)).toEqual(["i1", "i5", "i6"]);
    expect(deltaCounts(items)).toEqual({ NEW: 3, CHANGED: 1, REJECTED: 1, UNCHANGED: 1 });
  });
});

describe("delta selection", () => {
  it("defaults from `selected` (unchanged items start unchecked)", () => {
    const sel = initialSelection(items);
    expect(sel.has("i2")).toBe(false);
    expect(sel.size).toBe(5);
  });

  it("toggles single items and whole groups", () => {
    const newGroup = groupDeltaItems(items)[0].items;
    let sel = initialSelection(items);
    expect(groupSelectionState(sel, newGroup)).toBe("all");
    sel = toggleItem(sel, "i5");
    expect(groupSelectionState(sel, newGroup)).toBe("some");
    sel = setGroupSelection(sel, newGroup, false);
    expect(groupSelectionState(sel, newGroup)).toBe("none");
    sel = setGroupSelection(sel, newGroup, true);
    expect(groupSelectionState(sel, newGroup)).toBe("all");
    expect(groupSelectionState(sel, [])).toBe("none");
  });

  it("builds the merge payload in review order", () => {
    const sel = new Set(["i4", "i1", "i3"]);
    expect(mergePayload(items, sel)).toEqual({ item_ids: ["i1", "i3", "i4"] });
  });
});

describe("mergeEffect", () => {
  it("explains what merging does without rewriting history", () => {
    expect(mergeEffect(item("x", "NEW", { kind: "insight" }))).toBe("Records a new insight.");
    expect(mergeEffect(item("x", "CHANGED", { target_item_id: "t", target_label: "D3" }))).toMatch(/supersedes D3; D3 stays in history/);
    expect(mergeEffect(item("x", "REJECTED", { target_item_id: "t", target_label: "D3" }))).toBe("Reverses D3.");
    expect(mergeEffect(item("x", "REJECTED", { kind: "assumption", target_item_id: "t", target_label: "A1" }))).toBe(
      "Records challenging evidence and invalidates A1.",
    );
    expect(mergeEffect(item("x", "UNCHANGED", { target_item_id: "t", target_label: "D1" }))).toBe("Adds supporting evidence to D1.");
  });
});
