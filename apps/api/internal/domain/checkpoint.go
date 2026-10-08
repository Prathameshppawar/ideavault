package domain

import (
	"time"

	"github.com/google/uuid"
)

// CheckpointKind records why a checkpoint was taken.
type CheckpointKind string

const (
	CheckpointManual     CheckpointKind = "manual"
	CheckpointAuto       CheckpointKind = "auto"
	CheckpointConclusion CheckpointKind = "conclusion"
	CheckpointForkBase   CheckpointKind = "fork_base"
	CheckpointMerge      CheckpointKind = "merge"
)

// Checkpoint is an immutable snapshot of a branch's thinking state.
type Checkpoint struct {
	ID                 uuid.UUID      `json:"id"`
	UserID             uuid.UUID      `json:"-"`
	IdeaID             uuid.UUID      `json:"idea_id"`
	BranchID           uuid.UUID      `json:"branch_id"`
	Number             int            `json:"number"`
	Label              string         `json:"label"` // "CP4"
	Title              string         `json:"title"`
	Summary            string         `json:"summary"`
	Kind               CheckpointKind `json:"kind"`
	ParentCheckpointID *uuid.UUID     `json:"parent_checkpoint_id,omitempty"`
	Snapshot           *Snapshot      `json:"snapshot,omitempty"`
	ContentHash        string         `json:"content_hash"`
	CreatedBy          Actor          `json:"created_by"`
	AgentRunID         *uuid.UUID     `json:"agent_run_id,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	BranchName         string         `json:"branch_name,omitempty"`
	Counts             map[string]int `json:"counts,omitempty"`
}

// Snapshot is the complete, self-contained knowledge state captured by a checkpoint.
// It embeds full copies of items so history stays recoverable even if live rows change.
type Snapshot struct {
	SchemaVersion  int                     `json:"schema_version"`
	CapturedAt     time.Time               `json:"captured_at"`
	Idea           SnapshotIdea            `json:"idea"`
	Branch         SnapshotBranch          `json:"branch"`
	Items          []KnowledgeItem         `json:"items"`
	Relationships  []Relationship          `json:"relationships"`
	Conversations  []SnapshotRef           `json:"conversations"`
	Sources        []SnapshotRef           `json:"sources"`
	Artifacts      []SnapshotRef           `json:"artifacts"`
	Context        string                  `json:"context"` // important free-form context (e.g. checkpoint note)
	Contradictions []SnapshotContradiction `json:"contradictions,omitempty"`
}

// SnapshotIdea is the idea's identity at snapshot time.
type SnapshotIdea struct {
	ID         uuid.UUID  `json:"id"`
	Title      string     `json:"title"`
	Summary    string     `json:"summary"`
	OriginText string     `json:"origin_text"`
	Status     IdeaStatus `json:"status"`
	Version    int        `json:"version"`
	Tags       []string   `json:"tags"`
}

// SnapshotBranch is the branch identity at snapshot time.
type SnapshotBranch struct {
	ID                     uuid.UUID  `json:"id"`
	Name                   string     `json:"name"`
	ParentBranchID         *uuid.UUID `json:"parent_branch_id,omitempty"`
	ForkedFromCheckpointID *uuid.UUID `json:"forked_from_checkpoint_id,omitempty"`
}

// SnapshotRef is a lightweight reference to a non-copied entity.
type SnapshotRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Kind  string    `json:"kind,omitempty"`
	At    time.Time `json:"at"`
}

// SnapshotContradiction records a known contradiction at snapshot time.
type SnapshotContradiction struct {
	FromID    uuid.UUID `json:"from_id"`
	ToID      uuid.UUID `json:"to_id"`
	FromLabel string    `json:"from_label"`
	ToLabel   string    `json:"to_label"`
	Rationale string    `json:"rationale"`
}

// ItemsOfKind returns snapshot items of the given kind.
func (s *Snapshot) ItemsOfKind(k KnowledgeKind) []KnowledgeItem {
	var out []KnowledgeItem
	for _, it := range s.Items {
		if it.Kind == k {
			out = append(out, it)
		}
	}
	return out
}

// CountByKind returns counts of items per kind.
func (s *Snapshot) CountByKind() map[string]int {
	m := map[string]int{}
	for _, it := range s.Items {
		m[string(it.Kind)]++
	}
	return m
}

// CheckpointDiff describes how thinking changed between two checkpoints.
type CheckpointDiff struct {
	From       EntityRef       `json:"from"`
	To         EntityRef       `json:"to"`
	Added      []KnowledgeItem `json:"added"`
	Removed    []KnowledgeItem `json:"removed"`
	Changed    []ItemChange    `json:"changed"`
	Superseded []Supersession  `json:"superseded"`
	Unchanged  int             `json:"unchanged"`
}

// ItemChange is a status change of the same lineage between snapshots.
type ItemChange struct {
	Before KnowledgeItem `json:"before"`
	After  KnowledgeItem `json:"after"`
	Fields []string      `json:"fields"`
}

// Supersession pairs an old item with the item that replaced it.
type Supersession struct {
	Old KnowledgeItem `json:"old"`
	New KnowledgeItem `json:"new"`
}
