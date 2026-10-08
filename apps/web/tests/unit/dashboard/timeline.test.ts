import { describe, expect, it } from "vitest";
import { format } from "date-fns";
import { bucketDayKey, buildHeatmap, buildSeries, coversRange, heatLevel, localBuckets } from "@/features/dashboard/lib/timeline";

const localMidnight = (y: number, m: number, d: number) => format(new Date(y, m - 1, d), "yyyy-MM-dd'T'HH:mm:ssxxx");
const bucket = (day: string, by_type: Record<string, number>) => ({ day, by_type, total: Object.values(by_type).reduce((a, b) => a + b, 0), ideas: 1 });

describe("bucketDayKey", () => {
  it("uses the UTC date for UTC-midnight buckets (database default)", () => {
    expect(bucketDayKey("2026-10-08T00:00:00Z")).toBe("2026-10-08");
    expect(bucketDayKey("2026-10-08T05:30:00+05:30")).toBe("2026-10-08");
  });
  it("uses the local date for local-midnight buckets", () => {
    expect(bucketDayKey(localMidnight(2026, 10, 9))).toBe("2026-10-09");
  });
  it("returns empty for garbage", () => {
    expect(bucketDayKey("nope")).toBe("");
  });
});

describe("heatLevel", () => {
  it("buckets counts into quartiles of the busiest day", () => {
    expect(heatLevel(0, 10)).toBe(0);
    expect(heatLevel(1, 10)).toBe(1);
    expect(heatLevel(5, 10)).toBe(2);
    expect(heatLevel(7, 10)).toBe(3);
    expect(heatLevel(10, 10)).toBe(4);
    expect(heatLevel(3, 0)).toBe(0);
  });
});

describe("buildHeatmap", () => {
  const today = new Date(2026, 9, 9, 15); // Friday
  const buckets = [
    bucket(localMidnight(2026, 10, 9), { "decision.recorded": 2, "idea.created": 2 }),
    bucket(localMidnight(2026, 10, 1), { "question.proposed": 1 }),
    bucket(localMidnight(2026, 6, 1), { "idea.created": 9 }), // outside a 30-day range
  ];

  it("lays out Monday-first week columns ending today, padding outside the range", () => {
    const h = buildHeatmap(buckets, 30, today);
    expect(h.weeks.every((w) => w.length === 7)).toBe(true);
    const first = h.weeks[0][0];
    expect(first.date.getDay()).toBe(1); // Monday
    const flat = h.weeks.flat();
    const inside = flat.filter((c) => !c.outside);
    expect(inside).toHaveLength(30);
    expect(inside[inside.length - 1].key).toBe("2026-10-09");
    expect(flat.filter((c) => c.outside && c.date > today)).toHaveLength(2); // Sat + Sun after Friday
  });

  it("totals, busiest day, levels and category breakdown only count the range", () => {
    const h = buildHeatmap(buckets, 30, today);
    expect(h.total).toBe(5);
    expect(h.activeDays).toBe(2);
    expect(h.max).toBe(4);
    expect(h.busiest).toMatchObject({ key: "2026-10-09", count: 4 });
    const cell = h.weeks.flat().find((c) => c.key === "2026-10-09")!;
    expect(cell.level).toBe(4);
    expect(cell.totals?.byCategory).toEqual({ knowledge: 0, ideas: 2, decisions: 2, structure: 0 });
    expect(h.weeks.flat().find((c) => c.key === "2026-10-01")!.level).toBe(1);
  });

  it("labels months at the column containing the 1st", () => {
    const h = buildHeatmap(buckets, 90, today);
    expect(h.months.map((m) => m.label)).toEqual(["Jul", "Aug", "Sep", "Oct"]);
    const oct = h.months.find((m) => m.label === "Oct")!;
    expect(h.weeks[oct.col].some((c) => c.key === "2026-10-01")).toBe(true);
  });
});

describe("buildSeries", () => {
  const today = new Date(2026, 9, 9, 15);
  const buckets = [bucket(localMidnight(2026, 10, 9), { "checkpoint.created": 3 }), bucket(localMidnight(2026, 10, 6), { "evidence.recorded": 1 })];

  it("emits one zero-filled row per day up to 90 days", () => {
    const { rows, unit } = buildSeries(buckets, 30, today);
    expect(unit).toBe("day");
    expect(rows).toHaveLength(30);
    expect(rows.at(-1)).toMatchObject({ key: "2026-10-09", structure: 3, total: 3 });
    expect(rows.find((r) => r.key === "2026-10-06")).toMatchObject({ knowledge: 1, total: 1 });
    expect(rows.filter((r) => r.total === 0)).toHaveLength(28);
  });

  it("aggregates by Monday-start week beyond 90 days", () => {
    const { rows, unit } = buildSeries(buckets, 365, today);
    expect(unit).toBe("week");
    const last = rows.at(-1)!;
    expect(last.key).toBe("2026-10-05");
    expect(last).toMatchObject({ total: 4, knowledge: 1, structure: 3 });
  });
});

describe("localBuckets / coversRange", () => {
  it("re-buckets raw events by local day in the server's shape", () => {
    const evs = [
      { event_type: "idea.created", created_at: new Date(2026, 9, 9, 0, 30).toISOString(), idea_id: "a" },
      { event_type: "idea.created", created_at: new Date(2026, 9, 9, 23, 0).toISOString(), idea_id: "b" },
      { event_type: "decision.recorded", created_at: new Date(2026, 9, 8, 23, 59).toISOString(), idea_id: "a" },
    ];
    const b = localBuckets(evs);
    expect(b.map((x) => bucketDayKey(x.day))).toEqual(["2026-10-08", "2026-10-09"]);
    expect(b[1]).toMatchObject({ total: 2, ideas: 2, by_type: { "idea.created": 2 } });
  });

  it("knows whether a limited, newest-first list reaches the range start", () => {
    const today = new Date(2026, 9, 9);
    const recent = [{ created_at: new Date(2026, 9, 9).toISOString() }, { created_at: new Date(2026, 9, 1).toISOString() }];
    expect(coversRange(recent, 30, today, 5)).toBe(true); // fewer than limit → complete
    expect(coversRange(recent, 30, today, 2)).toBe(false); // hit the limit, oldest is inside range
    expect(coversRange(recent, 5, today, 2)).toBe(true); // oldest predates a 5-day range
  });
});
