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

// RecordKnowledgeInput records a decision/assumption/evidence/insight/question/action.
type RecordKnowledgeInput struct {
	IdeaID     uuid.UUID            `json:"idea_id"`
	BranchID   *uuid.UUID           `json:"branch_id,omitempty"`
	Kind       domain.KnowledgeKind `json:"kind"`
	Statement  string               `json:"statement"`
	Details    string               `json:"details,omitempty"`
	Status     string               `json:"status,omitempty"`
	Origin     domain.Origin        `json:"origin,omitempty"`
	Proposed   bool                 `json:"proposed,omitempty"` // store as a PROPOSED suggestion awaiting review
	Confidence *float32             `json:"confidence,omitempty"`

	SourceExcerpt        string     `json:"source_excerpt,omitempty"`
	SourceMessageID      *uuid.UUID `json:"source_message_id,omitempty"`
	SourceConversationID *uuid.UUID `json:"source_conversation_id,omitempty"`
	SourceID             *uuid.UUID `json:"source_id,omitempty"`

	// Supersedes replaces an existing item (same kind) without rewriting it.
	Supersedes *uuid.UUID `json:"supersedes,omitempty"`
	// Reverses marks a decision that reverses a previous decision (status REVERSED instead of SUPERSEDED).
	Reverses bool `json:"reverses,omitempty"`

	// Type-specific
	Rationale        string               `json:"rationale,omitempty"`
	Alternatives     []domain.Alternative `json:"alternatives,omitempty"`
	Risk             string               `json:"risk,omitempty"`
	ValidationMethod string               `json:"validation_method,omitempty"`
	Stance           string               `json:"stance,omitempty"`
	URL              string               `json:"url,omitempty"`
	Strength         string               `json:"strength,omitempty"`
	TargetItemID     *uuid.UUID           `json:"target_item_id,omitempty"`
	Importance       string               `json:"importance,omitempty"`
	Priority         string               `json:"priority,omitempty"`
	DueAt            *time.Time           `json:"due_at,omitempty"`

	// Related links this item to others with related_to/derived_from/depends_on edges.
	Related []RelatedLink `json:"related,omitempty"`
}

// RelatedLink is an edge to create alongside a new item.
type RelatedLink struct {
	ItemID  uuid.UUID      `json:"item_id"`
	RelType domain.RelType `json:"rel_type"`
}

