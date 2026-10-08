import { describe, expect, it } from "vitest";
import type { Graph } from "@/lib/types";
import { bounds, buildModel, labelBounds, connectionsOf, edgeStyle, fitTransform, legendFor, neighbourhood, searchNodes, seedNearIdeas, type UNode } from "@/features/universe/graph-model";

const graph: Graph = {
  nodes: [
    { id: "i1", type: "idea", label: "Biker Community Platform", title: "Ride together", weight: 9, status: "ACTIVE" },
    { id: "i2", type: "idea", label: "Clinic Platform", title: "", weight: 1, status: "PARKED" },
    { id: "d2", type: "decision", label: "D2", title: "No marketplace", idea_id: "i1", weight: 1, status: "REVERSED", origin: "SOURCE" },
    { id: "d3", type: "decision", label: "D3", title: "Verified guides marketplace", idea_id: "i1", weight: 1, status: "ACTIVE" },
    { id: "e1", type: "evidence", label: "E1", title: "Marketplaces shut down", idea_id: "i1", weight: 1, status: "ACTIVE" },
    { id: "c1", type: "conversation", label: "Imported chat", title: "Imported chat", idea_id: "i1", weight: 1, status: "imported" },
  ],
  edges: [
    { id: "k:d2", source: "i1", target: "d2", type: "contains", structural: true },
    { id: "k:d3", source: "i1", target: "d3", type: "contains", structural: true },
    { id: "k:e1", source: "i1", target: "e1", type: "contains", structural: true },
    { id: "c:c1", source: "i1", target: "c1", type: "discussed_in", structural: true },
    { id: "r1", source: "d3", target: "d2", type: "supersedes", structural: false, origin: "SOURCE" },
    { id: "r2", source: "e1", target: "d2", type: "supports", structural: false },
    { id: "r3", source: "e1", target: "d3", type: "supports", structural: false },
    { id: "bad", source: "e1", target: "missing", type: "supports", structural: false },
  ],
};

describe("buildModel", () => {
  it("keeps edges only between visible nodes and computes degree, radius and dead flags", () => {
    const m = buildModel(graph, new Set());
    expect(m.nodes).toHaveLength(6);
    expect(m.links).toHaveLength(7); // the dangling edge is dropped
    const i1 = m.nodes.find((n) => n.id === "i1")!;
    const i2 = m.nodes.find((n) => n.id === "i2")!;
    expect(i1.degree).toBe(4);
    expect(i1.r).toBeGreaterThan(i2.r);
    expect(i2.r).toBeGreaterThan(m.nodes.find((n) => n.id === "d3")!.r);
    expect(m.nodes.find((n) => n.id === "d2")!.dead).toBe(true);
    expect(m.nodes.find((n) => n.id === "d3")!.dead).toBe(false);
  });

  it("hides node types and every edge touching them", () => {
    const m = buildModel(graph, new Set(["evidence", "conversation"]));
    expect(m.nodes.map((n) => n.id).sort()).toEqual(["d2", "d3", "i1", "i2"]);
    expect(m.links.map((l) => l.id).sort()).toEqual(["k:d2", "k:d3", "r1"]);
  });

  it("reuses previous positions so toggles keep the layout", () => {
    const prev = new Map([["i1", { x: 10, y: -5 }]]);
    const m = buildModel(graph, new Set(), prev);
    expect(m.nodes.find((n) => n.id === "i1")).toMatchObject({ x: 10, y: -5 });
    expect(m.nodes.find((n) => n.id === "d2")!.x).toBeUndefined();
    seedNearIdeas(m.nodes, 0, () => 0.5);
    expect(m.nodes.find((n) => n.id === "d2")).toMatchObject({ x: 10, y: -5 });
    expect(m.nodes.find((n) => n.id === "i2")!.x).toBeUndefined();
  });

  it("returns an empty model without data", () => {
    expect(buildModel(undefined, new Set())).toEqual({ nodes: [], links: [] });
  });
});

describe("legendFor", () => {
  it("lists node types in canonical order and only semantic edge types", () => {
    const l = legendFor(graph);
    expect(l.nodes.map((n) => [n.type, n.count])).toEqual([
      ["idea", 2],
      ["decision", 2],
      ["evidence", 1],
      ["conversation", 1],
    ]);
    expect(l.edges.map((e) => [e.type, e.count])).toEqual([
      ["supports", 3],
      ["supersedes", 1],
    ]);
    expect(l.edges[1].style.dash).not.toBeNull();
  });
  it("styles unknown relationship types neutrally", () => {
    expect(edgeStyle("frobnicates")).toMatchObject({ label: "Frobnicates", tone: null });
    expect(edgeStyle("supersedes").inverse).toBe("Superseded by");
  });
});

describe("neighbourhood & connections", () => {
  it("finds one-hop neighbours", () => {
    const m = buildModel(graph, new Set());
    expect([...neighbourhood(m.links, "d2")!].sort()).toEqual(["d2", "d3", "e1", "i1"]);
    expect(neighbourhood(m.links, null)).toBeNull();
  });
  it("lists semantic connections with direction", () => {
    const c = connectionsOf(graph, "d2");
    expect(c.map((x) => [x.edge.type, x.direction, x.node.id])).toEqual([
      ["supersedes", "in", "d3"],
      ["supports", "in", "e1"],
    ]);
  });
});

describe("fit & find", () => {
  it("computes bounds including radius and a centred fit transform", () => {
    const nodes = [
      { id: "a", x: -100, y: -50, r: 10 },
      { id: "b", x: 100, y: 50, r: 10 },
      { id: "c", r: 4 },
    ] as UNode[];
    const b = bounds(nodes)!;
    expect(b).toEqual({ x0: -110, y0: -60, x1: 110, y1: 60 });
    const t = fitTransform(b, 1000, 600, 50, 5);
    expect(t.k).toBeCloseTo(Math.min(900 / 220, 500 / 120));
    expect(t.x).toBeCloseTo(500);
    expect(t.y).toBeCloseTo(300);
    expect(bounds([])).toBeNull();
    const withIdea = [{ id: "i", type: "idea", label: "Ten chars!", x: 0, y: 0, r: 10 }] as UNode[];
    expect(labelBounds(withIdea, 1)).toEqual({ x0: -32, y0: -10, x1: 32, y1: 32 });
    expect(labelBounds(withIdea, 0.5)!.x1).toBeCloseTo(64);
  });
  it("ranks label prefix matches, ideas first", () => {
    const r = searchNodes(graph.nodes, "d");
    expect(r.map((n) => n.id).slice(0, 2)).toEqual(["d2", "d3"]);
    expect(searchNodes(graph.nodes, "market").map((n) => n.id)).toEqual(["d2", "d3", "e1"]);
    expect(searchNodes(graph.nodes, "  ")).toEqual([]);
  });
});
