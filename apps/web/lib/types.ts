// Domain type aliases generated from the API's OpenAPI document
// (packages/contracts/openapi.json → types/api.generated.ts via `npm run gen:api`).
import type { components } from "@/types/api.generated";

type S = components["schemas"];

export type User = S["User"];
export type Idea = S["Idea"];
export type IdeaStats = S["IdeaStats"];
export type IdeaVersion = S["IdeaVersion"];
export type Branch = S["Branch"];
export type Checkpoint = S["Checkpoint"];
export type Snapshot = S["Snapshot"];
export type CheckpointDiff = S["CheckpointDiff"];
export type KnowledgeItem = S["KnowledgeItem"];
export type Relationship = S["Relationship"];
export type Conversation = S["Conversation"];
export type Message = S["Message"];
export type Artifact = S["Artifact"];
export type ArtifactVersion = S["ArtifactVersion"];
export type ProvenanceEntry = S["ProvenanceEntry"];
export type ContextPack = S["ContextPack"];
export type Delta = S["Delta"];
export type DeltaItem = S["DeltaItem"];
export type Analysis = S["Analysis"];
export type ActivityEvent = S["ActivityEvent"];
export type EntityRef = S["EntityRef"];
export type InheritanceRecord = S["InheritanceRecord"];
export type ConclusionRecord = S["ConclusionRecord"];
export type ModelConfig = S["ModelConfig"];
export type UsageEvent = S["UsageEvent"];
export type ToolCallRecord = S["ToolCallRecord"];
export type AgentRun = S["AgentRun"];
export type TraceEvent = S["TraceEvent"];
export type Import = S["Import"];
export type ImportItem = S["ImportItem"];
export type ConnectorEvent = S["ConnectorEvent"];
export type Session = S["Session"];

export type IdeaOverview = S["ServiceIdeaOverview"];
export type IdeaCreated = S["ServiceIdeaCreated"];
export type IdeaCandidate = S["ServiceIdeaCandidate"];
export type ForkResult = S["ServiceForkResult"];
export type BranchTree = S["ServiceBranchTree"];
export type BranchComparison = S["ServiceBranchComparison"];
export type KnowledgeExplanation = S["ServiceKnowledgeExplanation"];
export type JourneyNode = S["ServiceJourneyNode"];
export type ArtifactResult = S["ServiceArtifactResult"];
export type ArtifactExplanation = S["ServiceArtifactExplanation"];
export type ConclusionResult = S["ServiceConclusionResult"];
export type MergeDeltaResult = S["ServiceMergeDeltaResult"];
export type ImportDetail = S["ServiceImportDetail"];
export type SearchResponse = S["ServiceSearchResponse"];
export type TodayView = S["ServiceTodayView"];
export type TimelineView = S["ServiceTimelineView"];
export type DecisionsView = S["ServiceDecisionsView"];
export type DecisionEntry = S["ServiceDecisionEntry"];
export type LearningView = S["ServiceLearningView"];
export type Graph = S["ServiceGraph"];
export type GraphNode = S["ServiceGraphNode"];
export type GraphEdge = S["ServiceGraphEdge"];
export type PromptAnalysis = S["ServicePromptAnalysis"];
export type ThinkingAnalysis = S["ServiceThinkingAnalysis"];
export type IdeaEvolution = S["ServiceIdeaEvolution"];
export type ProviderStatus = S["ServiceProviderStatus"];
export type RoutingPreview = S["ServiceRoutingPreview"];
export type UsageSummary = S["ServiceUsageSummary"];
export type ConnectorView = S["ServiceConnectorView"];
export type IdeaMomentum = S["PostgresIdeaMomentum"];
export type LabRun = S["PostgresLabRun"];
export type LabResult = S["PostgresLabResult"];
export type UsageGroup = S["PostgresUsageGroup"];
export type SearchHit = S["PostgresSearchHit"];
export type Selection = S["ServiceSelection"];

export type KnowledgeKind = "decision" | "assumption" | "evidence" | "insight" | "question" | "action";
export const KNOWLEDGE_KINDS: KnowledgeKind[] = ["decision", "assumption", "evidence", "insight", "question", "action"];

export type IdeaStatus =
  | "EXPLORING"
  | "ACTIVE"
  | "DECIDED"
  | "READY_TO_IMPLEMENT"
  | "CONCLUDED"
  | "PARKED"
  | "ABANDONED"
  | "MERGED";

export const IDEA_STATUSES: IdeaStatus[] = ["EXPLORING", "ACTIVE", "DECIDED", "READY_TO_IMPLEMENT", "CONCLUDED", "PARKED", "ABANDONED", "MERGED"];

export const OUTCOMES = [
  "IMPLEMENT",
  "ACTION_PLAN",
  "RESEARCH_COMPLETE",
  "DECISION",
  "PROPOSAL",
  "REFERENCE",
  "PARK",
  "ABANDON",
  "MERGE",
  "OTHER",
] as const;
export type Outcome = (typeof OUTCOMES)[number];

export const ARTIFACT_TYPES = [
  "ACTION_PLAN",
  "IMPLEMENTATION_PROMPT",
  "PRODUCT_BRIEF",
  "RESEARCH_REPORT",
  "STRATEGY",
  "TECHNICAL_SPEC",
  "ARCHITECTURE",
  "DECISION_MEMO",
  "PROPOSAL",
  "CHECKLIST",
  "MEETING_BRIEF",
  "EXECUTIVE_SUMMARY",
  "EXPERIMENT_PLAN",
  "REQUIREMENTS",
  "REFERENCE",
  "CUSTOM",
] as const;
export type ArtifactType = (typeof ARTIFACT_TYPES)[number];

/** Server-sent agent events. */
export type AgentEvent =
  | { type: "run_started"; data: { run_id: string; conversation_id: string; user_message_id?: string; conversation_title?: string; resumed?: boolean } }
  | { type: "token"; data: { text: string } }
  | { type: "trace"; data: TraceEvent }
  | { type: "confirmation_required"; data: { tool_call_id: string; tool: string; category: string; arguments: Record<string, unknown>; description: string; run_id: string } }
  | { type: "message"; data: { id: string; content: string; refs?: EntityRef[]; trace?: { label: string; tool?: string; status?: string; kind?: string }[]; model?: string; provider?: string; created_at?: string } }
  | { type: "run_completed"; data: { run_id: string; status: string; usage?: { input_tokens: number; output_tokens: number }; model?: string; provider?: string; focus?: { idea_id?: string; branch_id?: string; checkpoint_id?: string } } }
  | { type: "error"; data: { message: string } }
  | { type: "done"; data: Record<string, never> };