// RecordKnowledge validates and records a knowledge item, handling supersession and evidence links.
func (s *Service) RecordKnowledge(ctx context.Context, a Actor, in RecordKnowledgeInput) (*domain.KnowledgeItem, error) {
	kind, ok := domain.ParseKnowledgeKind(string(in.Kind))
	if !ok {
		return nil, domain.Invalid("kind", "kind must be decision, assumption, evidence, insight, question or action")
	}
	stmt := strings.TrimSpace(in.Statement)
	if stmt == "" {
		return nil, domain.Invalid("statement", "statement is required")
	}
	if len([]rune(stmt)) > 4000 {
		return nil, domain.Invalid("statement", "statement is too long (max 4000 characters)")
	}
	origin := in.Origin
	if origin == "" {
		origin = domain.OriginSource
		if a.Kind == domain.ActorAgent && in.SourceExcerpt == "" {
			origin = domain.OriginInterpretation
		}
	}
	if !origin.Valid() {
		return nil, domain.Invalid("origin", "origin must be SOURCE or INTERPRETATION")
	}
	if in.Confidence != nil && (*in.Confidence < 0 || *in.Confidence > 1) {
		return nil, domain.Invalid("confidence", "confidence must be between 0 and 1")
	}
	item := &domain.KnowledgeItem{UserID: a.UserID, IdeaID: in.IdeaID, Kind: kind, Statement: stmt, Details: strings.TrimSpace(in.Details),
		Origin: origin, Confidence: in.Confidence, SourceExcerpt: trimTo(in.SourceExcerpt, 2000), SourceMessageID: in.SourceMessageID,
		SourceConversationID: in.SourceConversationID, SourceID: in.SourceID, CreatedBy: a.Kind, AgentRunID: a.AgentRunID}
	item.ReviewState = domain.ReviewAccepted
	if in.Proposed {
		item.ReviewState = domain.ReviewProposed
	}
	item.Status = kind.DefaultStatus()
	if in.Status != "" {
		st := strings.ToUpper(in.Status)
		if !kind.ValidStatus(st) {
			return nil, domain.Invalid("status", fmt.Sprintf("invalid status for %s; allowed: %s", kind, strings.Join(kind.Statuses(), ", ")))
		}
		item.Status = st
	}
	if err := fillAttrs(item, in); err != nil {
		return nil, err
	}

	err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		idea, err := tx.GetIdea(ctx, a.UserID, in.IdeaID)
		if err != nil {
			return err
		}
		br, err := s.resolveBranchTx(ctx, tx, a.UserID, idea, in.BranchID)
		if err != nil {
			return err
		}
		item.BranchID = br.ID
		if in.SourceMessageID != nil && in.SourceConversationID == nil {
			if m, err := tx.GetMessage(ctx, a.UserID, *in.SourceMessageID); err == nil {
				item.SourceConversationID = &m.ConversationID
				if item.SourceExcerpt == "" {
					item.SourceExcerpt = trimTo(m.Content, 600)
				}
			} else {
				return domain.Invalid("source_message_id", "message not found")
			}
		}
		var old *domain.KnowledgeItem
		if in.Supersedes != nil {
			old, err = tx.GetKnowledge(ctx, a.UserID, *in.Supersedes)
			if err != nil {
				return err
			}
			if old.IdeaID != idea.ID {
				return domain.Invalid("supersedes", "superseded item belongs to another idea")
			}
			if old.Kind != kind {
				return domain.Invalid("supersedes", fmt.Sprintf("a %s can only supersede another %s", kind, kind))
			}
			if old.SupersededByID != nil {
				return domain.Conflict(fmt.Sprintf("%s was already superseded; supersede its replacement instead", old.Label))
			}
			if kind == domain.KindDecision && in.Reverses {
				item.Decision.ReversesID = &old.ID
			}
		}
		if item.Kind == domain.KindEvidence && item.Evidence.TargetItemID != nil {
			if _, err := tx.GetKnowledge(ctx, a.UserID, *item.Evidence.TargetItemID); err != nil {
				return domain.Invalid("target_item_id", "evidence target not found")
			}
		}
		if err := tx.InsertKnowledge(ctx, item); err != nil {
			return err
		}
		if old != nil {
			st := "SUPERSEDED"
			if kind == domain.KindDecision && in.Reverses {
				st = "REVERSED"
			}
			if err := tx.UpdateKnowledgeState(ctx, a.UserID, old.ID, &st, nil, &item.ID); err != nil {
				return err
			}
			if err := tx.CreateRelationship(ctx, a.UserID, &domain.Relationship{FromType: kind.EntityType(), FromID: item.ID, ToType: old.Kind.EntityType(), ToID: old.ID,
				RelType: domain.RelSupersedes, IdeaID: &idea.ID, BranchID: &br.ID, Origin: origin, Rationale: trimTo(in.Rationale, 1000), CreatedBy: a.Kind, AgentRunID: a.AgentRunID}); err != nil {
				return err
			}
			verb := "superseded"
			if st == "REVERSED" {
				verb = "reversed"
			}
			s.activity(ctx, tx, a, &idea.ID, &br.ID, string(kind)+"."+verb, kind.EntityType(), &item.ID,
				fmt.Sprintf("%s %s %s: %s", item.Label, verb, old.Label, trimTo(stmt, 140)), map[string]any{"old_id": old.ID.String()})
		}
		if item.Kind == domain.KindEvidence && item.Evidence.TargetItemID != nil {
			rel := domain.RelSupports
			if item.Evidence.Stance == "CHALLENGES" {
				rel = domain.RelChallenges
			}
			target, _ := tx.GetKnowledge(ctx, a.UserID, *item.Evidence.TargetItemID)
			if err := tx.CreateRelationship(ctx, a.UserID, &domain.Relationship{FromType: domain.EntityEvidence, FromID: item.ID, ToType: target.Kind.EntityType(), ToID: target.ID,
				RelType: rel, IdeaID: &idea.ID, BranchID: &br.ID, Origin: origin, CreatedBy: a.Kind, AgentRunID: a.AgentRunID}); err != nil {
				return err
			}
		}
		for _, rl := range in.Related {
			target, err := tx.GetKnowledge(ctx, a.UserID, rl.ItemID)
			if err != nil {
				return domain.Invalid("related", "related item not found")
			}
			rt := rl.RelType
			if rt == "" {
				rt = domain.RelRelatedTo
			}
			if !rt.Valid() {
				return domain.Invalid("related", "unknown relationship type")
			}
			if err := tx.CreateRelationship(ctx, a.UserID, &domain.Relationship{FromType: kind.EntityType(), FromID: item.ID, ToType: target.Kind.EntityType(), ToID: target.ID,
				RelType: rt, IdeaID: &idea.ID, BranchID: &br.ID, Origin: origin, CreatedBy: a.Kind, AgentRunID: a.AgentRunID}); err != nil {
				return err
			}
		}
		if old == nil {
			ev := string(kind) + ".recorded"
			if item.ReviewState == domain.ReviewProposed {
				ev = string(kind) + ".proposed"
			}
			s.activity(ctx, tx, a, &idea.ID, &br.ID, ev, kind.EntityType(), &item.ID, fmt.Sprintf("%s %s", item.Label, trimTo(stmt, 160)), nil)
		}
		if idea.Status == domain.StatusExploring && kind == domain.KindDecision && item.ReviewState == domain.ReviewAccepted {
			// First accepted decision moves an exploring idea to ACTIVE thinking.
			idea.Status = domain.StatusActive
			idea.Version++
			if err := tx.UpdateIdea(ctx, idea); err != nil {
				return err
			}
			if err := tx.InsertIdeaVersion(ctx, &domain.IdeaVersion{IdeaID: idea.ID, Version: idea.Version, Title: idea.Title, Summary: idea.Summary,
				Status: idea.Status, Tags: idea.Tags, ChangedFields: []string{"status"}, ChangeReason: "First decision recorded", CreatedBy: domain.ActorSystem}, a.AgentRunID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.enqueueEmbedding(ctx, a.UserID, kind.EntityType(), item.ID)
	return s.store.GetKnowledge(ctx, a.UserID, item.ID)
}

func (s *Service) resolveBranchTx(ctx context.Context, tx *postgres.Store, userID uuid.UUID, idea *domain.Idea, branchID *uuid.UUID) (*domain.Branch, error) {
	if branchID != nil && *branchID != uuid.Nil {
		b, err := tx.GetBranch(ctx, userID, *branchID)
		if err != nil {
			return nil, err
		}
		if b.IdeaID != idea.ID {
			return nil, domain.Invalid("branch_id", "branch does not belong to this idea")
		}
		return b, nil
	}
	if idea.DefaultBranchID == nil {
		return nil, domain.Conflict("idea has no default branch")
	}
	return tx.GetBranch(ctx, userID, *idea.DefaultBranchID)
}

func fillAttrs(item *domain.KnowledgeItem, in RecordKnowledgeInput) error {
	item.EnsureAttrs()
	switch item.Kind {
	case domain.KindDecision:
		item.Decision.Rationale = strings.TrimSpace(in.Rationale)
		alts := []domain.Alternative{}
		for _, alt := range in.Alternatives {
			if strings.TrimSpace(alt.Option) != "" {
				alts = append(alts, domain.Alternative{Option: strings.TrimSpace(alt.Option), ReasonRejected: strings.TrimSpace(alt.ReasonRejected)})
			}
		}
		item.Decision.Alternatives = alts
	case domain.KindAssumption:
		if in.Risk != "" {
			item.Assumption.Risk = strings.ToUpper(in.Risk)
		}
		if err := domain.ValidateLevel("risk", item.Assumption.Risk, "LOW", "MEDIUM", "HIGH"); err != nil {
			return err
		}
		item.Assumption.ValidationMethod = strings.TrimSpace(in.ValidationMethod)
	case domain.KindEvidence:
		if in.Stance != "" {
			item.Evidence.Stance = strings.ToUpper(in.Stance)
		}
		if err := domain.ValidateLevel("stance", item.Evidence.Stance, "SUPPORTS", "CHALLENGES", "NEUTRAL"); err != nil {
			return err
		}
		if in.Strength != "" {
			item.Evidence.Strength = strings.ToUpper(in.Strength)
		}
		if err := domain.ValidateLevel("strength", item.Evidence.Strength, "WEAK", "MODERATE", "STRONG"); err != nil {
			return err
		}
		u := strings.TrimSpace(in.URL)
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return domain.Invalid("url", "evidence URL must be http(s)")
		}
		item.Evidence.URL = u
		item.Evidence.TargetItemID = in.TargetItemID
	case domain.KindInsight:
		if in.Importance != "" {
			item.Insight.Importance = strings.ToUpper(in.Importance)
		}
		if err := domain.ValidateLevel("importance", item.Insight.Importance, "LOW", "MEDIUM", "HIGH"); err != nil {
			return err
		}
	case domain.KindAction:
		if in.Priority != "" {
			item.Action.Priority = strings.ToUpper(in.Priority)
		}
		if err := domain.ValidateLevel("priority", item.Action.Priority, "LOW", "MEDIUM", "HIGH"); err != nil {
			return err
		}
		item.Action.DueAt = in.DueAt
	}
	return nil
}

// UpdateKnowledgeStatusInput changes an item's status (never its content).
type UpdateKnowledgeStatusInput struct {
	Status string     `json:"status"`
	Answer string     `json:"answer,omitempty"` // for questions
	Note   string     `json:"note,omitempty"`
	ByItem *uuid.UUID `json:"by_item_id,omitempty"` // question answered by / evidence that validated
}

// UpdateKnowledgeStatus transitions an item's status with kind-specific bookkeeping.
func (s *Service) UpdateKnowledgeStatus(ctx context.Context, a Actor, id uuid.UUID, in UpdateKnowledgeStatusInput) (*domain.KnowledgeItem, error) {
	err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		item, err := tx.GetKnowledge(ctx, a.UserID, id)
		if err != nil {
			return err
		}
		st := strings.ToUpper(strings.TrimSpace(in.Status))
		if !item.Kind.ValidStatus(st) {
			return domain.Invalid("status", fmt.Sprintf("invalid status for %s; allowed: %s", item.Kind, strings.Join(item.Kind.Statuses(), ", ")))
		}
		if st == "SUPERSEDED" {
			return domain.Invalid("status", "record a new item with `supersedes` instead of setting SUPERSEDED directly")
		}
		if item.SupersededByID != nil {
			return domain.Conflict(item.Label + " is superseded; change its replacement instead")
		}
		if err := tx.UpdateKnowledgeState(ctx, a.UserID, id, &st, nil, nil); err != nil {
			return err
		}
		switch item.Kind {
		case domain.KindQuestion:
			if st == "ANSWERED" {
				if strings.TrimSpace(in.Answer) == "" && in.ByItem == nil {
					return domain.Invalid("answer", "provide the answer (or the item that answers it)")
				}
				if err := tx.UpdateQuestionAnswer(ctx, id, strings.TrimSpace(in.Answer), in.ByItem); err != nil {
					return err
				}
				if in.ByItem != nil {
					other, err := tx.GetKnowledge(ctx, a.UserID, *in.ByItem)
					if err != nil {
						return err
					}
					if err := tx.CreateRelationship(ctx, a.UserID, &domain.Relationship{FromType: other.Kind.EntityType(), FromID: other.ID, ToType: domain.EntityQuestion, ToID: id,
						RelType: domain.RelAnswers, IdeaID: &item.IdeaID, BranchID: &item.BranchID, Origin: domain.OriginSource, CreatedBy: a.Kind, AgentRunID: a.AgentRunID}); err != nil {
						return err
					}
				}
			}
		case domain.KindAssumption:
			if err := tx.UpdateAssumptionValidation(ctx, id, st == "VALIDATED"); err != nil {
				return err
			}
		case domain.KindAction:
			if err := tx.UpdateActionCompletion(ctx, id, st == "DONE"); err != nil {
				return err
			}
		}
		summary := fmt.Sprintf("%s → %s", item.Label, st)
		if in.Note != "" {
			summary += ": " + trimTo(in.Note, 160)
		}
		s.activity(ctx, tx, a, &item.IdeaID, &item.BranchID, string(item.Kind)+".status_changed", item.Kind.EntityType(), &item.ID, summary,
			map[string]any{"from": item.Status, "to": st})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetKnowledge(ctx, a.UserID, id)
}

// ReviewKnowledge accepts or rejects PROPOSED items (memory policy: suggestions become durable only when accepted).
func (s *Service) ReviewKnowledge(ctx context.Context, a Actor, ids []uuid.UUID, accept bool) ([]domain.KnowledgeItem, error) {
	var out []domain.KnowledgeItem
	err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		for _, id := range ids {
			item, err := tx.GetKnowledge(ctx, a.UserID, id)
			if err != nil {
				return err
			}
			if item.ReviewState != domain.ReviewProposed {
				return domain.Conflict(item.Label + " is not awaiting review")
			}
			rs := domain.ReviewRejected
			ev := "rejected"
			if accept {
				rs = domain.ReviewAccepted
				ev = "accepted"
			}
			if err := tx.UpdateKnowledgeState(ctx, a.UserID, id, nil, &rs, nil); err != nil {
				return err
			}
			s.activity(ctx, tx, a, &item.IdeaID, &item.BranchID, string(item.Kind)+"."+ev, item.Kind.EntityType(), &item.ID,
				fmt.Sprintf("%s %s: %s", item.Label, ev, trimTo(item.Statement, 140)), nil)
			item.ReviewState = rs
			out = append(out, *item)
		}
		return nil
	})
	return out, err
}

