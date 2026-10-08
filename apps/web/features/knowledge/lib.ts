// Pure helpers for knowledge items (grouping, liveness, status actions, form payloads).
import { KNOWLEDGE_KINDS, type KnowledgeItem, type KnowledgeKind } from "@/lib/types";

const DEAD = new Set(["SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED"]);

/** Live = still represents current thinking (mirrors domain.IsLiveStatus). */
export function isLive(item: Pick<KnowledgeItem, "status">): boolean {
  return !DEAD.has(item.status);
}

export function isKnowledgeKind(k: string): k is KnowledgeKind {
  return (KNOWLEDGE_KINDS as string[]).includes(k);
}

export const KIND_BLURB: Record<KnowledgeKind, string> = {
  decision: "What was chosen, and why",
  assumption: "Beliefs not yet proven",
  evidence: "Facts that support or challenge",
  insight: "What was learned",
  question: "What is still unknown",
  action: "What to do next",
};

/** Group a flat list by kind in canonical order (every kind present, possibly empty). */
export function groupByKind(items: KnowledgeItem[]): Record<KnowledgeKind, KnowledgeItem[]> {
  const out = Object.fromEntries(KNOWLEDGE_KINDS.map((k) => [k, [] as KnowledgeItem[]])) as Record<KnowledgeKind, KnowledgeItem[]>;
  for (const it of items) if (isKnowledgeKind(it.kind)) out[it.kind].push(it);
  for (const k of KNOWLEDGE_KINDS) out[k].sort(byRefDesc);
  return out;
}

/** Flatten the overview's kind → items map. */
export function flattenKnowledge(map: Record<string, KnowledgeItem[] | null | undefined> | null | undefined): KnowledgeItem[] {
  if (!map) return [];
  return Object.values(map).flatMap((v) => v ?? []);
}

function byRefDesc(a: KnowledgeItem, b: KnowledgeItem) {
  return b.ref_number - a.ref_number;
}

const LEVEL: Record<string, number> = { HIGH: 0, MEDIUM: 1, LOW: 2 };

export interface CurrentState {
  decisions: KnowledgeItem[];
  questions: KnowledgeItem[];
  assumptions: KnowledgeItem[];
  actions: KnowledgeItem[];
  totals: { decisions: number; questions: number; assumptions: number; actions: number };
}

/** The "where does this idea stand" digest: live decisions, open questions, riskiest unvalidated assumptions, next actions. */
export function currentState(items: KnowledgeItem[], limit = 5): CurrentState {
  const accepted = items.filter((i) => i.review_state !== "PROPOSED" && i.review_state !== "REJECTED");
  const decisions = accepted.filter((i) => i.kind === "decision" && i.status === "ACTIVE").sort(byRefDesc);
  const questions = accepted.filter((i) => i.kind === "question" && i.status === "OPEN").sort(byRefDesc);
  const assumptions = accepted
    .filter((i) => i.kind === "assumption" && (i.status === "UNVALIDATED" || i.status === "VALIDATING"))
    .sort((a, b) => (LEVEL[a.assumption?.risk ?? "LOW"] ?? 3) - (LEVEL[b.assumption?.risk ?? "LOW"] ?? 3) || byRefDesc(a, b));
  const actions = accepted
    .filter((i) => i.kind === "action" && (i.status === "TODO" || i.status === "IN_PROGRESS"))
    .sort(
      (a, b) =>
        Number(b.status === "IN_PROGRESS") - Number(a.status === "IN_PROGRESS") ||
        (LEVEL[a.action?.priority ?? "LOW"] ?? 3) - (LEVEL[b.action?.priority ?? "LOW"] ?? 3) ||
        byRefDesc(a, b),
    );
  return {
    decisions: decisions.slice(0, limit),
    questions: questions.slice(0, limit),
    assumptions: assumptions.slice(0, limit),
    actions: actions.slice(0, limit),
    totals: { decisions: decisions.length, questions: questions.length, assumptions: assumptions.length, actions: actions.length },
  };
}

