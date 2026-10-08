import { ARTIFACT_TYPES, type ArtifactType } from "@/lib/types";
import { humanize } from "@/lib/format";

/** Human names + one-liners for every artifact type (mirrors /v1/artifacts/templates). */
export const ARTIFACT_INFO: Record<ArtifactType, { name: string; description: string }> = {
  ACTION_PLAN: { name: "Action Plan", description: "Concrete, sequenced next steps from decisions and open questions." },
  IMPLEMENTATION_PROMPT: { name: "Implementation Prompt", description: "A self-contained brief to hand to a coding agent." },
  PRODUCT_BRIEF: { name: "Product Brief", description: "Problem, users, value, scope and success metrics." },
  RESEARCH_REPORT: { name: "Research Report", description: "Evidence gathered and what it supports or challenges." },
  STRATEGY: { name: "Strategy Document", description: "Direction, choices, trade-offs and how to win." },
  TECHNICAL_SPEC: { name: "Technical Specification", description: "Requirements, design, interfaces and constraints." },
  ARCHITECTURE: { name: "Architecture Document", description: "System structure, components, data and decisions." },
  DECISION_MEMO: { name: "Decision Memo", description: "Decisions, rationale, alternatives and how they evolved." },
  PROPOSAL: { name: "Proposal", description: "A persuasive, grounded proposal." },
  CHECKLIST: { name: "Checklist", description: "Everything to do or verify, as checkboxes." },
  MEETING_BRIEF: { name: "Meeting Brief", description: "Context, decisions needed and questions for a meeting." },
  EXECUTIVE_SUMMARY: { name: "Executive Summary", description: "A one-page summary of the idea and its state." },
  EXPERIMENT_PLAN: { name: "Experiment Plan", description: "Experiments to validate the riskiest assumptions." },
  REQUIREMENTS: { name: "Requirements Document", description: "Functional and non-functional requirements." },
  REFERENCE: { name: "Reference Document", description: "Everything known, organised for later retrieval." },
  CUSTOM: { name: "Custom Document", description: "Shaped entirely by your instructions." },
};

export const ALL_ARTIFACT_TYPES = ARTIFACT_TYPES as readonly ArtifactType[];

export function artifactTypeName(t: string): string {
  return ARTIFACT_INFO[t as ArtifactType]?.name ?? humanize(t);
}

/** "template:v1" → Template, "llm:groq/llama" → AI · groq/llama, "user" → Edited by you. */
export function generatorLabel(g: string | undefined | null): { label: string; detail?: string; ai: boolean } {
  if (!g) return { label: "Unknown", ai: false };
  if (g.startsWith("llm:")) return { label: "AI", detail: g.slice(4), ai: true };
  if (g.startsWith("template")) return { label: "Template", detail: g.split(":")[1], ai: false };
  if (g === "user") return { label: "Written by you", ai: false };
  return { label: humanize(g), ai: false };
}
