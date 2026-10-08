// Idea lifecycle rules mirrored from the API (apps/api/internal/domain/idea.go).
// The server is the source of truth and validates every change; these mirrors only
// let the UI explain *why* an option is unavailable before the user tries it.
import { humanize } from "@/lib/format";
import { IDEA_STATUSES, type IdeaStatus, type KnowledgeItem, type Outcome } from "@/lib/types";

export const OPEN_STATUSES: IdeaStatus[] = ["EXPLORING", "ACTIVE", "DECIDED", "READY_TO_IMPLEMENT"];
export const CLOSED_STATUSES: IdeaStatus[] = ["CONCLUDED", "PARKED", "ABANDONED", "MERGED"];

const TRANSITIONS: Record<IdeaStatus, IdeaStatus[]> = {
  EXPLORING: ["ACTIVE", "DECIDED", "READY_TO_IMPLEMENT", "CONCLUDED", "PARKED", "ABANDONED", "MERGED"],
  ACTIVE: ["EXPLORING", "DECIDED", "READY_TO_IMPLEMENT", "CONCLUDED", "PARKED", "ABANDONED", "MERGED"],
  DECIDED: ["EXPLORING", "ACTIVE", "READY_TO_IMPLEMENT", "CONCLUDED", "PARKED", "ABANDONED", "MERGED"],
  READY_TO_IMPLEMENT: ["EXPLORING", "ACTIVE", "DECIDED", "CONCLUDED", "PARKED", "ABANDONED", "MERGED"],
  CONCLUDED: ["EXPLORING", "ACTIVE"],
  PARKED: ["EXPLORING", "ACTIVE", "ABANDONED", "MERGED"],
  ABANDONED: ["EXPLORING", "ACTIVE"],
  MERGED: [],
};

export function isOpenStatus(s: string): boolean {
  return (OPEN_STATUSES as string[]).includes(s);
}

/** Can be reopened via POST /reopen (moves to ACTIVE). */
export function isReopenable(s: string): boolean {
  return s === "CONCLUDED" || s === "PARKED" || s === "ABANDONED";
}

export function canTransition(from: string, to: string): boolean {
  if (from === to) return true;
  return (TRANSITIONS[from as IdeaStatus] ?? []).includes(to as IdeaStatus);
}

export const STATUS_HINT: Record<IdeaStatus, string> = {
  EXPLORING: "Still figuring out what it is",
  ACTIVE: "Actively thinking and deciding",
  DECIDED: "The key decisions are made",
  READY_TO_IMPLEMENT: "Ready to hand off and build",
  CONCLUDED: "Intentionally wrapped up",
  PARKED: "Paused — nothing is lost",
  ABANDONED: "No longer pursued",
  MERGED: "Folded into another idea",
};

/** Options for the manual status dropdown. MERGED is only reachable through Conclude → Merge. */
export function statusOptions(current: string): { status: IdeaStatus; allowed: boolean; reason?: string }[] {
  return IDEA_STATUSES.filter((s) => s !== "MERGED").map((s) => {
    if (s === current) return { status: s, allowed: false, reason: "Current status" };
    if (current === "MERGED") return { status: s, allowed: false, reason: "Merged ideas are final" };
    const allowed = canTransition(current, s);
    return { status: s, allowed, reason: allowed ? undefined : `Not reachable from ${humanize(current).toLowerCase()}` };
  });
}

// ---------- Conclusion ----------

export interface OutcomeInfo {
  outcome: Outcome;
  label: string;
  description: string;
  status: IdeaStatus;
  /** Artifacts suggested by default for this outcome. */
  artifacts: string[];
}

