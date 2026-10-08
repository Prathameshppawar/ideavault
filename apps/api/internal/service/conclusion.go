package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// ConcludeInput intentionally closes an idea. Conclusion never deletes anything.
type ConcludeInput struct {
	IdeaID            uuid.UUID             `json:"idea_id"`
	BranchID          *uuid.UUID            `json:"branch_id,omitempty"`
	Outcome           domain.Outcome        `json:"outcome"`
	Note              string                `json:"note,omitempty"`
	MergeIntoIdeaID   *uuid.UUID            `json:"merge_into_idea_id,omitempty"`
	GenerateArtifacts []domain.ArtifactType `json:"generate_artifacts,omitempty"`
}

// ConclusionResult reports everything the conclusion produced.
type ConclusionResult struct {
	Conclusion *domain.ConclusionRecord `json:"conclusion"`
	Checkpoint *domain.Checkpoint       `json:"checkpoint"`
	Idea       *domain.Idea             `json:"idea"`
	Artifacts  []ArtifactResult         `json:"artifacts"`
}

// SuggestOutcome proposes an outcome from the idea's state (used when the user says "we're done").
func SuggestOutcome(items []domain.KnowledgeItem) (domain.Outcome, string) {
	var decisions, openQ, actions, evidence, insights int
	for _, it := range items {
		if !it.IsLive() {
			continue
		}
		switch it.Kind {
		case domain.KindDecision:
			decisions++
		case domain.KindQuestion:
			openQ++
		case domain.KindAction:
			if it.Status == "TODO" || it.Status == "IN_PROGRESS" {
				actions++
			}
		case domain.KindEvidence:
			evidence++
		case domain.KindInsight:
			insights++
		}
	}
	switch {
	case decisions >= 3 && actions >= 2:
		return domain.OutcomeActionPlan, "There are several decisions and open actions — an action plan turns them into execution."
	case decisions >= 3:
		return domain.OutcomeImplement, "Enough decisions are recorded to hand this to implementation."
	case evidence+insights >= 3 && decisions == 0:
		return domain.OutcomeResearchComplete, "Mostly evidence and insights — this reads as completed research."
	case decisions >= 1:
		return domain.OutcomeDecision, "The idea resolved into a decision."
	case decisions == 0 && openQ > 0:
		return domain.OutcomePark, "No decisions yet and questions remain — parking preserves it for later."
	}
	return domain.OutcomeReference, "Keep it as a reference for later."
}

