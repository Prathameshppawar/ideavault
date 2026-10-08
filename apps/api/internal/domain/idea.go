package domain

import (
	"time"

	"github.com/google/uuid"
)

// IdeaStatus is the thinking lifecycle state of an Idea Space.
type IdeaStatus string

const (
	StatusExploring        IdeaStatus = "EXPLORING"
	StatusActive           IdeaStatus = "ACTIVE"
	StatusDecided          IdeaStatus = "DECIDED"
	StatusReadyToImplement IdeaStatus = "READY_TO_IMPLEMENT"
	StatusConcluded        IdeaStatus = "CONCLUDED"
	StatusParked           IdeaStatus = "PARKED"
	StatusAbandoned        IdeaStatus = "ABANDONED"
	StatusMerged           IdeaStatus = "MERGED"
)

// AllIdeaStatuses lists lifecycle states in their natural order.
var AllIdeaStatuses = []IdeaStatus{
	StatusExploring, StatusActive, StatusDecided, StatusReadyToImplement,
	StatusConcluded, StatusParked, StatusAbandoned, StatusMerged,
}

// Valid reports whether s is a known status.
func (s IdeaStatus) Valid() bool {
	for _, x := range AllIdeaStatuses {
		if x == s {
			return true
		}
	}
	return false
}

// IsOpen reports whether the idea is still being actively thought about.
func (s IdeaStatus) IsOpen() bool {
	return s == StatusExploring || s == StatusActive || s == StatusDecided || s == StatusReadyToImplement
}

// ideaTransitions encodes the allowed lifecycle moves. Any non-merged state may be
// reopened to EXPLORING/ACTIVE, because thinking is allowed to resume.
var ideaTransitions = map[IdeaStatus][]IdeaStatus{
	StatusExploring:        {StatusActive, StatusDecided, StatusReadyToImplement, StatusConcluded, StatusParked, StatusAbandoned, StatusMerged},
	StatusActive:           {StatusExploring, StatusDecided, StatusReadyToImplement, StatusConcluded, StatusParked, StatusAbandoned, StatusMerged},
	StatusDecided:          {StatusExploring, StatusActive, StatusReadyToImplement, StatusConcluded, StatusParked, StatusAbandoned, StatusMerged},
	StatusReadyToImplement: {StatusExploring, StatusActive, StatusDecided, StatusConcluded, StatusParked, StatusAbandoned, StatusMerged},
	StatusConcluded:        {StatusExploring, StatusActive},
	StatusParked:           {StatusExploring, StatusActive, StatusAbandoned, StatusMerged},
	StatusAbandoned:        {StatusExploring, StatusActive},
	StatusMerged:           {},
}

// CanTransition reports whether an idea may move from one status to another.
func CanTransition(from, to IdeaStatus) bool {
	if from == to {
		return true
	}
	for _, x := range ideaTransitions[from] {
		if x == to {
			return true
		}
	}
	return false
}

// Outcome is what a concluded idea produced.
type Outcome string

const (
	OutcomeImplement        Outcome = "IMPLEMENT"
	OutcomeActionPlan       Outcome = "ACTION_PLAN"
	OutcomeResearchComplete Outcome = "RESEARCH_COMPLETE"
	OutcomeDecision         Outcome = "DECISION"
	OutcomeProposal         Outcome = "PROPOSAL"
	OutcomeReference        Outcome = "REFERENCE"
	OutcomePark             Outcome = "PARK"
	OutcomeAbandon          Outcome = "ABANDON"
	OutcomeMerge            Outcome = "MERGE"
	OutcomeOther            Outcome = "OTHER"
)

// AllOutcomes lists every conclusion outcome.
var AllOutcomes = []Outcome{
	OutcomeImplement, OutcomeActionPlan, OutcomeResearchComplete, OutcomeDecision, OutcomeProposal,
	OutcomeReference, OutcomePark, OutcomeAbandon, OutcomeMerge, OutcomeOther,
}

// Valid reports whether o is a known outcome.
func (o Outcome) Valid() bool {
	for _, x := range AllOutcomes {
		if x == o {
			return true
		}
	}
	return false
}

// StatusForOutcome maps a conclusion outcome to the resulting idea status.
func StatusForOutcome(o Outcome) IdeaStatus {
	switch o {
	case OutcomeImplement:
		return StatusReadyToImplement
	case OutcomeDecision:
		return StatusDecided
	case OutcomePark:
		return StatusParked
	case OutcomeAbandon:
		return StatusAbandoned
	case OutcomeMerge:
		return StatusMerged
	default:
		return StatusConcluded
	}
}

// Idea is the persistent identity of one idea (an Idea Space).
type Idea struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"-"`
	Title  string    `json:"title"`
	Slug   string    `json:"slug"`
	// OriginText is SOURCE: what the user actually said when the idea was born.
	OriginText string `json:"origin_text"`
	// Summary is INTERPRETATION: IdeaVault's current understanding.
	Summary              string     `json:"summary"`
	Status               IdeaStatus `json:"status"`
	Outcome              *Outcome   `json:"outcome,omitempty"`
	OutcomeNote          string     `json:"outcome_note"`
	Tags                 []string   `json:"tags"`
	DefaultBranchID      *uuid.UUID `json:"default_branch_id,omitempty"`
	OriginSourceID       *uuid.UUID `json:"origin_source_id,omitempty"`
	OriginConversationID *uuid.UUID `json:"origin_conversation_id,omitempty"`
	MergedIntoIdeaID     *uuid.UUID `json:"merged_into_idea_id,omitempty"`
	Version              int        `json:"version"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	LastActivityAt       time.Time  `json:"last_activity_at"`
	ConcludedAt          *time.Time `json:"concluded_at,omitempty"`
	Stats                *IdeaStats `json:"stats,omitempty"`
}