export const OUTCOME_INFO: OutcomeInfo[] = [
  { outcome: "IMPLEMENT", label: "Implement", description: "The decisions are firm enough to build.", status: "READY_TO_IMPLEMENT", artifacts: ["IMPLEMENTATION_PROMPT", "TECHNICAL_SPEC"] },
  { outcome: "ACTION_PLAN", label: "Action plan", description: "Turn decisions and open actions into sequenced steps.", status: "CONCLUDED", artifacts: ["ACTION_PLAN"] },
  { outcome: "RESEARCH_COMPLETE", label: "Research complete", description: "The investigation is done; keep what was learned.", status: "CONCLUDED", artifacts: ["RESEARCH_REPORT"] },
  { outcome: "DECISION", label: "Decision", description: "The idea resolved into a clear decision.", status: "DECIDED", artifacts: ["DECISION_MEMO"] },
  { outcome: "PROPOSAL", label: "Proposal", description: "Package it for someone else to decide on.", status: "CONCLUDED", artifacts: ["PROPOSAL"] },
  { outcome: "REFERENCE", label: "Reference", description: "Keep it as reference material for later.", status: "CONCLUDED", artifacts: ["REFERENCE"] },
  { outcome: "PARK", label: "Park", description: "Pause it without losing anything. Reopen any time.", status: "PARKED", artifacts: [] },
  { outcome: "ABANDON", label: "Abandon", description: "Stop pursuing it; the reasoning stays on record.", status: "ABANDONED", artifacts: [] },
  { outcome: "MERGE", label: "Merge", description: "Fold this thinking into another idea.", status: "MERGED", artifacts: [] },
  { outcome: "OTHER", label: "Other", description: "Something else — describe it in the note.", status: "CONCLUDED", artifacts: [] },
];

export function outcomeInfo(o: string | null | undefined): OutcomeInfo | undefined {
  return OUTCOME_INFO.find((x) => x.outcome === o);
}

export function statusForOutcome(o: Outcome): IdeaStatus {
  return outcomeInfo(o)?.status ?? "CONCLUDED";
}

export interface ConcludeDraft {
  ideaId: string;
  currentStatus: string;
  outcome: Outcome | null;
  mergeIntoIdeaId: string | null;
}

export interface ConcludeValidation {
  ok: boolean;
  errors: { outcome?: string; merge?: string };
}

export function validateConclude(d: ConcludeDraft): ConcludeValidation {
  const errors: ConcludeValidation["errors"] = {};
  if (d.currentStatus === "MERGED") {
    errors.outcome = "This idea is already merged into another idea.";
  } else if (!d.outcome) {
    errors.outcome = "Choose how this idea ends.";
  } else {
    const to = statusForOutcome(d.outcome);
    if (!canTransition(d.currentStatus, to)) {
      errors.outcome = `A ${humanize(d.currentStatus).toLowerCase()} idea can't become ${humanize(to).toLowerCase()} — reopen it first.`;
    }
    if (d.outcome === "MERGE") {
      if (!d.mergeIntoIdeaId) errors.merge = "Pick the idea to merge into.";
      else if (d.mergeIntoIdeaId === d.ideaId) errors.merge = "An idea can't be merged into itself.";
    }
  }
  return { ok: Object.keys(errors).length === 0, errors };
}

const LIVE_DEAD = new Set(["SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED"]);

/** Mirror of service.SuggestOutcome: a gentle default based on the branch's live knowledge. */
export function suggestOutcome(items: KnowledgeItem[]): { outcome: Outcome; reason: string } {
  let decisions = 0,
    openQ = 0,
    actions = 0,
    evidence = 0,
    insights = 0;
  for (const it of items) {
    if (LIVE_DEAD.has(it.status) || it.review_state !== "ACCEPTED") continue;
    if (it.kind === "decision") decisions++;
    else if (it.kind === "question") openQ++;
    else if (it.kind === "action" && (it.status === "TODO" || it.status === "IN_PROGRESS")) actions++;
    else if (it.kind === "evidence") evidence++;
    else if (it.kind === "insight") insights++;
  }
  if (decisions >= 3 && actions >= 2) return { outcome: "ACTION_PLAN", reason: "Several decisions and open actions — a plan turns them into execution." };
  if (decisions >= 3) return { outcome: "IMPLEMENT", reason: "Enough decisions are recorded to hand this to implementation." };
  if (evidence + insights >= 3 && decisions === 0) return { outcome: "RESEARCH_COMPLETE", reason: "Mostly evidence and insights — this reads as completed research." };
  if (decisions >= 1) return { outcome: "DECISION", reason: "The idea resolved into a decision." };
  if (openQ > 0) return { outcome: "PARK", reason: "No decisions yet and questions remain — parking keeps it for later." };
  return { outcome: "REFERENCE", reason: "Keep it as a reference for later." };
}
