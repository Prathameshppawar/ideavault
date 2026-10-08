import { describe, expect, it } from "vitest";
import { formatProvenanceValue, journeyKind, layoutJourney, splitRefTitle } from "@/features/ideas/lib/journey";
import { branch, jnode } from "./fixtures";

const branches = [
  branch({ id: "main", is_default: true, name: "Main" }),
  branch({ id: "fork", name: "Marketplace-free", parent_branch_id: "main", forked_from_checkpoint_number: 2 }),
];

describe("layoutJourney", () => {
  const nodes = [
    jnode({ id: "current", kind: "current", at: "2026-10-09T12:00:00Z", title: "Now · active" }),
    jnode({ id: "cp4", kind: "checkpoint", at: "2026-10-03T10:00:00Z", branch_id: "fork" }),
    jnode({ id: "origin", kind: "origin", at: "2026-10-01T09:00:00Z" }),
    jnode({ id: "d1", kind: "decision", at: "2026-10-01T10:00:00Z", branch_id: "main" }),
    jnode({ id: "b", kind: "branch", at: "2026-10-03T09:00:00Z", branch_id: "fork" }),
    jnode({ id: "imp", kind: "import", at: "2026-10-04T09:00:00Z" }),
  ];
  const layout = layoutJourney(nodes, { branches, colWidth: 100, laneHeight: 50, padX: 10, padTop: 20 });

  it("orders nodes chronologically with 'current' last, one column each", () => {
    expect(layout.nodes.map((n) => n.node.id)).toEqual(["origin", "d1", "b", "cp4", "imp", "current"]);
    expect(layout.nodes.map((n) => n.x)).toEqual([10, 110, 210, 310, 410, 510]);
  });

  it("puts default-branch and idea-wide nodes on lane 0 and forks on their own lane", () => {
    const lane = Object.fromEntries(layout.nodes.map((n) => [n.node.id, n.lane]));
    expect(lane).toEqual({ origin: 0, d1: 0, b: 1, cp4: 1, imp: 0, current: 0 });
    expect(layout.lanes).toHaveLength(2);
    expect(layout.lanes[1]).toMatchObject({ branchId: "fork", name: "Marketplace-free", y: 70, startX: 210, endX: 310, forkedFrom: "CP2" });
    expect(layout.lanes[1].fork).toEqual({ fromX: 145, fromY: 20 });
    // Active fork continues (dashed) to "now".
    expect(layout.lanes[1].liveToX).toBe(510);
    expect(layout.lanes[0]).toMatchObject({ startX: 10, endX: 510, fork: null });
  });

  it("labels each new day once and the final node as Now", () => {
    const labels = layout.nodes.map((n) => n.dayLabel);
    expect(labels[0]).not.toBeNull();
    expect(labels[1]).toBeNull(); // same day as origin
    expect(labels[2]).not.toBeNull();
    expect(labels[3]).toBeNull();
    expect(labels[5]).toBe("Now");
  });

  it("does not continue concluded branches to now", () => {
    const l = layoutJourney(nodes, { branches: [branches[0], { ...branches[1], status: "CONCLUDED" }] });
    expect(l.lanes[1].liveToX).toBeNull();
  });

  it("handles an idea with only origin and now", () => {
    const l = layoutJourney([jnode({ id: "origin", kind: "origin", at: "2026-10-01T09:00:00Z" }), jnode({ id: "current", kind: "current", at: "2026-10-01T10:00:00Z" })]);
    expect(l.lanes).toHaveLength(1);
    expect(l.nodes).toHaveLength(2);
    expect(l.width).toBeGreaterThan(0);
  });
});

describe("journey helpers", () => {
  it("splits ref prefixes from titles", () => {
    expect(splitRefTitle("D3 · Build a marketplace")).toEqual({ ref: "D3", text: "Build a marketplace" });
    expect(splitRefTitle("CP12 · Research: organisers first")).toEqual({ ref: "CP12", text: "Research: organisers first" });
    expect(splitRefTitle("Imported chatgpt conversation")).toEqual({ ref: null, text: "Imported chatgpt conversation" });
  });

  it("formats provenance values readably", () => {
    expect(formatProvenanceValue({ decision: 2, question: 1 })).toBe("decision 2 · question 1");
    expect(formatProvenanceValue("REVERSED")).toBe("Reversed");
    expect(formatProvenanceValue("template:v1")).toBe("template:v1");
    expect(formatProvenanceValue(null)).toBe("—");
  });

  it("has a colour for every journey kind", () => {
    for (const k of ["origin", "decision", "checkpoint", "branch", "artifact", "conclusion", "import", "status", "current"]) {
      expect(journeyKind(k).tone).toBeTruthy();
    }
    expect(journeyKind("unknown").label).toBe("unknown");
  });
});
