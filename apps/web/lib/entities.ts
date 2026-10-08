// Visual + routing metadata for graph entities.
import {
  Bookmark,
  Box,
  CheckSquare,
  CircleHelp,
  FileText,
  FlaskConical,
  GitBranch,
  Lightbulb,
  MessagesSquare,
  Package,
  Scale,
  Sparkles,
  type LucideIcon,
} from "lucide-react";

export type EntityType =
  | "idea"
  | "branch"
  | "checkpoint"
  | "conversation"
  | "message"
  | "source"
  | "decision"
  | "assumption"
  | "evidence"
  | "insight"
  | "question"
  | "action"
  | "artifact"
  | "context_pack"
  | "delta";

export interface EntityMeta {
  label: string;
  plural: string;
  icon: LucideIcon;
  /** CSS variable name suffix, e.g. "decision" → var(--k-decision). */
  tone: string;
}

export const ENTITY_META: Record<EntityType, EntityMeta> = {
  idea: { label: "Idea", plural: "Ideas", icon: Sparkles, tone: "idea" },
  branch: { label: "Branch", plural: "Branches", icon: GitBranch, tone: "branch" },
  checkpoint: { label: "Checkpoint", plural: "Checkpoints", icon: Bookmark, tone: "checkpoint" },
  conversation: { label: "Conversation", plural: "Conversations", icon: MessagesSquare, tone: "conversation" },
  message: { label: "Message", plural: "Messages", icon: MessagesSquare, tone: "conversation" },
  source: { label: "Source", plural: "Sources", icon: Box, tone: "conversation" },
  decision: { label: "Decision", plural: "Decisions", icon: Scale, tone: "decision" },
  assumption: { label: "Assumption", plural: "Assumptions", icon: FlaskConical, tone: "assumption" },
  evidence: { label: "Evidence", plural: "Evidence", icon: FileText, tone: "evidence" },
  insight: { label: "Insight", plural: "Insights", icon: Lightbulb, tone: "insight" },
  question: { label: "Question", plural: "Questions", icon: CircleHelp, tone: "question" },
  action: { label: "Action", plural: "Actions", icon: CheckSquare, tone: "action" },
  artifact: { label: "Artifact", plural: "Artifacts", icon: FileText, tone: "artifact" },
  context_pack: { label: "Context pack", plural: "Context packs", icon: Package, tone: "checkpoint" },
  delta: { label: "Delta", plural: "Deltas", icon: GitBranch, tone: "branch" },
};

export function entityMeta(t: string): EntityMeta {
  return ENTITY_META[(t as EntityType) in ENTITY_META ? (t as EntityType) : "idea"];
}

/** CSS color for an entity type. */
export function tone(t: string, soft = false): string {
  return `var(--k-${entityMeta(t).tone}${soft ? "-soft" : ""})`;
}

const KNOWLEDGE = new Set(["decision", "assumption", "evidence", "insight", "question", "action"]);
export const isKnowledge = (t: string) => KNOWLEDGE.has(t);

/**
 * App route for an entity. Knowledge items open in the knowledge drawer via ?item=;
 * we route to the global knowledge page which resolves the idea.
 */
export function entityHref(type: string, id: string, ideaId?: string | null): string {
  switch (type) {
    case "idea":
      return `/ideas/${id}`;
    case "branch":
      return ideaId ? `/ideas/${ideaId}?branch=${id}` : `/branches/${id}`;
    case "checkpoint":
      return `/checkpoints/${id}`;
    case "conversation":
      return `/conversations/${id}`;
    case "artifact":
      return `/artifacts/${id}`;
    case "context_pack":
      return `/context-packs/${id}`;
    case "delta":
      return `/deltas/${id}`;
    case "message":
      return `/messages/${id}`;
    default:
      if (KNOWLEDGE.has(type)) return `/knowledge/${id}`;
      return "/";
  }
}

/** Parse an in-app link "iv://decision/<uuid>" into {type, id}. */
export function parseIvLink(href: string | undefined): { type: string; id: string } | null {
  if (!href) return null;
  const m = /^iv:\/\/([a-z_]+)\/([0-9a-f-]{36})/i.exec(href);
  return m ? { type: m[1].toLowerCase(), id: m[2] } : null;
}
