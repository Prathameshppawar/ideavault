// Pure helpers for presenting artifacts: names, generators, provenance grouping, previews.
import { humanize } from "@/lib/format";
import type { ProvenanceEntry } from "@/lib/types";

export const ARTIFACT_STATUSES = ["DRAFT", "FINAL", "ARCHIVED"] as const;
export type ArtifactStatus = (typeof ARTIFACT_STATUSES)[number];

/** Tone (CSS variable stem) used for each artifact status pill. */
export const ARTIFACT_STATUS_TONE: Record<string, Tone> = {
  DRAFT: "muted",
  FINAL: "success",
  ARCHIVED: "faint",
};

export type Tone = "muted" | "faint" | "success" | "warning" | "danger" | "accent" | "info";

const TYPE_NAMES: Record<string, string> = {
  IMPLEMENTATION_PROMPT: "Implementation prompt",
  TECHNICAL_SPEC: "Technical spec",
  EXECUTIVE_SUMMARY: "Executive summary",
};

/** "ACTION_PLAN" → "Action plan". */
export function artifactTypeName(type: string): string {
  return TYPE_NAMES[type] ?? humanize(type);
}

export interface GeneratorInfo {
  kind: "template" | "ai" | "user" | "other";
  /** Short label, e.g. "Template" or "AI · groq/llama-3.3-70b". */
  label: string;
  /** Longer explanation for tooltips. */
  detail: string;
}

/** Interprets the `generator` field: `template:v1`, `llm:provider/model`, `user`. */
export function generatorInfo(generator: string | undefined | null): GeneratorInfo {
  const g = (generator ?? "").trim();
  if (g.startsWith("template")) {
    const v = g.split(":")[1];
    return { kind: "template", label: "Template", detail: `Deterministic template${v ? ` (${v})` : ""} — assembled from recorded items, no AI` };
  }
  if (g.startsWith("llm:")) {
    const model = g.slice(4);
    return { kind: "ai", label: `AI · ${model}`, detail: `Written by ${model} from recorded thinking` };
  }
  if (g === "user") return { kind: "user", label: "Manual edit", detail: "Written or edited by you" };
  return { kind: "other", label: g ? humanize(g) : "Unknown", detail: g || "Unknown generator" };
}

/** Interprets analyzer strings from deltas and explanations (`heuristic:v1`, `deterministic`, `llm:…`). */
export function analyzerLabel(analyzer: string | undefined | null): string {
  const a = (analyzer ?? "").trim();
  if (!a) return "";
  if (a.startsWith("llm:")) return `AI · ${a.slice(4)}`;
  if (a.startsWith("heuristic")) return "Heuristic analysis";
  if (a === "deterministic") return "Deterministic lookup";
  return humanize(a);
}

const KNOWLEDGE_KINDS = new Set(["decision", "assumption", "evidence", "insight", "question", "action"]);
export const isKnowledgeType = (t: string) => KNOWLEDGE_KINDS.has(t);

/** Statuses that mean an item no longer stands (history stays visible, struck through). */
const DEAD_VERB: Record<string, string> = {
  SUPERSEDED: "superseded",
  REVERSED: "reversed",
  REJECTED: "rejected",
  RETRACTED: "retracted",
  INVALIDATED: "invalidated",
  DROPPED: "dropped",
};
export const isDeadStatus = (s: string | undefined | null) => !!s && s in DEAD_VERB;
export const deadVerb = (s: string) => DEAD_VERB[s] ?? humanize(s).toLowerCase();

export interface ProvenanceGroups {
  baseCheckpoint: ProvenanceEntry[];
  cited: ProvenanceEntry[];
  inputs: ProvenanceEntry[];
  idea?: ProvenanceEntry;
  /** Distinct knowledge items the version was built from (cited + inputs). */
  itemCount: number;
  /** Knowledge items (any role) whose status is no longer live. */
  dead: ProvenanceEntry[];
}

