import { describe, expect, it } from "vitest";
import { canTransition, isReopenable, statusForOutcome, statusOptions, suggestOutcome, validateConclude } from "@/features/ideas/lib/lifecycle";
import { item } from "./fixtures";

describe("idea lifecycle mirror", () => {
  it("allows open → closed moves and only reopening from closed states", () => {
    expect(canTransition("EXPLORING", "DECIDED")).toBe(true);
    expect(canTransition("CONCLUDED", "ACTIVE")).toBe(true);
    expect(canTransition("CONCLUDED", "DECIDED")).toBe(false);
    expect(canTransition("PARKED", "ABANDONED")).toBe(true);
    expect(canTransition("PARKED", "READY_TO_IMPLEMENT")).toBe(false);
    expect(canTransition("MERGED", "ACTIVE")).toBe(false);
    expect(canTransition("MERGED", "MERGED")).toBe(true);
  });

  it("never offers MERGED in the status menu and marks unreachable states", () => {
    const opts = statusOptions("PARKED");
    expect(opts.map((o) => o.status)).not.toContain("MERGED");
    expect(opts.find((o) => o.status === "PARKED")).toMatchObject({ allowed: false, reason: "Current status" });
    expect(opts.find((o) => o.status === "ACTIVE")?.allowed).toBe(true);
    expect(opts.find((o) => o.status === "DECIDED")).toMatchObject({ allowed: false });
    expect(statusOptions("MERGED").every((o) => !o.allowed)).toBe(true);
  });

  it("knows which statuses can be reopened", () => {
    expect(["CONCLUDED", "PARKED", "ABANDONED"].every(isReopenable)).toBe(true);
    expect(isReopenable("MERGED")).toBe(false);
    expect(isReopenable("ACTIVE")).toBe(false);
  });
});

describe("conclude validation", () => {
  const base = { ideaId: "idea-1", currentStatus: "ACTIVE", outcome: null, mergeIntoIdeaId: null };

  it("requires an outcome", () => {
    const v = validateConclude(base);
    expect(v.ok).toBe(false);
    expect(v.errors.outcome).toMatch(/Choose/);
  });

  it("maps outcomes to statuses like the API", () => {
    expect(statusForOutcome("IMPLEMENT")).toBe("READY_TO_IMPLEMENT");
    expect(statusForOutcome("PARK")).toBe("PARKED");
    expect(statusForOutcome("ACTION_PLAN")).toBe("CONCLUDED");
    expect(statusForOutcome("MERGE")).toBe("MERGED");
  });

  it("rejects outcomes the current status can't reach", () => {
    const v = validateConclude({ ...base, currentStatus: "CONCLUDED", outcome: "IMPLEMENT" });
    expect(v.ok).toBe(false);
    expect(v.errors.outcome).toMatch(/reopen it first/);
    expect(validateConclude({ ...base, currentStatus: "PARKED", outcome: "ABANDON" }).ok).toBe(true);
  });

  it("requires a different idea to merge into", () => {
    expect(validateConclude({ ...base, outcome: "MERGE" }).errors.merge).toMatch(/Pick/);
    expect(validateConclude({ ...base, outcome: "MERGE", mergeIntoIdeaId: "idea-1" }).errors.merge).toMatch(/itself/);
    expect(validateConclude({ ...base, outcome: "MERGE", mergeIntoIdeaId: "idea-2" }).ok).toBe(true);
  });

  it("refuses to conclude an idea that is already merged", () => {
    expect(validateConclude({ ...base, currentStatus: "MERGED", outcome: "PARK" }).errors.outcome).toMatch(/already merged/);
  });
});

describe("suggested outcome", () => {
  it("mirrors the server heuristic", () => {
    const d = (n: number) => Array.from({ length: n }, (_, i) => item({ kind: "decision", ref_number: i + 1 }));
    const a = (n: number) => Array.from({ length: n }, (_, i) => item({ kind: "action", status: "TODO", ref_number: i + 1 }));
    expect(suggestOutcome([...d(3), ...a(2)]).outcome).toBe("ACTION_PLAN");
    expect(suggestOutcome(d(3)).outcome).toBe("IMPLEMENT");
    expect(suggestOutcome(d(1)).outcome).toBe("DECISION");
    expect(suggestOutcome([item({ kind: "evidence" }), item({ kind: "insight" }), item({ kind: "insight", ref_number: 2 })]).outcome).toBe("RESEARCH_COMPLETE");
    expect(suggestOutcome([item({ kind: "question", status: "OPEN" })]).outcome).toBe("PARK");
    expect(suggestOutcome([]).outcome).toBe("REFERENCE");
  });

  it("ignores superseded and proposed items", () => {
    const items = [
      item({ kind: "decision", status: "SUPERSEDED" }),
      item({ kind: "decision", ref_number: 2, review_state: "PROPOSED" }),
      item({ kind: "decision", ref_number: 3, status: "REVERSED" }),
    ];
    expect(suggestOutcome(items).outcome).toBe("REFERENCE");
  });
});
