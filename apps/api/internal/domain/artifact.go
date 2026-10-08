package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// ArtifactType enumerates first-class outcome documents.
type ArtifactType string

const (
	ArtifactActionPlan           ArtifactType = "ACTION_PLAN"
	ArtifactImplementationPrompt ArtifactType = "IMPLEMENTATION_PROMPT"
	ArtifactProductBrief         ArtifactType = "PRODUCT_BRIEF"
	ArtifactResearchReport       ArtifactType = "RESEARCH_REPORT"
	ArtifactStrategy             ArtifactType = "STRATEGY"
	ArtifactTechnicalSpec        ArtifactType = "TECHNICAL_SPEC"
	ArtifactArchitecture         ArtifactType = "ARCHITECTURE"
	ArtifactDecisionMemo         ArtifactType = "DECISION_MEMO"
	ArtifactProposal             ArtifactType = "PROPOSAL"
	ArtifactChecklist            ArtifactType = "CHECKLIST"
	ArtifactMeetingBrief         ArtifactType = "MEETING_BRIEF"
	ArtifactExecutiveSummary     ArtifactType = "EXECUTIVE_SUMMARY"
	ArtifactExperimentPlan       ArtifactType = "EXPERIMENT_PLAN"
	ArtifactRequirements         ArtifactType = "REQUIREMENTS"
	ArtifactReference            ArtifactType = "REFERENCE"
	ArtifactCustom               ArtifactType = "CUSTOM"
)

// AllArtifactTypes lists every artifact type.
var AllArtifactTypes = []ArtifactType{
	ArtifactActionPlan, ArtifactImplementationPrompt, ArtifactProductBrief, ArtifactResearchReport,
	ArtifactStrategy, ArtifactTechnicalSpec, ArtifactArchitecture, ArtifactDecisionMemo, ArtifactProposal,
	ArtifactChecklist, ArtifactMeetingBrief, ArtifactExecutiveSummary, ArtifactExperimentPlan,
	ArtifactRequirements, ArtifactReference, ArtifactCustom,
}

// Valid reports whether t is a known artifact type.
func (t ArtifactType) Valid() bool {
	for _, x := range AllArtifactTypes {
		if x == t {
			return true
		}
	}
	return false
}

// ParseArtifactType accepts loose spellings ("action plan", "implementation-prompt", "spec").
func ParseArtifactType(s string) (ArtifactType, bool) {
	n := strings.ToUpper(strings.NewReplacer(" ", "_", "-", "_").Replace(strings.TrimSpace(s)))
	if ArtifactType(n).Valid() {
		return ArtifactType(n), true
	}
	aliases := map[string]ArtifactType{
		"PLAN": ArtifactActionPlan, "MVP_PLAN": ArtifactActionPlan, "ROADMAP": ArtifactActionPlan,
		"PROMPT": ArtifactImplementationPrompt, "CODING_PROMPT": ArtifactImplementationPrompt, "AGENT_PROMPT": ArtifactImplementationPrompt,
		"BRIEF": ArtifactProductBrief, "PRD": ArtifactRequirements, "SPEC": ArtifactTechnicalSpec, "SPECIFICATION": ArtifactTechnicalSpec,
		"TECHNICAL_SPECIFICATION": ArtifactTechnicalSpec, "ARCHITECTURE_DOCUMENT": ArtifactArchitecture, "MEMO": ArtifactDecisionMemo,
		"RESEARCH": ArtifactResearchReport, "REPORT": ArtifactResearchReport, "SUMMARY": ArtifactExecutiveSummary,
		"EXPERIMENT": ArtifactExperimentPlan, "REQUIREMENTS_DOCUMENT": ArtifactRequirements, "STRATEGY_DOCUMENT": ArtifactStrategy,
		"DOC": ArtifactCustom, "DOCUMENT": ArtifactCustom, "MARKDOWN": ArtifactCustom,
	}
	if t, ok := aliases[n]; ok {
		return t, true
	}
	return "", false
}

