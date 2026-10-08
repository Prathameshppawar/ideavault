import { describe, expect, it } from "vitest";
import type { LabResult } from "@/lib/types";
import { prettyJson, rankLabResults, schemaErrors, schemaInputError } from "@/features/models/lab";
import { capabilitiesOf, formatContext } from "@/features/models/model-meta";
import { modelPayload } from "@/features/models/registry-tab";

const r = (id: string, over: Partial<LabResult> = {}): LabResult => ({
  id,
  run_id: "run",
  provider: "groq",
  model: id,
  output: "",
  latency_ms: 1000,
  input_tokens: 10,
  output_tokens: 10,
  total_tokens: 20,
  estimated_cost_usd: 0.001,
  schema_errors: null,
  created_at: "2026-10-09T00:00:00Z",
  ...over,
});

describe("lab result ranking", () => {
  it("highlights fastest, cheapest and best among successful results only", () => {
    const results = [
      r("a", { latency_ms: 400, estimated_cost_usd: 0.002, correctness: 0.5 }),
      r("b", { latency_ms: 900, estimated_cost_usd: 0.0005, correctness: 0.8 }),
      r("c", { latency_ms: 50, estimated_cost_usd: 0, error: "HTTP 429: rate limited" }),
    ];
    expect(rankLabResults(results, { expectJson: false, hasReference: true })).toEqual({ fastest: "a", cheapest: "b", best: "b" });
  });

  it("does not crown anyone when there is no competition or everyone ties", () => {
    expect(rankLabResults([r("solo")], { expectJson: false, hasReference: true })).toEqual({ fastest: undefined, cheapest: undefined, best: undefined });
    const tied = [r("a", { estimated_cost_usd: 0, latency_ms: 10 }), r("b", { estimated_cost_usd: 0, latency_ms: 20 })];
    expect(rankLabResults(tied, { expectJson: false, hasReference: false })).toEqual({ fastest: "a", cheapest: undefined });
  });

  it("ranks invalid JSON below valid output when JSON is expected", () => {
    const results = [r("a", { correctness: 0.9, valid_json: false }), r("b", { correctness: 0.6, valid_json: true })];
    expect(rankLabResults(results, { expectJson: true, hasReference: true }).best).toBe("b");
    // Without a reference, validity alone decides only when it separates the models.
    expect(rankLabResults(results, { expectJson: true, hasReference: false }).best).toBe("b");
    expect(rankLabResults([r("a", { valid_json: true }), r("b", { valid_json: true })], { expectJson: true, hasReference: false }).best).toBeUndefined();
  });

  it("parses schema errors and pretty-prints JSON output", () => {
    expect(schemaErrors(null)).toEqual([]);
    expect(schemaErrors(["missing field: title"])).toEqual(["missing field: title"]);
    expect(schemaErrors('["a","b"]')).toEqual(["a", "b"]);
    expect(prettyJson('Here you go:\n```json\n{"a":1}\n```')).toBe('{\n  "a": 1\n}');
    expect(prettyJson("plain text")).toBeNull();
    expect(schemaInputError("")).toBeNull();
    expect(schemaInputError("[1]")).toMatch(/object/);
    expect(schemaInputError("{oops")).toMatch(/Invalid JSON/);
  });
});

describe("model registry helpers", () => {
  it("formats context sizes and merges capability flags", () => {
    expect(formatContext(131072)).toBe("128K");
    expect(formatContext(400000)).toBe("400K");
    expect(formatContext(1048576)).toBe("1M");
    expect(formatContext(1000000)).toBe("1M");
    const m = { capabilities: ["chat", "fast"], tool_calling: true, structured_output: false, vision: true, reasoning: false } as Parameters<typeof capabilitiesOf>[0];
    expect(capabilitiesOf(m)).toEqual(["tools", "vision", "fast"]);
  });

  it("builds the custom-model body, keeping capabilities the dialog doesn't edit", () => {
    const body = modelPayload({
      provider: " Ollama ",
      model: " qwen3:8b ",
      display_name: "",
      context_length: "40960",
      caps: new Set(["tools", "json"] as const),
      speed: 3,
      quality: 2,
      relative_cost: 0,
      input_cost_per_mtok: "",
      output_cost_per_mtok: "0.5",
      enabled: true,
      extra: ["local"],
    });
    expect(body).toMatchObject({
      provider: "ollama",
      model: "qwen3:8b",
      display_name: "qwen3:8b",
      context_length: 40960,
      capabilities: ["chat", "tools", "json", "local"],
      tool_calling: true,
      structured_output: true,
      vision: false,
      input_cost_per_mtok: 0,
      output_cost_per_mtok: 0.5,
    });
  });
});
