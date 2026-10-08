// Package agent implements IdeaVault's Agent Supervisor: it interprets requests,
// operates on the real thinking graph through permissioned tools, streams safe
// execution traces (never chain-of-thought) and produces grounded responses.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

// Focus is the idea/branch/checkpoint the conversation is currently about.
type Focus struct {
	IdeaID       *uuid.UUID `json:"idea_id,omitempty"`
	BranchID     *uuid.UUID `json:"branch_id,omitempty"`
	CheckpointID *uuid.UUID `json:"checkpoint_id,omitempty"`
}

// ToolContext is passed to every tool invocation.
type ToolContext struct {
	Svc            *service.Service
	Actor          service.Actor
	UserID         uuid.UUID
	RunID          uuid.UUID
	ConversationID uuid.UUID
	UserMessageID  *uuid.UUID
	UserMessage    string
	Focus          *Focus
	// Progress emits an intermediate, user-visible trace label.
	Progress func(label string)
}

// ToolResult is what a tool returns: data for the model, plus safe trace metadata.
type ToolResult struct {
	Summary   string             `json:"summary"`
	Data      any                `json:"data"`
	Refs      []domain.EntityRef `json:"refs,omitempty"`
	Untrusted bool               `json:"untrusted,omitempty"` // result contains imported/external content
}

// Tool is one agent capability.
type Tool struct {
	Name        string
	Description string
	Category    domain.ToolCategory
	Params      json.RawMessage
	Run         func(ctx context.Context, tc *ToolContext, args json.RawMessage) (*ToolResult, error)
}

// link renders the in-app link for an entity.
func link(t domain.EntityType, id uuid.UUID) string { return "iv://" + string(t) + "/" + id.String() }

// ---------- resolution helpers ----------

func (tc *ToolContext) resolveIdea(ctx context.Context, ref string) (*domain.Idea, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if tc.Focus != nil && tc.Focus.IdeaID != nil {
			return tc.Svc.GetIdea(ctx, tc.UserID, *tc.Focus.IdeaID)
		}
		return nil, domain.Invalid("idea", "no idea is in focus; specify which idea (title, slug or id)")
	}
	idea, err := tc.Svc.ResolveIdea(ctx, tc.UserID, ref)
	if err != nil {
		return nil, err
	}
	tc.setFocusIdea(idea)
	return idea, nil
}

func (tc *ToolContext) setFocusIdea(idea *domain.Idea) {
	if tc.Focus == nil {
		tc.Focus = &Focus{}
	}
	if tc.Focus.IdeaID == nil || *tc.Focus.IdeaID != idea.ID {
		id := idea.ID
		tc.Focus.IdeaID = &id
		tc.Focus.BranchID = idea.DefaultBranchID
		tc.Focus.CheckpointID = nil
	}
}

func (tc *ToolContext) resolveBranch(ctx context.Context, idea *domain.Idea, ref string) (*domain.Branch, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if tc.Focus != nil && tc.Focus.BranchID != nil && tc.Focus.IdeaID != nil && *tc.Focus.IdeaID == idea.ID {
			return tc.Svc.Store().GetBranch(ctx, tc.UserID, *tc.Focus.BranchID)
		}
		if idea.DefaultBranchID == nil {
			return nil, domain.Conflict("idea has no default branch")
		}
		return tc.Svc.Store().GetBranch(ctx, tc.UserID, *idea.DefaultBranchID)
	}
	b, err := tc.Svc.Store().FindBranch(ctx, tc.UserID, idea.ID, ref)
	if err != nil {
		return nil, domain.NotFound(fmt.Sprintf("branch %q in %s", ref, idea.Title))
	}
	return b, nil
}

func (tc *ToolContext) resolveCheckpoint(ctx context.Context, idea *domain.Idea, ref string) (*domain.Checkpoint, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, domain.Invalid("checkpoint", "checkpoint is required (e.g. CP4)")
	}
	return tc.Svc.ResolveCheckpoint(ctx, tc.UserID, idea.ID, ref)
}

// resolveItem accepts a knowledge label (D3) or uuid within the idea/branch.
func (tc *ToolContext) resolveItem(ctx context.Context, idea *domain.Idea, branchID *uuid.UUID, ref string) (*domain.KnowledgeItem, error) {
	ref = strings.TrimSpace(strings.Trim(ref, "[]"))
	if id, err := uuid.Parse(ref); err == nil {
		return tc.Svc.Store().GetKnowledge(ctx, tc.UserID, id)
	}
	if idea == nil {
		return nil, domain.Invalid("item", "item labels like D3 need an idea in focus")
	}
	k, n, ok := domain.ParseRefLabel(ref)
	if !ok {
		return nil, domain.Invalid("item", fmt.Sprintf("%q is not a knowledge label (e.g. D3, A1, Q2) or id", ref))
	}
	return tc.Svc.Store().FindKnowledgeByRef(ctx, tc.UserID, idea.ID, branchID, k, n)
}

func (tc *ToolContext) userMessageForSource() *uuid.UUID { return tc.UserMessageID }

// compactItem renders a knowledge item for model consumption.
func compactItem(it domain.KnowledgeItem) map[string]any {
	m := map[string]any{"label": it.Label, "id": it.ID, "kind": it.Kind, "statement": it.Statement, "status": it.Status, "origin": it.Origin,
		"link": link(it.Kind.EntityType(), it.ID), "created_at": it.CreatedAt.Format("2006-01-02")}
	if it.Details != "" {
		m["details"] = trim(it.Details, 400)
	}
	if it.ReviewState != domain.ReviewAccepted {
		m["review_state"] = it.ReviewState
	}
	if it.Decision != nil {
		if it.Decision.Rationale != "" {
			m["rationale"] = trim(it.Decision.Rationale, 500)
		}
		if len(it.Decision.Alternatives) > 0 {
			m["alternatives"] = it.Decision.Alternatives
		}
	}
	if it.Assumption != nil {
		m["risk"] = it.Assumption.Risk
	}
	if it.Evidence != nil {
		m["stance"] = it.Evidence.Stance
		if it.Evidence.URL != "" {
			m["url"] = it.Evidence.URL
		}
	}
	if it.Question != nil && it.Question.Answer != "" {
		m["answer"] = trim(it.Question.Answer, 300)
	}
	if it.SupersededByID != nil {
		m["superseded_by"] = it.SupersededByID
	}
	if it.SourceExcerpt != "" {
		m["source_excerpt"] = trim(it.SourceExcerpt, 240)
	}
	return m
}

func compactItems(items []domain.KnowledgeItem, max int) []map[string]any {
	out := []map[string]any{}
	for i, it := range items {
		if i >= max {
			break
		}
		out = append(out, compactItem(it))
	}
	return out
}

func refsOf(items []domain.KnowledgeItem, max int) []domain.EntityRef {
	var out []domain.EntityRef
	for i, it := range items {
		if i >= max {
			break
		}
		out = append(out, it.Ref())
	}
	return out
}

func ideaRef(i *domain.Idea) domain.EntityRef {
	return domain.EntityRef{Type: domain.EntityIdea, ID: i.ID, Title: i.Title}
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func decode(args json.RawMessage, v any) error {
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	if err := json.Unmarshal(args, v); err != nil {
		return domain.Invalid("arguments", "invalid tool arguments: "+err.Error())
	}
	return nil
}

func schema(s string) json.RawMessage { return json.RawMessage(s) }

func plural(n int, w string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, w)
	}
	return fmt.Sprintf("%d %ss", n, w)
}