// ConcludeIdea runs the full conclusion flow: final checkpoint, summaries, outcome, status, optional artifacts.
func (s *Service) ConcludeIdea(ctx context.Context, a Actor, in ConcludeInput) (*ConclusionResult, error) {
	if !in.Outcome.Valid() {
		return nil, domain.Invalid("outcome", "outcome must be one of IMPLEMENT, ACTION_PLAN, RESEARCH_COMPLETE, DECISION, PROPOSAL, REFERENCE, PARK, ABANDON, MERGE, OTHER")
	}
	idea, err := s.store.GetIdea(ctx, a.UserID, in.IdeaID)
	if err != nil {
		return nil, err
	}
	if idea.Status == domain.StatusMerged {
		return nil, domain.Conflict("idea is already merged into another idea")
	}
	br, err := s.resolveBranch(ctx, a.UserID, idea, in.BranchID)
	if err != nil {
		return nil, err
	}
	newStatus := domain.StatusForOutcome(in.Outcome)
	var target *domain.Idea
	if in.Outcome == domain.OutcomeMerge {
		if in.MergeIntoIdeaID == nil {
			return nil, domain.Invalid("merge_into_idea_id", "MERGE requires the idea to merge into")
		}
		if *in.MergeIntoIdeaID == idea.ID {
			return nil, domain.Invalid("merge_into_idea_id", "cannot merge an idea into itself")
		}
		if target, err = s.store.GetIdea(ctx, a.UserID, *in.MergeIntoIdeaID); err != nil {
			return nil, err
		}
	}
	if !domain.CanTransition(idea.Status, newStatus) {
		return nil, domain.Invalid("outcome", fmt.Sprintf("cannot conclude from %s to %s", idea.Status, newStatus))
	}
	items, err := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: a.UserID, BranchID: &br.ID, LiveOnly: true, Limit: 5000, OldestFirst: true})
	if err != nil {
		return nil, err
	}
	learned, decisions, unresolved := conclusionSummaries(items)
	note := strings.TrimSpace(in.Note)
	cp, err := s.CreateCheckpoint(ctx, a, CreateCheckpointInput{IdeaID: idea.ID, BranchID: &br.ID, Kind: domain.CheckpointConclusion,
		Title: "Conclusion: " + strings.ReplaceAll(strings.ToLower(string(in.Outcome)), "_", " "), Context: note})
	if err != nil {
		return nil, err
	}
	res := &ConclusionResult{Checkpoint: cp}
	prev := idea.Status
	err = s.store.WithTx(ctx, func(tx *postgres.Store) error {
		idea, err := tx.GetIdea(ctx, a.UserID, in.IdeaID)
		if err != nil {
			return err
		}
		rec := &domain.ConclusionRecord{IdeaID: idea.ID, BranchID: &br.ID, CheckpointID: &cp.ID, Outcome: in.Outcome, Note: note, Learned: learned,
			Decisions: decisions, Unresolved: unresolved, PreviousStatus: prev, NewStatus: newStatus}
		if err := tx.CreateConclusion(ctx, a.UserID, rec); err != nil {
			return err
		}
		res.Conclusion = rec
		o := in.Outcome
		idea.Outcome = &o
		idea.OutcomeNote = note
		idea.Status = newStatus
		now := time.Now()
		idea.ConcludedAt = &now
		if target != nil {
			idea.MergedIntoIdeaID = &target.ID
		}
		idea.Version++
		if err := tx.UpdateIdea(ctx, idea); err != nil {
			return err
		}
		if err := tx.InsertIdeaVersion(ctx, &domain.IdeaVersion{IdeaID: idea.ID, Version: idea.Version, Title: idea.Title, Summary: idea.Summary, Status: idea.Status,
			Outcome: idea.Outcome, Tags: idea.Tags, ChangedFields: []string{"status", "outcome"}, ChangeReason: "Concluded: " + string(in.Outcome), CreatedBy: a.Kind}, a.AgentRunID); err != nil {
			return err
		}
		br.Status = domain.BranchConcluded
		if target != nil {
			br.Status = domain.BranchMerged
		}
		if err := tx.UpdateBranch(ctx, br); err != nil {
			return err
		}
		if target != nil {
			if err := tx.CreateRelationship(ctx, a.UserID, &domain.Relationship{FromType: domain.EntityIdea, FromID: idea.ID, ToType: domain.EntityIdea, ToID: target.ID,
				RelType: domain.RelMergedInto, IdeaID: &idea.ID, Origin: domain.OriginSource, CreatedBy: a.Kind, AgentRunID: a.AgentRunID, Rationale: note}); err != nil {
				return err
			}
		}
		s.activity(ctx, tx, a, &idea.ID, &br.ID, "idea.concluded", domain.EntityIdea, &idea.ID,
			fmt.Sprintf("Concluded as %s (%s → %s)", in.Outcome, prev, newStatus), map[string]any{"outcome": string(in.Outcome)})
		res.Idea = idea
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, t := range in.GenerateArtifacts {
		ar, err := s.GenerateArtifact(ctx, a, GenerateArtifactInput{IdeaID: idea.ID, CheckpointID: &cp.ID, Type: t}, nil)
		if err != nil {
			s.log.WarnContext(ctx, "conclusion artifact failed", "type", t, "error", err)
			continue
		}
		res.Artifacts = append(res.Artifacts, *ar)
	}
	return res, nil
}

func conclusionSummaries(items []domain.KnowledgeItem) (learned, decisions, unresolved string) {
	var l, d, u []string
	for _, it := range items {
		switch it.Kind {
		case domain.KindInsight:
			l = append(l, fmt.Sprintf("- [%s] %s", it.Label, it.Statement))
		case domain.KindEvidence:
			if it.Evidence != nil && it.Evidence.Strength == "STRONG" {
				l = append(l, fmt.Sprintf("- [%s] %s", it.Label, it.Statement))
			}
		case domain.KindDecision:
			line := fmt.Sprintf("- [%s] %s", it.Label, it.Statement)
			if it.Decision != nil && it.Decision.Rationale != "" {
				line += " — " + it.Decision.Rationale
			}
			d = append(d, line)
		case domain.KindQuestion:
			if it.Status == "OPEN" {
				u = append(u, fmt.Sprintf("- [%s] %s", it.Label, it.Statement))
			}
		case domain.KindAssumption:
			if it.Status == "UNVALIDATED" || it.Status == "VALIDATING" {
				u = append(u, fmt.Sprintf("- [%s] Unvalidated: %s", it.Label, it.Statement))
			}
		}
	}
	join := func(xs []string, empty string) string {
		if len(xs) == 0 {
			return empty
		}
		return strings.Join(xs, "\n")
	}
	return join(l, "No insights recorded."), join(d, "No decisions recorded."), join(u, "Nothing left unresolved.")
}

// ReopenIdea moves a concluded/parked/abandoned idea back to active thinking.
func (s *Service) ReopenIdea(ctx context.Context, a Actor, id uuid.UUID, reason string) (*domain.Idea, error) {
	st := string(domain.StatusActive)
	if reason == "" {
		reason = "Reopened"
	}
	return s.UpdateIdea(ctx, a, id, UpdateIdeaInput{Status: &st, Reason: reason})
}
