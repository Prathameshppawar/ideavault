package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// KnowledgeKind is one of the six explicit semantic knowledge types.
type KnowledgeKind string

const (
	KindDecision   KnowledgeKind = "decision"
	KindAssumption KnowledgeKind = "assumption"
	KindEvidence   KnowledgeKind = "evidence"
	KindInsight    KnowledgeKind = "insight"
	KindQuestion   KnowledgeKind = "question"
	KindAction     KnowledgeKind = "action"
)

// AllKnowledgeKinds lists kinds in their canonical display order.
var AllKnowledgeKinds = []KnowledgeKind{KindDecision, KindAssumption, KindEvidence, KindInsight, KindQuestion, KindAction}

// ParseKnowledgeKind accepts singular/plural and loose spellings ("decisions", "open_questions").
func ParseKnowledgeKind(s string) (KnowledgeKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "decision", "decisions":
		return KindDecision, true
	case "assumption", "assumptions":
		return KindAssumption, true
	case "evidence", "evidences":
		return KindEvidence, true
	case "insight", "insights", "learning", "learnings", "lesson", "lessons":
		return KindInsight, true
	case "question", "questions", "open_question", "open_questions":
		return KindQuestion, true
	case "action", "actions", "action_item", "action_items", "task", "tasks", "next_step", "next_steps":
		return KindAction, true
	}
	return "", false
}

// Valid reports whether k is a known kind.
func (k KnowledgeKind) Valid() bool {
	_, ok := ParseKnowledgeKind(string(k))
	return ok && k == KnowledgeKind(strings.ToLower(string(k)))
}

// EntityType returns the graph entity type for this kind.
func (k KnowledgeKind) EntityType() EntityType { return EntityType(k) }

// RefPrefix is the human reference prefix (D3, A1, E2, I4, Q1, T2).
func (k KnowledgeKind) RefPrefix() string {
	switch k {
	case KindDecision:
		return "D"
	case KindAssumption:
		return "A"
	case KindEvidence:
		return "E"
	case KindInsight:
		return "I"
	case KindQuestion:
		return "Q"
	case KindAction:
		return "T"
	}
	return "K"
}

// Label renders a human reference such as "D3".
func (k KnowledgeKind) Label(ref int) string { return k.RefPrefix() + strconv.Itoa(ref) }

// ParseRefLabel parses "D3" / "cp4"-style labels for knowledge refs. Returns kind and number.
func ParseRefLabel(label string) (KnowledgeKind, int, bool) {
	label = strings.ToUpper(strings.TrimSpace(label))
	if len(label) < 2 {
		return "", 0, false
	}
	n, err := strconv.Atoi(label[1:])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	for _, k := range AllKnowledgeKinds {
		if k.RefPrefix() == label[:1] {
			return k, n, true
		}
	}
	return "", 0, false
}

// DefaultStatus is the initial status for a newly recorded item of this kind.
func (k KnowledgeKind) DefaultStatus() string {
	switch k {
	case KindDecision:
		return "ACTIVE"
	case KindAssumption:
		return "UNVALIDATED"
	case KindQuestion:
		return "OPEN"
	case KindAction:
		return "TODO"
	default:
		return "ACTIVE"
	}
}

// Statuses lists the valid statuses for this kind.
func (k KnowledgeKind) Statuses() []string {
	switch k {
	case KindDecision:
		return []string{"PROPOSED", "ACTIVE", "SUPERSEDED", "REVERSED", "REJECTED"}
	case KindAssumption:
		return []string{"UNVALIDATED", "VALIDATING", "VALIDATED", "INVALIDATED", "SUPERSEDED"}
	case KindEvidence:
		return []string{"ACTIVE", "DISPUTED", "RETRACTED", "SUPERSEDED"}
	case KindInsight:
		return []string{"ACTIVE", "SUPERSEDED", "RETRACTED"}
	case KindQuestion:
		return []string{"OPEN", "ANSWERED", "DROPPED", "SUPERSEDED"}
	case KindAction:
		return []string{"TODO", "IN_PROGRESS", "DONE", "DROPPED", "SUPERSEDED"}
	}
	return nil
}

// ValidStatus reports whether status is valid for this kind.
func (k KnowledgeKind) ValidStatus(status string) bool {
	for _, s := range k.Statuses() {
		if s == status {
			return true
		}
	}
	return false
}

