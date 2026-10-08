import { describe, expect, it } from "vitest";
import type { ActivityEvent } from "@/lib/types";
import { collapseProposals, dayLabel, describeCounts, eventCategory, groupByDay, groupByMonth } from "@/features/dashboard/lib/activity";

/** Local-time ISO string (so tests don't depend on the machine's time zone). */
const at = (y: number, m: number, d: number, h = 12, min = 0) => new Date(y, m - 1, d, h, min).toISOString();

let n = 0;
const ev = (event_type: string, created_at: string, extra: Partial<ActivityEvent> = {}): ActivityEvent => ({
  id: `e${++n}`,
  actor: "user",
  event_type,
  created_at,
  summary: event_type,
  ...extra,
});

describe("eventCategory", () => {
  it("folds raw event types into four chart categories", () => {
    expect(eventCategory("decision.reversed")).toBe("decisions");
    expect(eventCategory("evidence.proposed")).toBe("knowledge");
    expect(eventCategory("question.recorded")).toBe("knowledge");
    expect(eventCategory("checkpoint.created")).toBe("structure");
    expect(eventCategory("context_pack.created")).toBe("structure");
    expect(eventCategory("idea.created")).toBe("ideas");
    expect(eventCategory("conversation.imported")).toBe("ideas");
    expect(eventCategory("something.new")).toBe("ideas");
  });
});

describe("day grouping", () => {
  const now = new Date(2026, 9, 9, 15, 0); // Fri 9 Oct 2026, local

  it("labels days relative to now", () => {
    expect(dayLabel(at(2026, 10, 9, 0, 5), now)).toBe("Today");
    expect(dayLabel(at(2026, 10, 8, 23, 59), now)).toBe("Yesterday");
    expect(dayLabel(at(2026, 10, 5), now)).toBe("Monday");
    expect(dayLabel(at(2026, 9, 1), now)).toBe("Tue 1 Sep");
    expect(dayLabel(at(2025, 12, 31), now)).toBe("31 Dec 2025");
  });

  it("groups by local calendar day, preserving order", () => {
    const events = [ev("a.x", at(2026, 10, 9, 9)), ev("b.x", at(2026, 10, 9, 0, 30)), ev("c.x", at(2026, 10, 8, 23)), ev("d.x", at(2026, 10, 1))];
    const groups = groupByDay(events, (e) => e.created_at, now);
    expect(groups.map((g) => g.label)).toEqual(["Today", "Yesterday", "Thu 1 Oct"]);
    expect(groups[0].items.map((e) => e.event_type)).toEqual(["a.x", "b.x"]);
    expect(groups[0].key).toBe("2026-10-09");
  });

  it("groups by month, newest first", () => {
    const events = [ev("a.x", at(2026, 8, 3)), ev("b.x", at(2026, 10, 2)), ev("c.x", at(2026, 10, 7)), ev("d.x", at(2026, 9, 30))];
    const months = groupByMonth(events, (e) => e.created_at);
    expect(months.map((m) => m.label)).toEqual(["October 2026", "September 2026", "August 2026"]);
    expect(months[0].items.map((e) => e.event_type)).toEqual(["c.x", "b.x"]);
  });
});

describe("collapseProposals", () => {
  it("folds every *.proposed event of an idea into one summary at the first occurrence", () => {
    const t = at(2026, 10, 9);
    const events = [
      ev("question.proposed", t, { idea_id: "i1", idea_title: "Bikes", entity_type: "question" }),
      ev("conversation.imported", t, { idea_id: "i1" }),
      ev("evidence.proposed", t, { idea_id: "i1", entity_type: "evidence" }),
      ev("evidence.proposed", t, { idea_id: "i1", entity_type: "evidence" }),
      ev("question.proposed", t, { idea_id: "i2", entity_type: "question" }),
      ev("decision.recorded", t, { idea_id: "i1" }),
    ];
    const items = collapseProposals(events);
    expect(items.map((i) => i.type)).toEqual(["proposals", "event", "proposals", "event"]);
    const first = items[0];
    expect(first.type === "proposals" && first.total).toBe(3);
    expect(first.type === "proposals" && first.ideaTitle).toBe("Bikes");
    expect(first.type === "proposals" && describeCounts(first.counts)).toBe("2 evidence, 1 question");
  });

  it("describes counts with plural kind names", () => {
    expect(describeCounts({ question: 2, decision: 1 })).toBe("2 questions, 1 decision");
  });
});