const byLabel = (a: ProvenanceEntry, b: ProvenanceEntry) => a.label.localeCompare(b.label, undefined, { numeric: true });

export function groupProvenance(entries: ProvenanceEntry[] | undefined | null): ProvenanceGroups {
  const list = entries ?? [];
  const idea = list.find((e) => e.entity_type === "idea");
  const baseCheckpoint = list.filter((e) => e.role === "base_checkpoint");
  const cited = list.filter((e) => e.role === "cited").sort(byLabel);
  const citedIds = new Set(cited.map((e) => e.entity_id));
  const inputs = list.filter((e) => e.role === "input" && e.entity_type !== "idea" && !citedIds.has(e.entity_id)).sort(byLabel);
  const knowledge = new Set(list.filter((e) => isKnowledgeType(e.entity_type)).map((e) => e.entity_id));
  const seen = new Set<string>();
  const dead = list.filter((e) => {
    if (!isKnowledgeType(e.entity_type) || !isDeadStatus(e.status) || seen.has(e.entity_id)) return false;
    seen.add(e.entity_id);
    return true;
  });
  return { baseCheckpoint, cited, inputs, idea, itemCount: knowledge.size, dead };
}

/** Plain-text preview of Markdown (for list rows): skips the title heading, strips syntax. */
export function markdownPreview(md: string, max = 240): string {
  const text = md
    .replace(/\r\n?/g, "\n")
    .replace(/```[\s\S]*?```/g, " ")
    // Generated artifacts open with an italic byline ("_Action Plan for … assembled by IdeaVault…_"): skip it.
    .replace(/^\s*(#[^\n]*\n+)?\s*_[^_\n][^\n]*_\s*(\n|$)/, "$1")
    .split("\n")
    // Headings (incl. a heading cut off by the list endpoint's truncation, e.g. "#…") and rules.
    .filter((l) => !/^\s*#{1,6}(\s|…|$)/.test(l) && !/^\s*([-*_]\s*){3,}$/.test(l))
    .join(" ")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/^\s*>\s?/gm, "")
    .replace(/(^|\s)[-*+]\s+\[[ xX]\]\s+/g, "$1")
    .replace(/(^|\s)([-*+]|\d+\.)\s+/g, "$1")
    .replace(/(\*\*|__|~~|`)/g, "")
    .replace(/(^|[\s(])[*_]([^*_]+)[*_](?=[\s).,;:!?]|$)/g, "$1$2")
    .replace(/\s+/g, " ")
    .trim();
  return text.length > max ? `${text.slice(0, max - 1).trimEnd()}…` : text;
}

/** Drops a leading `# Title` when it repeats the page title (the header already shows it). */
export function stripLeadingTitle(md: string, title: string): string {
  const m = /^\s*#\s+(.+?)\s*#*\s*(\r?\n|$)/.exec(md);
  if (!m) return md;
  const norm = (s: string) => s.toLowerCase().replace(/\s+/g, " ").trim();
  return norm(m[1]) === norm(title) ? md.slice(m[0].length).replace(/^\s*\n/, "") : md;
}

export interface LabelRef {
  label: string;
  /** Entity type for routing (knowledge kind or "checkpoint"). */
  type: string;
  id: string;
}

/**
 * Turns bare ref labels like `[D3]` into in-app `iv://` links so `<Markdown>` renders them
 * as entity chips. Only labels present in `refs` are linked; existing links are left alone.
 */
export function linkifyLabels(md: string, refs: LabelRef[]): string {
  if (!refs.length) return md;
  const map = new Map(refs.map((r) => [r.label.toUpperCase(), r]));
  return md.replace(/\[([A-Za-z]{1,3}\d{1,5})\](?!\(|:)/g, (whole, label: string) => {
    const r = map.get(label.toUpperCase());
    return r ? `[${r.label}](iv://${r.type}/${r.id})` : whole;
  });
}