// IsLiveStatus reports whether an item in this status still represents current thinking.
func IsLiveStatus(status string) bool {
	switch status {
	case "SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED":
		return false
	}
	return true
}

// ReviewState is whether an item is part of durable knowledge.
type ReviewState string

const (
	// ReviewProposed items are suggestions (e.g. extracted from an import) awaiting review.
	ReviewProposed ReviewState = "PROPOSED"
	ReviewAccepted ReviewState = "ACCEPTED"
	ReviewRejected ReviewState = "REJECTED"
)

// Alternative is a considered-but-not-chosen option for a decision.
type Alternative struct {
	Option         string `json:"option"`
	ReasonRejected string `json:"reason_rejected,omitempty"`
}

// KnowledgeItem is a decision, assumption, evidence, insight, question or action.
// Content (statement/details/origin/excerpt) is immutable after creation; beliefs
// change by superseding, never by rewriting.
type KnowledgeItem struct {
	ID                   uuid.UUID     `json:"id"`
	UserID               uuid.UUID     `json:"-"`
	IdeaID               uuid.UUID     `json:"idea_id"`
	BranchID             uuid.UUID     `json:"branch_id"`
	Kind                 KnowledgeKind `json:"kind"`
	RefNumber            int           `json:"ref_number"`
	Label                string        `json:"label"`
	LineageID            uuid.UUID     `json:"lineage_id"`
	Statement            string        `json:"statement"`
	Details              string        `json:"details"`
	Status               string        `json:"status"`
	Origin               Origin        `json:"origin"`
	ReviewState          ReviewState   `json:"review_state"`
	Confidence           *float32      `json:"confidence,omitempty"`
	SourceExcerpt        string        `json:"source_excerpt"`
	SourceMessageID      *uuid.UUID    `json:"source_message_id,omitempty"`
	SourceConversationID *uuid.UUID    `json:"source_conversation_id,omitempty"`
	SourceID             *uuid.UUID    `json:"source_id,omitempty"`
	CreatedBy            Actor         `json:"created_by"`
	AgentRunID           *uuid.UUID    `json:"agent_run_id,omitempty"`
	InheritedFromID      *uuid.UUID    `json:"inherited_from_id,omitempty"`
	SupersededByID       *uuid.UUID    `json:"superseded_by_id,omitempty"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`

	// Type-specific attributes (exactly one is set, matching Kind).
	Decision   *DecisionAttrs   `json:"decision,omitempty"`
	Assumption *AssumptionAttrs `json:"assumption,omitempty"`
	Evidence   *EvidenceAttrs   `json:"evidence,omitempty"`
	Insight    *InsightAttrs    `json:"insight,omitempty"`
	Question   *QuestionAttrs   `json:"question,omitempty"`
	Action     *ActionAttrs     `json:"action,omitempty"`
}

// DecisionAttrs are decision-specific fields.
type DecisionAttrs struct {
	Rationale    string        `json:"rationale"`
	Alternatives []Alternative `json:"alternatives"`
	DecidedAt    time.Time     `json:"decided_at"`
	ReversesID   *uuid.UUID    `json:"reverses_id,omitempty"`
}

// AssumptionAttrs are assumption-specific fields.
type AssumptionAttrs struct {
	Risk             string     `json:"risk"` // LOW | MEDIUM | HIGH
	ValidationMethod string     `json:"validation_method"`
	ValidatedAt      *time.Time `json:"validated_at,omitempty"`
}

// EvidenceAttrs are evidence-specific fields.
type EvidenceAttrs struct {
	Stance       string     `json:"stance"` // SUPPORTS | CHALLENGES | NEUTRAL
	URL          string     `json:"url"`
	Strength     string     `json:"strength"` // WEAK | MODERATE | STRONG
	TargetItemID *uuid.UUID `json:"target_item_id,omitempty"`
}

// InsightAttrs are insight-specific fields.
type InsightAttrs struct {
	Importance string `json:"importance"` // LOW | MEDIUM | HIGH
}

// QuestionAttrs are open-question-specific fields.
type QuestionAttrs struct {
	Answer           string     `json:"answer"`
	AnsweredAt       *time.Time `json:"answered_at,omitempty"`
	AnsweredByItemID *uuid.UUID `json:"answered_by_item_id,omitempty"`
}

