// Pure helpers for the Insights screen (unit-tested).
import type { Analysis, PromptAnalysis, ThinkingAnalysis } from "@/lib/types";
import type { components } from "@/types/api.generated";

export type ThinkingPattern = components["schemas"]["ServiceThinkingPattern"];

export const PROMPT_DIMENSIONS = ["context", "goal", "constraints", "specificity", "examples", "expected_output", "clarity", "completeness"] as const;
export type PromptDimension = (typeof PROMPT_DIMENSIONS)[number];

export const DIMENSION_LABEL: Record<PromptDimension, string> = {
  context: "Context",
  goal: "Goal",
  constraints: "Constraints",
  specificity: "Specificity",
  examples: "Examples",
  expected_output: "Expected output",
  clarity: "Clarity",
  completeness: "Completeness",
};

export const DIMENSION_HINT: Record<PromptDimension, string> = {
  context: "Background about your situation, tools and what exists",
  goal: "What you want produced or decided",
  constraints: "Must / must-not, budget, platform, limits",
  specificity: "Enough detail to act on",
  examples: "Examples of what good looks like",
  expected_output: "The form the answer should take",
  clarity: "Precise, unambiguous wording",
  completeness: "Context, goal, constraints and output all present",
};

export const SCORE_WORD = ["Absent", "Partial", "Strong"] as const;

export function scoreBand(overall: number): { label: string; tone: "success" | "warning" | "danger" } {
  if (overall >= 75) return { label: "Strong prompt", tone: "success" };
  if (overall >= 45) return { label: "Decent — room to improve", tone: "warning" };
  return { label: "Needs more to go on", tone: "danger" };
}

/** Rubric scores in fixed order, clamped to 0–2 (missing → 0). */
export function rubric(scores: Record<string, number> | undefined | null): { key: PromptDimension; score: 0 | 1 | 2 }[] {
  return PROMPT_DIMENSIONS.map((key) => {
    const v = Math.round(Number(scores?.[key] ?? 0));
    return { key, score: (v <= 0 ? 0 : v >= 2 ? 2 : 1) as 0 | 1 | 2 };
  });
}

export function analyzerLabel(a: string | undefined): string {
  if (!a) return "Unknown analyzer";
  const parts = a.split(" + ").map((p) => {
    if (p.startsWith("heuristic")) return "transparent heuristic rules";
    if (p.startsWith("deterministic")) return "deterministic graph statistics";
    if (p.startsWith("llm:")) return `AI model ${p.slice(4)}`;
    return p;
  });
  return parts.join(" + ");
}

export const PATTERN_KINDS = ["strength", "habit", "weakness", "risk"] as const;

export const PATTERN_KIND_META: Record<string, { label: string; tone: string; blurb: string }> = {
  strength: { label: "Strengths", tone: "action", blurb: "Habits worth keeping." },
  habit: { label: "Habits", tone: "branch", blurb: "Neither good nor bad — worth noticing." },
  weakness: { label: "Gaps", tone: "assumption", blurb: "Where your record is thinner than it could be." },
  risk: { label: "Risks", tone: "question", blurb: "Things that could bite later." },
};

export function groupPatterns(patterns: ThinkingPattern[] | null | undefined): { kind: string; patterns: ThinkingPattern[] }[] {
  const all = patterns ?? [];
  const kinds = [...PATTERN_KINDS, ...new Set(all.map((p) => p.kind).filter((k) => !(PATTERN_KINDS as readonly string[]).includes(k)))];
  return kinds.map((kind) => ({ kind, patterns: all.filter((p) => p.kind === kind) })).filter((g) => g.patterns.length > 0);
}

function n(stats: Record<string, unknown>, key: string): number | null {
  const v = stats[key];
  return typeof v === "number" && Number.isFinite(v) ? v : null;
}

function count(v: number, one: string, many: string): string {
  return `${v.toLocaleString("en")} ${v === 1 ? one : many}`;
}

/** Turn the analysis stats into a readable paragraph (no stat-card grid). */
export function statsSentences(stats: Record<string, unknown> | null | undefined, scope: string): string[] {
  const s = stats ?? {};
  const out: string[] = [];
  const ideas = n(s, "ideas");
  const decisions = n(s, "decisions");
  const changed = n(s, "decisions_changed");
  const rationale = n(s, "decisions_with_rationale");
  const where = scope === "idea" ? "In this idea" : ideas != null ? `Across ${count(ideas, "idea", "ideas")}` : "Across your vault";
  if (decisions != null) {
    if (decisions === 0) out.push(`${where} you haven't recorded any accepted decisions yet.`);
    else {
      const tail: string[] = [];
      if (changed != null) tail.push(changed === 0 ? "none has been reversed or superseded" : `${changed} later changed`);
      if (rationale != null)
        tail.push(rationale === 0 ? "none has a written rationale" : rationale === decisions ? `${decisions === 1 ? "it has" : "all have"} a written rationale` : `${rationale} ${rationale === 1 ? "has" : "have"} a written rationale`);
      out.push(`${where} you've recorded ${count(decisions, "decision", "decisions")}${tail.length ? ` — ${tail.join(", and ")}` : ""}.`);
    }
  }
  const unval = n(s, "assumptions_unvalidated");
  const stale = n(s, "assumptions_stale");
  const open = n(s, "questions_open");
  const parts: string[] = [];
  if (unval != null) parts.push(unval === 0 ? "no assumptions are waiting for validation" : `${count(unval, "assumption is", "assumptions are")} still unvalidated${stale ? ` (${stale} for more than two weeks)` : ""}`);
  if (open != null) parts.push(open === 0 ? "no questions are open" : `${count(open, "question is", "questions are")} open`);
  if (parts.length) {
    const sentence = parts.join(" and ");
    out.push(sentence.charAt(0).toUpperCase() + sentence.slice(1) + ".");
  }
  const bpi = n(s, "branches_per_idea");
  const parked = n(s, "ideas_parked_or_abandoned");
  if (bpi != null || parked) {
    const bits: string[] = [];
    if (bpi != null) bits.push(`Ideas average ${bpi.toLocaleString("en", { maximumFractionDigits: 1 })} ${bpi === 1 ? "branch" : "branches"}`);
    if (parked) bits.push(`${parked} ${parked === 1 ? "is" : "are"} parked or abandoned`);
    out.push(bits.join("; ") + ".");
  }
  const prompts = n(s, "prompts_analyzed");
  if (prompts) out.push(`Prompt habits are based on ${count(prompts, "message", "messages")} you wrote yourself (imported content is excluded).`);
  return out;
}

/** Latest stored analysis matching the scope ("" = whole vault). */
export function latestFor(list: Analysis[] | undefined, ideaId: string): Analysis | undefined {
  return (list ?? []).find((a) => (ideaId ? a.idea_id === ideaId : !a.idea_id));
}

export function asThinking(a: Analysis | undefined): ThinkingAnalysis | undefined {
  return a ? (a.result as unknown as ThinkingAnalysis) : undefined;
}

export function asPrompt(a: Analysis | undefined): PromptAnalysis | undefined {
  if (!a) return undefined;
  const r = a.result as unknown as PromptAnalysis;
  return { ...r, prompt: r.prompt || String((a.input as { prompt?: unknown })?.prompt ?? "") };
}
