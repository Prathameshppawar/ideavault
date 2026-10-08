import { describe, expect, it } from "vitest";
import type { DecisionEntry, KnowledgeItem } from "@/lib/types";
import { stateSentence } from "@/features/dashboard/views/today-view";
import { groupDecisions, isChanged } from "@/features/dashboard/views/decisions-view";
import { velocity } from "@/features/dashboard/views/momentum-view";
import { asItem } from "@/features/dashboard/views/learning-view";

const item = (over: Partial<KnowledgeItem>): KnowledgeItem =>
  ({
    id: "x",
    idea_id: "i",
    branch_id: "b",
    kind: "decision",
    label: "D1",
    ref_number: 1,
    lineage_id: "x",
    statement: "s",
    details: "",
    status: "ACTIVE",
    origin: "SOURCE",
    review_state: "ACCEPTED",
    source_excerpt: "",
    created_by: "user",
    created_at: "2026-10-09T10:00:00Z",
    updated_at: "2026-10-09T10:00:00Z",
    ...over,
  }) as KnowledgeItem;

const entry = (over: Partial<DecisionEntry>): DecisionEntry => ({ ...item(over), idea_title: "Idea", branch_name: "Main", evidence_count: 0, ...over }) as DecisionEntry;

describe("Today", () => {
  it("summarises the state of thinking in one sentence", () => {
    const base = { active_ideas: [{}, {}, {}], open_questions: [{}, {}], risky_assumptions: [{}], open_actions: [], pending_proposals: 0 } as never;
    expect(stateSentence(base)).toBe("3 ideas in motion and 3 open loops.");
    expect(stateSentence({ ...(base as object), pending_proposals: 26 } as never)).toBe("3 ideas in motion, 3 open loops and 26 proposals to review.");
    expect(stateSentence({ active_ideas: [], open_questions: [], risky_assumptions: [], open_actions: [], pending_proposals: 1 } as never)).toBe(
      "No ideas in motion, no open loops and 1 proposal to review.",
    );
  });
});

describe("Decisions", () => {
  const ds = [
    entry({ id: "a1", idea_id: "A", idea_title: "Alpha", ref_number: 2, status: "ACTIVE", created_at: "2026-10-01T00:00:00Z" }),
    entry({ id: "a0", idea_id: "A", idea_title: "Alpha", ref_number: 1, status: "REVERSED", replaced_by: { type: "decision", id: "a1", label: "D2" }, created_at: "2026-09-01T00:00:00Z" }),
    entry({ id: "b1", idea_id: "B", idea_title: "Beta", ref_number: 1, status: "SUPERSEDED", created_at: "2026-10-05T00:00:00Z" }),
  ];
  it("groups by idea (most recent first) and orders each idea's decisions as a story", () => {
    const g = groupDecisions(ds, "all");
    expect(g.map((x) => x.title)).toEqual(["Beta", "Alpha"]);
    expect(g[1].items.map((d) => d.id)).toEqual(["a0", "a1"]);
  });
  it("filters standing vs changed decisions", () => {
    expect(groupDecisions(ds, "active").flatMap((g) => g.items.map((d) => d.id))).toEqual(["a1"]);
    expect(groupDecisions(ds, "changed").flatMap((g) => g.items.map((d) => d.id)).sort()).toEqual(["a0", "b1"]);
    expect(isChanged({ status: "ACTIVE", replaced_by: undefined })).toBe(false);
  });
  it("strips the entry's context fields before rendering as a knowledge item", () => {
    const it = asItem(entry({ evidence_count: 3 }));
    expect("evidence_count" in it).toBe(false);
    expect("idea_title" in it).toBe(false);
    expect(it.statement).toBe("s");
  });
});

describe("Momentum", () => {
  it("describes velocity between windows", () => {
    expect(velocity(5, 0)).toEqual({ dir: "new", text: "newly active" });
    expect(velocity(3, 3)).toEqual({ dir: "flat", text: "steady" });
    expect(velocity(6, 3)).toEqual({ dir: "up", text: "up 100%" });
    expect(velocity(1, 4)).toEqual({ dir: "down", text: "down 75%" });
    expect(velocity(0, 0).dir).toBe("flat");
  });
});
