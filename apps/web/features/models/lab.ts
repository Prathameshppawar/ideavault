// Pure helpers for the Model Lab (unit-tested).
import type { LabResult } from "@/lib/types";

export interface LabRanking {
  fastest?: string;
  cheapest?: string;
  best?: string;
}

const succeeded = (r: LabResult) => !r.error;

/** Unique arg-min/arg-max over successful results; undefined when fewer than two compete or all tie. */
function pick(results: LabResult[], value: (r: LabResult) => number | undefined, dir: "min" | "max"): string | undefined {
  const scored = results.map((r) => ({ id: r.id, v: value(r) })).filter((x): x is { id: string; v: number } => typeof x.v === "number" && Number.isFinite(x.v));
  if (scored.length < 2) return undefined;
  const vals = scored.map((s) => s.v);
  const target = dir === "min" ? Math.min(...vals) : Math.max(...vals);
  const other = dir === "min" ? Math.max(...vals) : Math.min(...vals);
  if (target === other) return undefined; // everyone tied — nothing to highlight
  return scored.find((s) => s.v === target)?.id;
}

/**
 * Highlights for a Model Lab run. Only successful results compete.
 * - fastest: lowest latency
 * - cheapest: lowest estimated cost
 * - best: highest correctness against the reference answer; when JSON is expected,
 *   invalid JSON ranks below any valid output.
 */
export function rankLabResults(results: LabResult[], opts: { expectJson: boolean; hasReference: boolean }): LabRanking {
  const ok = results.filter(succeeded);
  const out: LabRanking = {
    fastest: pick(ok, (r) => r.latency_ms, "min"),
    cheapest: pick(ok, (r) => r.estimated_cost_usd, "min"),
  };
  if (opts.hasReference) {
    out.best = pick(ok, (r) => (r.correctness ?? 0) - (opts.expectJson && r.valid_json === false ? 1 : 0), "max");
  } else if (opts.expectJson) {
    // Without a reference, "best" only means something if validity separates the models.
    const valid = ok.filter((r) => r.valid_json);
    if (valid.length === 1 && ok.length > 1) out.best = valid[0].id;
  }
  return out;
}

export function schemaErrors(raw: unknown): string[] {
  if (!raw) return [];
  if (Array.isArray(raw)) return raw.map(String).filter(Boolean);
  if (typeof raw === "string") {
    try {
      return schemaErrors(JSON.parse(raw));
    } catch {
      return [raw];
    }
  }
  return [];
}

/** Pretty-print JSON output (also inside ```json fences); null when it isn't JSON. */
export function prettyJson(output: string): string | null {
  const fenced = /```(?:json)?\s*([\s\S]*?)```/i.exec(output);
  const candidates = [output.trim(), fenced?.[1]?.trim()].filter(Boolean) as string[];
  for (const c of candidates) {
    try {
      const v: unknown = JSON.parse(c);
      if (v !== null && typeof v === "object") return JSON.stringify(v, null, 2);
    } catch {
      /* not JSON */
    }
  }
  return null;
}

/** Validate the optional JSON-schema textarea. Returns an error message or null. */
export function schemaInputError(text: string): string | null {
  if (!text.trim()) return null;
  try {
    const v: unknown = JSON.parse(text);
    if (v === null || typeof v !== "object" || Array.isArray(v)) return "The schema must be a JSON object.";
    return null;
  } catch (e) {
    return `Invalid JSON: ${(e as Error).message}`;
  }
}

export function formatLatency(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
}

export function formatCost(usd: number): string {
  if (!usd) return "$0";
  if (usd < 0.0001) return "<$0.0001";
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(3)}`;
}