// KnowledgeExplanation answers "why do we believe/decide this?" with grounded history.
type KnowledgeExplanation struct {
	Item          *domain.KnowledgeItem  `json:"item"`
	Chain         []domain.KnowledgeItem `json:"chain"`
	Relationships []domain.Relationship  `json:"relationships"`
	Supporting    []domain.KnowledgeItem `json:"supporting"`
	Challenging   []domain.KnowledgeItem `json:"challenging"`
	SourceMessage *domain.Message        `json:"source_message,omitempty"`
	SourceWindow  []domain.Message       `json:"source_window,omitempty"`
	Conversation  *domain.Conversation   `json:"conversation,omitempty"`
	Checkpoints   []domain.Checkpoint    `json:"checkpoints"`
	InheritedFrom *domain.KnowledgeItem  `json:"inherited_from,omitempty"`
	Source        *domain.Source         `json:"source,omitempty"`
}

// ExplainKnowledge gathers provenance and evidence for an item.
func (s *Service) ExplainKnowledge(ctx context.Context, userID, id uuid.UUID) (*KnowledgeExplanation, error) {
	item, err := s.store.GetKnowledge(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	ex := &KnowledgeExplanation{Item: item}
	if ex.Chain, err = s.store.SupersessionChain(ctx, userID, id); err != nil {
		return nil, err
	}
	if ex.Relationships, err = s.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: userID, EntityID: &id}); err != nil {
		return nil, err
	}
	var supportIDs, challengeIDs []uuid.UUID
	for _, r := range ex.Relationships {
		if r.ToID != id {
			continue
		}
		switch r.RelType {
		case domain.RelSupports:
			supportIDs = append(supportIDs, r.FromID)
		case domain.RelChallenges, domain.RelContradicts:
			challengeIDs = append(challengeIDs, r.FromID)
		}
	}
	if len(supportIDs) > 0 {
		ex.Supporting, _ = s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, IDs: supportIDs})
	}
	if len(challengeIDs) > 0 {
		ex.Challenging, _ = s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, IDs: challengeIDs})
	}
	if item.SourceMessageID != nil {
		if m, err := s.store.GetMessage(ctx, userID, *item.SourceMessageID); err == nil {
			ex.SourceMessage = m
			ex.SourceWindow, _ = s.store.MessageWindow(ctx, userID, m.ConversationID, m.Position, 1)
		}
	}
	if item.SourceConversationID != nil {
		ex.Conversation, _ = s.store.GetConversation(ctx, userID, *item.SourceConversationID)
	}
	if item.SourceID != nil {
		ex.Source, _ = s.store.GetSource(ctx, userID, *item.SourceID)
	}
	// Checkpoints containing any version in this lineage on any branch.
	if ex.Checkpoints, err = s.store.CheckpointsContaining(ctx, userID, id); err != nil {
		return nil, err
	}
	if item.InheritedFromID != nil {
		ex.InheritedFrom, _ = s.store.GetKnowledge(ctx, userID, *item.InheritedFromID)
	}
	return ex, nil
}