// IdeaStats are cheap aggregate counts for list/overview views.
type IdeaStats struct {
	Branches      int `json:"branches"`
	Checkpoints   int `json:"checkpoints"`
	Conversations int `json:"conversations"`
	Decisions     int `json:"decisions"`
	Assumptions   int `json:"assumptions"`
	Evidence      int `json:"evidence"`
	Insights      int `json:"insights"`
	OpenQuestions int `json:"open_questions"`
	OpenActions   int `json:"open_actions"`
	Artifacts     int `json:"artifacts"`
	Proposed      int `json:"proposed"`
	// Momentum is the count of graph changes in the last 7 days.
	Momentum int `json:"momentum"`
}

// IdeaVersion is an immutable record of an idea's identity fields at a point in time.
type IdeaVersion struct {
	ID            uuid.UUID  `json:"id"`
	IdeaID        uuid.UUID  `json:"idea_id"`
	Version       int        `json:"version"`
	Title         string     `json:"title"`
	Summary       string     `json:"summary"`
	Status        IdeaStatus `json:"status"`
	Outcome       *Outcome   `json:"outcome,omitempty"`
	Tags          []string   `json:"tags"`
	ChangedFields []string   `json:"changed_fields"`
	ChangeReason  string     `json:"change_reason"`
	CreatedBy     Actor      `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

// BranchStatus is the state of a direction of thinking.
type BranchStatus string

const (
	BranchActive    BranchStatus = "ACTIVE"
	BranchConcluded BranchStatus = "CONCLUDED"
	BranchArchived  BranchStatus = "ARCHIVED"
	BranchMerged    BranchStatus = "MERGED"
)

// Valid reports whether s is a known branch status.
func (s BranchStatus) Valid() bool {
	return s == BranchActive || s == BranchConcluded || s == BranchArchived || s == BranchMerged
}

// ForkMode records how a branch was created.
type ForkMode string

const (
	ForkCheckpoint ForkMode = "checkpoint"
	ForkSelective  ForkMode = "selective"
	ForkEmpty      ForkMode = "empty"
)

// Branch is a direction of thinking within an idea. Branches are never overwritten.
type Branch struct {
	ID                     uuid.UUID    `json:"id"`
	UserID                 uuid.UUID    `json:"-"`
	IdeaID                 uuid.UUID    `json:"idea_id"`
	Name                   string       `json:"name"`
	Slug                   string       `json:"slug"`
	Description            string       `json:"description"`
	Status                 BranchStatus `json:"status"`
	IsDefault              bool         `json:"is_default"`
	ParentBranchID         *uuid.UUID   `json:"parent_branch_id,omitempty"`
	ForkedFromCheckpointID *uuid.UUID   `json:"forked_from_checkpoint_id,omitempty"`
	ForkMode               *ForkMode    `json:"fork_mode,omitempty"`
	HeadCheckpointID       *uuid.UUID   `json:"head_checkpoint_id,omitempty"`
	CreatedAt              time.Time    `json:"created_at"`
	UpdatedAt              time.Time    `json:"updated_at"`
	// Populated on read for convenience.
	ForkedFromCheckpointNumber *int `json:"forked_from_checkpoint_number,omitempty"`
	CheckpointCount            int  `json:"checkpoint_count"`
	KnowledgeCount             int  `json:"knowledge_count"`
}

// InheritanceRecord states exactly what a fork/merge inherited and from where.
type InheritanceRecord struct {
	ID                 uuid.UUID  `json:"id"`
	BranchID           uuid.UUID  `json:"branch_id"`
	EntityType         EntityType `json:"entity_type"`
	SourceEntityID     uuid.UUID  `json:"source_entity_id"`
	NewEntityID        *uuid.UUID `json:"new_entity_id,omitempty"`
	Via                string     `json:"via"` // checkpoint | selection | merge
	SourceCheckpointID *uuid.UUID `json:"source_checkpoint_id,omitempty"`
	SourceBranchID     *uuid.UUID `json:"source_branch_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	Label              string     `json:"label,omitempty"`
	Title              string     `json:"title,omitempty"`
}

// ConclusionRecord captures an intentional conclusion of an idea.
type ConclusionRecord struct {
	ID             uuid.UUID  `json:"id"`
	IdeaID         uuid.UUID  `json:"idea_id"`
	BranchID       *uuid.UUID `json:"branch_id,omitempty"`
	CheckpointID   *uuid.UUID `json:"checkpoint_id,omitempty"`
	Outcome        Outcome    `json:"outcome"`
	Note           string     `json:"note"`
	Learned        string     `json:"learned"`
	Decisions      string     `json:"decisions"`
	Unresolved     string     `json:"unresolved"`
	PreviousStatus IdeaStatus `json:"previous_status"`
	NewStatus      IdeaStatus `json:"new_status"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ActivityEvent is an entry in the thinking activity log.
type ActivityEvent struct {
	ID         uuid.UUID      `json:"id"`
	IdeaID     *uuid.UUID     `json:"idea_id,omitempty"`
	BranchID   *uuid.UUID     `json:"branch_id,omitempty"`
	EventType  string         `json:"event_type"`
	EntityType EntityType     `json:"entity_type,omitempty"`
	EntityID   *uuid.UUID     `json:"entity_id,omitempty"`
	Summary    string         `json:"summary"`
	Actor      Actor          `json:"actor"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	IdeaTitle  string         `json:"idea_title,omitempty"`
}