// HumanName renders the type for display ("Action Plan").
func (t ArtifactType) HumanName() string {
	words := strings.Split(strings.ToLower(string(t)), "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// ArtifactStatus is the editorial state of an artifact.
type ArtifactStatus string

const (
	ArtifactDraft    ArtifactStatus = "DRAFT"
	ArtifactFinal    ArtifactStatus = "FINAL"
	ArtifactArchived ArtifactStatus = "ARCHIVED"
)

// Artifact is a Markdown document produced by thinking. Content lives in PostgreSQL.
type Artifact struct {
	ID              uuid.UUID      `json:"id"`
	UserID          uuid.UUID      `json:"-"`
	IdeaID          uuid.UUID      `json:"idea_id"`
	BranchID        *uuid.UUID     `json:"branch_id,omitempty"`
	CheckpointID    *uuid.UUID     `json:"checkpoint_id,omitempty"`
	Type            ArtifactType   `json:"type"`
	Title           string         `json:"title"`
	ContentMarkdown string         `json:"content_markdown"`
	Status          ArtifactStatus `json:"status"`
	CurrentVersion  int            `json:"current_version"`
	Generator       string         `json:"generator"`
	Instructions    string         `json:"instructions"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	IdeaTitle       string         `json:"idea_title,omitempty"`
	BranchName      string         `json:"branch_name,omitempty"`
	CheckpointLabel string         `json:"checkpoint_label,omitempty"`
}

// ArtifactVersion is an immutable version of an artifact's content.
type ArtifactVersion struct {
	ID                 uuid.UUID  `json:"id"`
	ArtifactID         uuid.UUID  `json:"artifact_id"`
	Version            int        `json:"version"`
	Title              string     `json:"title"`
	ContentMarkdown    string     `json:"content_markdown"`
	ChangeNote         string     `json:"change_note"`
	CreatedBy          Actor      `json:"created_by"`
	Generator          string     `json:"generator"`
	SourceCheckpointID *uuid.UUID `json:"source_checkpoint_id,omitempty"`
	AgentRunID         *uuid.UUID `json:"agent_run_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

// ProvenanceEntry links an artifact version to the thinking that produced it.
type ProvenanceEntry struct {
	EntityType EntityType `json:"entity_type"`
	EntityID   uuid.UUID  `json:"entity_id"`
	Label      string     `json:"label"`
	Role       string     `json:"role"` // input | cited | base_checkpoint
	Title      string     `json:"title,omitempty"`
	Status     string     `json:"status,omitempty"`
}

// ContextPack is a portable representation of current thinking for external AI tools.
type ContextPack struct {
	ID              uuid.UUID      `json:"id"`
	UserID          uuid.UUID      `json:"-"`
	IdeaID          uuid.UUID      `json:"idea_id"`
	BranchID        *uuid.UUID     `json:"branch_id,omitempty"`
	CheckpointID    *uuid.UUID     `json:"checkpoint_id,omitempty"`
	Title           string         `json:"title"`
	Objective       string         `json:"objective"`
	ContentMarkdown string         `json:"content_markdown"`
	ContentJSON     map[string]any `json:"content_json"`
	TokenEstimate   int            `json:"token_estimate"`
	CreatedAt       time.Time      `json:"created_at"`
}

// DeltaClass classifies how external thinking relates to the current branch.
type DeltaClass string

const (
	DeltaNew       DeltaClass = "NEW"
	DeltaChanged   DeltaClass = "CHANGED"
	DeltaRejected  DeltaClass = "REJECTED"
	DeltaUnchanged DeltaClass = "UNCHANGED"
)

// Delta is an analysis of an external AI conversation against a branch, pending review.
type Delta struct {
	ID                uuid.UUID   `json:"id"`
	IdeaID            uuid.UUID   `json:"idea_id"`
	BranchID          uuid.UUID   `json:"branch_id"`
	ConversationID    *uuid.UUID  `json:"conversation_id,omitempty"`
	SourceID          *uuid.UUID  `json:"source_id,omitempty"`
	ContextPackID     *uuid.UUID  `json:"context_pack_id,omitempty"`
	Status            string      `json:"status"` // PENDING_REVIEW | MERGED | PARTIALLY_MERGED | DISCARDED
	Summary           string      `json:"summary"`
	Analyzer          string      `json:"analyzer"`
	MergeCheckpointID *uuid.UUID  `json:"merge_checkpoint_id,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	ResolvedAt        *time.Time  `json:"resolved_at,omitempty"`
	Items             []DeltaItem `json:"items,omitempty"`
}

// DeltaItem is one detected change.
type DeltaItem struct {
	ID              uuid.UUID     `json:"id"`
	DeltaID         uuid.UUID     `json:"delta_id"`
	Classification  DeltaClass    `json:"classification"`
	Kind            KnowledgeKind `json:"kind"`
	Statement       string        `json:"statement"`
	Details         string        `json:"details"`
	Rationale       string        `json:"rationale"`
	SourceExcerpt   string        `json:"source_excerpt"`
	TargetItemID    *uuid.UUID    `json:"target_item_id,omitempty"`
	TargetLabel     string        `json:"target_label,omitempty"`
	TargetStatement string        `json:"target_statement,omitempty"`
	Confidence      *float32      `json:"confidence,omitempty"`
	Selected        bool          `json:"selected"`
	MergedItemID    *uuid.UUID    `json:"merged_item_id,omitempty"`
	Position        int           `json:"position"`
}

// Analysis is a stored result of prompt/thinking/evolution/contradiction analysis.
type Analysis struct {
	ID          uuid.UUID      `json:"id"`
	Kind        string         `json:"kind"`
	IdeaID      *uuid.UUID     `json:"idea_id,omitempty"`
	BranchID    *uuid.UUID     `json:"branch_id,omitempty"`
	SubjectType string         `json:"subject_type"`
	SubjectID   *uuid.UUID     `json:"subject_id,omitempty"`
	Input       map[string]any `json:"input"`
	Result      map[string]any `json:"result"`
	Analyzer    string         `json:"analyzer"`
	CreatedAt   time.Time      `json:"created_at"`
}