// CreateRelationshipInput creates a graph edge.
type CreateRelationshipInput struct {
	FromType        domain.EntityType `json:"from_type"`
	FromID          uuid.UUID         `json:"from_id"`
	ToType          domain.EntityType `json:"to_type"`
	ToID            uuid.UUID         `json:"to_id"`
	RelType         domain.RelType    `json:"rel_type"`
	Rationale       string            `json:"rationale,omitempty"`
	Origin          domain.Origin     `json:"origin,omitempty"`
	Confidence      *float32          `json:"confidence,omitempty"`
	SourceMessageID *uuid.UUID        `json:"source_message_id,omitempty"`
}

// CreateRelationship validates ownership of both endpoints and creates the edge.
func (s *Service) CreateRelationship(ctx context.Context, a Actor, in CreateRelationshipInput) (*domain.Relationship, error) {
	if !in.RelType.Valid() {
		return nil, domain.Invalid("rel_type", "unknown relationship type")
	}
	if !in.FromType.Valid() || !in.ToType.Valid() {
		return nil, domain.Invalid("entity_type", "unknown entity type")
	}
	if in.FromType == in.ToType && in.FromID == in.ToID {
		return nil, domain.Invalid("to_id", "an entity cannot relate to itself")
	}
	for _, e := range []struct {
		t  domain.EntityType
		id uuid.UUID
	}{{in.FromType, in.FromID}, {in.ToType, in.ToID}} {
		ok, err := s.store.EntityOwned(ctx, a.UserID, e.t, e.id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, domain.NotFound(string(e.t))
		}
	}
	origin := in.Origin
	if origin == "" {
		origin = domain.OriginSource
		if a.Kind == domain.ActorAgent {
			origin = domain.OriginInterpretation
		}
	}
	r := &domain.Relationship{FromType: in.FromType, FromID: in.FromID, ToType: in.ToType, ToID: in.ToID, RelType: in.RelType, Origin: origin,
		Confidence: in.Confidence, Rationale: trimTo(in.Rationale, 1000), SourceMessageID: in.SourceMessageID, CreatedBy: a.Kind, AgentRunID: a.AgentRunID}
	// Attach idea/branch scope when an endpoint is knowledge or an idea.
	for _, e := range []struct {
		t  domain.EntityType
		id uuid.UUID
	}{{in.FromType, in.FromID}, {in.ToType, in.ToID}} {
		if e.t.IsKnowledge() && r.IdeaID == nil {
			if k, err := s.store.GetKnowledge(ctx, a.UserID, e.id); err == nil {
				r.IdeaID, r.BranchID = &k.IdeaID, &k.BranchID
			}
		}
	}
	if r.IdeaID == nil && in.FromType == domain.EntityIdea && in.ToType != domain.EntityIdea {
		r.IdeaID = &in.FromID
	}
	if err := s.store.CreateRelationship(ctx, a.UserID, r); err != nil {
		return nil, err
	}
	s.activity(ctx, s.store, a, r.IdeaID, r.BranchID, "relationship.created", "", &r.ID, fmt.Sprintf("%s %s → %s", in.RelType, in.FromType, in.ToType), nil)
	return r, nil
}
