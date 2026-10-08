import { describe, expect, it } from "vitest";
import { buildKnowledgePayload, currentState, emptyKnowledgeForm, groupByKind, flattenKnowledge, isLive, statusActions } from "@/features/knowledge/lib";
import { item } from "./fixtures";

describe("knowledge grouping and state", () => {
  it("treats retired statuses as history", () => {
    for (const s of ["SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED"]) expect(isLive({ status: s })).toBe(false);
    for (const s of ["ACTIVE", "OPEN", "DONE", "VALIDATED", "ANSWERED", "DISPUTED"]) expect(isLive({ status: s })).toBe(true);
  });

  it("groups by kind (every kind present) with newest refs first", () => {
    const g = groupByKind([item({ kind: "decision", ref_number: 1 }), item({ kind: "decision", ref_number: 3 }), item({ kind: "question" })]);
    expect(Object.keys(g)).toEqual(["decision", "assumption", "evidence", "insight", "question", "action"]);
    expect(g.decision.map((i) => i.label)).toEqual(["D3", "D1"]);
    expect(g.action).toEqual([]);
  });

  it("flattens the overview's kind map, tolerating nulls", () => {
    expect(flattenKnowledge({ decision: [item({ kind: "decision" })], question: null })).toHaveLength(1);
    expect(flattenKnowledge(undefined)).toEqual([]);
  });

  it("builds the current-state digest: live decisions, open questions, riskiest assumptions, in-progress actions first", () => {
    const s = currentState([
      item({ kind: "decision", ref_number: 1 }),
      item({ kind: "decision", ref_number: 2, status: "REVERSED" }),
      item({ kind: "decision", ref_number: 3, review_state: "PROPOSED" }),
      item({ kind: "question", status: "OPEN" }),
      item({ kind: "question", ref_number: 2, status: "ANSWERED" }),
      item({ kind: "assumption", ref_number: 1, status: "UNVALIDATED", assumption: { risk: "LOW", validation_method: "" } }),
      item({ kind: "assumption", ref_number: 2, status: "VALIDATING", assumption: { risk: "HIGH", validation_method: "" } }),
      item({ kind: "assumption", ref_number: 3, status: "VALIDATED", assumption: { risk: "HIGH", validation_method: "" } }),
      item({ kind: "action", ref_number: 1, status: "TODO", action: { priority: "HIGH" } }),
      item({ kind: "action", ref_number: 2, status: "IN_PROGRESS", action: { priority: "LOW" } }),
      item({ kind: "action", ref_number: 3, status: "DONE", action: { priority: "HIGH" } }),
    ]);
    expect(s.decisions.map((i) => i.label)).toEqual(["D1"]);
    expect(s.questions.map((i) => i.label)).toEqual(["Q1"]);
    expect(s.assumptions.map((i) => i.label)).toEqual(["A2", "A1"]);
    expect(s.actions.map((i) => i.label)).toEqual(["T2", "T1"]);
    expect(s.totals).toEqual({ decisions: 1, questions: 1, assumptions: 2, actions: 2 });
  });
});

describe("status actions", () => {
  it("offers kind-appropriate transitions and never SUPERSEDED", () => {
    const all = ["decision", "assumption", "evidence", "insight", "question", "action"].flatMap((k) =>
      ["ACTIVE", "UNVALIDATED", "VALIDATING", "OPEN", "TODO", "IN_PROGRESS", "DONE"].flatMap((s) => statusActions(k, s)),
    );
    expect(all.some((a) => a.status === "SUPERSEDED")).toBe(false);
    expect(statusActions("question", "OPEN")).toEqual([
      { status: "ANSWERED", label: "Answer…", needsAnswer: true, tone: "positive" },
      { status: "DROPPED", label: "Drop", tone: "negative" },
    ]);
    expect(statusActions("action", "TODO").map((a) => a.status)).toEqual(["IN_PROGRESS", "DONE", "DROPPED"]);
    expect(statusActions("assumption", "VALIDATED").map((a) => a.status)).toEqual(["UNVALIDATED"]);
    expect(statusActions("decision", "REVERSED")).toEqual([]);
  });
});

describe("buildKnowledgePayload", () => {
  const ctx = { ideaId: "idea-1", branchId: "b-1" };

  it("requires a statement", () => {
    const r = buildKnowledgePayload(emptyKnowledgeForm("decision"), ctx);
    expect(r.payload).toBeNull();
    expect(r.errors.statement).toBeTruthy();
  });

  it("includes only decision fields for a decision and drops empty alternatives", () => {
    const f = {
      ...emptyKnowledgeForm("decision"),
      statement: "  Ship invite-only  ",
      rationale: "Safety first",
      alternatives: [
        { option: "Public trips", reason: "Moderation cost" },
        { option: "  ", reason: "ignored" },
        { option: "Hybrid", reason: "" },
      ],
      risk: "HIGH" as const,
    };
    expect(buildKnowledgePayload(f, ctx).payload).toEqual({
      idea_id: "idea-1",
      branch_id: "b-1",
      kind: "decision",
      statement: "Ship invite-only",
      origin: "SOURCE",
      rationale: "Safety first",
      alternatives: [{ option: "Public trips", reason_rejected: "Moderation cost" }, { option: "Hybrid" }],
    });
  });

  it("validates evidence URLs and carries stance, strength and target", () => {
    const bad = buildKnowledgePayload({ ...emptyKnowledgeForm("evidence"), statement: "x", url: "ftp://nope" }, ctx);
    expect(bad.errors.url).toBeTruthy();
    const ok = buildKnowledgePayload({ ...emptyKnowledgeForm("evidence"), statement: "x", url: "https://a.b", stance: "CHALLENGES", targetItemId: "d1" }, ctx);
    expect(ok.payload).toMatchObject({ kind: "evidence", stance: "CHALLENGES", strength: "MODERATE", url: "https://a.b", target_item_id: "d1" });
    expect(ok.payload).not.toHaveProperty("risk");
  });

  it("converts an action due date to ISO and sets priority", () => {
    const r = buildKnowledgePayload({ ...emptyKnowledgeForm("action"), statement: "Prototype", priority: "HIGH", dueAt: "2026-11-02" }, ctx);
    expect(r.payload).toMatchObject({ priority: "HIGH", due_at: "2026-11-02T12:00:00.000Z" });
  });

  it("supersedes (and reverses decisions only)", () => {
    const d = buildKnowledgePayload({ ...emptyKnowledgeForm("decision"), statement: "New" }, { ...ctx, supersedes: "old", reverses: true });
    expect(d.payload).toMatchObject({ supersedes: "old", reverses: true });
    const q = buildKnowledgePayload({ ...emptyKnowledgeForm("question"), statement: "New?", rationale: "clearer" }, { ...ctx, supersedes: "oldq", reverses: true });
    expect(q.payload).toMatchObject({ supersedes: "oldq", rationale: "clearer" });
    expect(q.payload).not.toHaveProperty("reverses");
  });

  it("keeps the chosen origin", () => {
    const r = buildKnowledgePayload({ ...emptyKnowledgeForm("insight"), statement: "Organisers buy", origin: "INTERPRETATION" }, ctx);
    expect(r.payload).toMatchObject({ origin: "INTERPRETATION", importance: "MEDIUM" });
  });
});