// ---------- Status actions ----------

export interface StatusAction {
  status: string;
  label: string;
  /** Question answers need text. */
  needsAnswer?: boolean;
  tone?: "default" | "positive" | "negative";
}

/** Valid status moves for an item, phrased as verbs. SUPERSEDED is never set directly (use Supersede…). */
export function statusActions(kind: string, status: string): StatusAction[] {
  switch (kind) {
    case "decision":
      if (status === "ACTIVE") return [{ status: "REJECTED", label: "Reject", tone: "negative" }];
      if (status === "PROPOSED") return [{ status: "ACTIVE", label: "Adopt", tone: "positive" }, { status: "REJECTED", label: "Reject", tone: "negative" }];
      if (status === "REJECTED") return [{ status: "ACTIVE", label: "Reinstate" }];
      return [];
    case "assumption":
      if (status === "UNVALIDATED")
        return [
          { status: "VALIDATING", label: "Start validating" },
          { status: "VALIDATED", label: "Validated", tone: "positive" },
          { status: "INVALIDATED", label: "Invalidated", tone: "negative" },
        ];
      if (status === "VALIDATING")
        return [
          { status: "VALIDATED", label: "Validated", tone: "positive" },
          { status: "INVALIDATED", label: "Invalidated", tone: "negative" },
          { status: "UNVALIDATED", label: "Back to unvalidated" },
        ];
      if (status === "VALIDATED" || status === "INVALIDATED") return [{ status: "UNVALIDATED", label: "Reopen" }];
      return [];
    case "evidence":
      if (status === "ACTIVE") return [{ status: "DISPUTED", label: "Dispute" }, { status: "RETRACTED", label: "Retract", tone: "negative" }];
      if (status === "DISPUTED") return [{ status: "ACTIVE", label: "Reinstate", tone: "positive" }, { status: "RETRACTED", label: "Retract", tone: "negative" }];
      if (status === "RETRACTED") return [{ status: "ACTIVE", label: "Reinstate" }];
      return [];
    case "insight":
      if (status === "ACTIVE") return [{ status: "RETRACTED", label: "Retract", tone: "negative" }];
      if (status === "RETRACTED") return [{ status: "ACTIVE", label: "Reinstate" }];
      return [];
    case "question":
      if (status === "OPEN") return [{ status: "ANSWERED", label: "Answer…", needsAnswer: true, tone: "positive" }, { status: "DROPPED", label: "Drop", tone: "negative" }];
      if (status === "ANSWERED" || status === "DROPPED") return [{ status: "OPEN", label: "Reopen" }];
      return [];
    case "action":
      if (status === "TODO")
        return [
          { status: "IN_PROGRESS", label: "Start" },
          { status: "DONE", label: "Complete", tone: "positive" },
          { status: "DROPPED", label: "Drop", tone: "negative" },
        ];
      if (status === "IN_PROGRESS")
        return [
          { status: "DONE", label: "Complete", tone: "positive" },
          { status: "TODO", label: "Back to to-do" },
          { status: "DROPPED", label: "Drop", tone: "negative" },
        ];
      if (status === "DONE" || status === "DROPPED") return [{ status: "TODO", label: "Reopen" }];
      return [];
  }
  return [];
}

// ---------- Record / supersede form ----------

export type Level = "LOW" | "MEDIUM" | "HIGH";

export interface KnowledgeFormState {
  kind: KnowledgeKind;
  statement: string;
  details: string;
  origin: "SOURCE" | "INTERPRETATION";
  rationale: string;
  alternatives: { option: string; reason: string }[];
  risk: Level;
  validationMethod: string;
  stance: "SUPPORTS" | "CHALLENGES" | "NEUTRAL";
  strength: "WEAK" | "MODERATE" | "STRONG";
  url: string;
  targetItemId: string;
  importance: Level;
  priority: Level;
  /** yyyy-mm-dd from a date input. */
  dueAt: string;
}

