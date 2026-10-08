import { describe, expect, it } from "vitest";
import type { UsageGroup } from "@/lib/types";
import { buildDailySeries, dayKeys, estimatedShare, headlineParts, isEmptySeries, latencyRows, taskRows } from "@/features/usage/usage-data";
import { splitModelKey } from "@/features/models/model-meta";

const g = (key: string, over: Partial<UsageGroup> = {}): UsageGroup => ({
  key,
  calls: 1,
  failures: 0,
  input_tokens: 0,
  output_tokens: 0,
  total_tokens: 0,
  cost_usd: 0,
  avg_latency_ms: 0,
  p95_latency_ms: 0,
  tool_calls: 0,
  estimated: 0,
  ...over,
});

describe("usage series transforms", () => {
  it("lists every calendar day from since through today", () => {
    const keys = dayKeys(new Date(2026, 9, 6, 15, 30), new Date(2026, 9, 9, 9, 0));
    expect(keys).toEqual(["2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09"]);
  });

  it("builds a dense series stacked by provider, zero-filling missing days and ignoring out-of-range keys", () => {
    const keys = ["2026-10-07", "2026-10-08", "2026-10-09"];
    const rows = buildDailySeries(
      keys,
      {
        groq: [g("2026-10-08", { total_tokens: 1200 }), g("2026-09-01", { total_tokens: 99 })],
        openai: [g("2026-10-07", { total_tokens: 50 }), g("2026-10-08", { total_tokens: 300 })],
        anthropic: undefined, // still loading
      },
      (x) => x.total_tokens,
    );
    expect(rows.map((r) => r.day)).toEqual(keys);
    expect(rows[0]).toMatchObject({ groq: 0, openai: 50, anthropic: 0, label: "7 Oct" });
    expect(rows[1]).toMatchObject({ groq: 1200, openai: 300, anthropic: 0 });
    expect(rows[2]).toMatchObject({ groq: 0, openai: 0, anthropic: 0 });
    expect(isEmptySeries(rows, ["groq", "openai"])).toBe(false);
    expect(isEmptySeries(rows.slice(2), ["groq", "openai"])).toBe(true);
  });

  it("writes the headline as a sentence and reports estimated share honestly", () => {
    const t = g("total", { calls: 1284, total_tokens: 2_100_000, cost_usd: 0.84, failures: 12, estimated: 321 });
    expect(headlineParts(t)).toEqual(["1,284 model calls", "2.1M tokens", "~$0.84 estimated", "12 failures"]);
    expect(headlineParts(g("total", { calls: 1, failures: 1 }))[0]).toBe("1 model call");
    expect(estimatedShare(t)).toBeCloseTo(0.25, 2);
    expect(estimatedShare(g("x", { calls: 0 }))).toBe(0);
  });

  it("ranks latency by p95 and splits task calls into succeeded/failed", () => {
    const lat = latencyRows([g("groq/openai/gpt-oss-20b", { avg_latency_ms: 900, p95_latency_ms: 3000 }), g("groq/openai/gpt-oss-120b", { avg_latency_ms: 500, p95_latency_ms: 1300 })]);
    expect(lat.map((r) => r.label)).toEqual(["openai/gpt-oss-20b", "openai/gpt-oss-120b"]);
    expect(taskRows([g("summary", { calls: 1 }), g("supervisor", { calls: 18, failures: 10 })])).toEqual([
      { key: "supervisor", ok: 8, failed: 10, calls: 18 },
      { key: "summary", ok: 1, failed: 0, calls: 1 },
    ]);
  });

  it("splits provider/model keys where the model id itself contains a slash", () => {
    expect(splitModelKey("groq/openai/gpt-oss-20b")).toEqual({ provider: "groq", model: "openai/gpt-oss-20b" });
    expect(splitModelKey("mock/offline-planner")).toEqual({ provider: "mock", model: "offline-planner" });
  });
});