// ActionAttrs are action-item-specific fields.
type ActionAttrs struct {
	Priority    string     `json:"priority"` // LOW | MEDIUM | HIGH
	DueAt       *time.Time `json:"due_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Ref returns an EntityRef for the item.
func (k *KnowledgeItem) Ref() EntityRef {
	return EntityRef{Type: k.Kind.EntityType(), ID: k.ID, Label: k.Label, Title: k.Statement}
}

// IsLive reports whether the item is accepted and still current.
func (k *KnowledgeItem) IsLive() bool {
	return k.ReviewState == ReviewAccepted && IsLiveStatus(k.Status)
}

// EnsureAttrs fills a default attribute struct for the item's kind if missing.
func (k *KnowledgeItem) EnsureAttrs() {
	switch k.Kind {
	case KindDecision:
		if k.Decision == nil {
			k.Decision = &DecisionAttrs{Alternatives: []Alternative{}}
		}
		if k.Decision.Alternatives == nil {
			k.Decision.Alternatives = []Alternative{}
		}
	case KindAssumption:
		if k.Assumption == nil {
			k.Assumption = &AssumptionAttrs{Risk: "MEDIUM"}
		}
	case KindEvidence:
		if k.Evidence == nil {
			k.Evidence = &EvidenceAttrs{Stance: "SUPPORTS", Strength: "MODERATE"}
		}
	case KindInsight:
		if k.Insight == nil {
			k.Insight = &InsightAttrs{Importance: "MEDIUM"}
		}
	case KindQuestion:
		if k.Question == nil {
			k.Question = &QuestionAttrs{}
		}
	case KindAction:
		if k.Action == nil {
			k.Action = &ActionAttrs{Priority: "MEDIUM"}
		}
	}
}

// ValidateLevel checks LOW/MEDIUM/HIGH style enums.
func ValidateLevel(field, v string, allowed ...string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return Invalid(field, fmt.Sprintf("must be one of %s", strings.Join(allowed, ", ")))
}

// RelType is a typed graph edge.
type RelType string

const (
	RelSimilarTo        RelType = "similar_to"
	RelEvolvedFrom      RelType = "evolved_from"
	RelInspiredBy       RelType = "inspired_by"
	RelContradicts      RelType = "contradicts"
	RelDependsOn        RelType = "depends_on"
	RelRelatedTo        RelType = "related_to"
	RelSolves           RelType = "solves"
	RelReusesLessonFrom RelType = "reuses_lesson_from"
	RelSupersedes       RelType = "supersedes"
	RelDerivedFrom      RelType = "derived_from"
	RelInheritedFrom    RelType = "inherited_from"
	RelSupports         RelType = "supports"
	RelChallenges       RelType = "challenges"
	RelAnswers          RelType = "answers"
	RelGeneratedFrom    RelType = "generated_from"
	RelMergedInto       RelType = "merged_into"
)

// AllRelTypes lists every relationship type.
var AllRelTypes = []RelType{
	RelSimilarTo, RelEvolvedFrom, RelInspiredBy, RelContradicts, RelDependsOn, RelRelatedTo,
	RelSolves, RelReusesLessonFrom, RelSupersedes, RelDerivedFrom, RelInheritedFrom,
	RelSupports, RelChallenges, RelAnswers, RelGeneratedFrom, RelMergedInto,
}

// Valid reports whether r is a known relationship type.
func (r RelType) Valid() bool {
	for _, x := range AllRelTypes {
		if x == r {
			return true
		}
	}
	return false
}

// Relationship is a typed, provenance-carrying edge between two graph entities.
type Relationship struct {
	ID              uuid.UUID  `json:"id"`
	FromType        EntityType `json:"from_type"`
	FromID          uuid.UUID  `json:"from_id"`
	ToType          EntityType `json:"to_type"`
	ToID            uuid.UUID  `json:"to_id"`
	RelType         RelType    `json:"rel_type"`
	IdeaID          *uuid.UUID `json:"idea_id,omitempty"`
	BranchID        *uuid.UUID `json:"branch_id,omitempty"`
	Origin          Origin     `json:"origin"`
	Confidence      *float32   `json:"confidence,omitempty"`
	Rationale       string     `json:"rationale"`
	SourceMessageID *uuid.UUID `json:"source_message_id,omitempty"`
	CreatedBy       Actor      `json:"created_by"`
	AgentRunID      *uuid.UUID `json:"agent_run_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	FromLabel       string     `json:"from_label,omitempty"`
	ToLabel         string     `json:"to_label,omitempty"`
}