export function emptyKnowledgeForm(kind: KnowledgeKind = "decision"): KnowledgeFormState {
  return {
    kind,
    statement: "",
    details: "",
    origin: "SOURCE",
    rationale: "",
    alternatives: [],
    risk: "MEDIUM",
    validationMethod: "",
    stance: "SUPPORTS",
    strength: "MODERATE",
    url: "",
    targetItemId: "",
    importance: "MEDIUM",
    priority: "MEDIUM",
    dueAt: "",
  };
}

export interface KnowledgePayload {
  idea_id: string;
  branch_id?: string;
  kind: KnowledgeKind;
  statement: string;
  details?: string;
  origin: "SOURCE" | "INTERPRETATION";
  supersedes?: string;
  reverses?: boolean;
  rationale?: string;
  alternatives?: { option: string; reason_rejected?: string }[];
  risk?: string;
  validation_method?: string;
  stance?: string;
  strength?: string;
  url?: string;
  target_item_id?: string;
  importance?: string;
  priority?: string;
  due_at?: string;
}

export interface BuildResult {
  payload: KnowledgePayload | null;
  errors: Partial<Record<"statement" | "url" | "dueAt", string>>;
}

/** Validate a knowledge form and build the POST /v1/knowledge body with only the fields relevant to its kind. */
export function buildKnowledgePayload(
  f: KnowledgeFormState,
  ctx: { ideaId: string; branchId?: string | null; supersedes?: string | null; reverses?: boolean },
): BuildResult {
  const errors: BuildResult["errors"] = {};
  const statement = f.statement.trim();
  if (!statement) errors.statement = "Say what it is in one sentence.";
  else if (statement.length > 4000) errors.statement = "Keep the statement under 4000 characters — put the rest in details.";
  const url = f.url.trim();
  if (f.kind === "evidence" && url && !/^https?:\/\//i.test(url)) errors.url = "Links must start with http:// or https://";
  let due: string | undefined;
  if (f.kind === "action" && f.dueAt) {
    const d = new Date(`${f.dueAt}T12:00:00Z`);
    if (Number.isNaN(d.getTime())) errors.dueAt = "Not a valid date.";
    else due = d.toISOString();
  }
  if (Object.keys(errors).length) return { payload: null, errors };

  const p: KnowledgePayload = { idea_id: ctx.ideaId, kind: f.kind, statement, origin: f.origin };
  if (ctx.branchId) p.branch_id = ctx.branchId;
  if (f.details.trim()) p.details = f.details.trim();
  if (ctx.supersedes) {
    p.supersedes = ctx.supersedes;
    if (f.kind === "decision" && ctx.reverses) p.reverses = true;
  }
  switch (f.kind) {
    case "decision": {
      if (f.rationale.trim()) p.rationale = f.rationale.trim();
      const alts = f.alternatives
        .map((a) => ({ option: a.option.trim(), reason: a.reason.trim() }))
        .filter((a) => a.option)
        .map((a) => (a.reason ? { option: a.option, reason_rejected: a.reason } : { option: a.option }));
      if (alts.length) p.alternatives = alts;
      break;
    }
    case "assumption":
      p.risk = f.risk;
      if (f.validationMethod.trim()) p.validation_method = f.validationMethod.trim();
      break;
    case "evidence":
      p.stance = f.stance;
      p.strength = f.strength;
      if (url) p.url = url;
      if (f.targetItemId) p.target_item_id = f.targetItemId;
      break;
    case "insight":
      p.importance = f.importance;
      break;
    case "action":
      p.priority = f.priority;
      if (due) p.due_at = due;
      break;
  }
  // Supersession rationale is meaningful for every kind (stored on the supersedes edge).
  if (ctx.supersedes && f.kind !== "decision" && f.rationale.trim()) p.rationale = f.rationale.trim();
  return { payload: p, errors };
}
