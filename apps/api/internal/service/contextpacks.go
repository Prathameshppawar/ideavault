package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/contextengine"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// BuildContextPackInput builds a portable context pack.
type BuildContextPackInput struct {
	IdeaID       uuid.UUID  `json:"idea_id"`
	BranchID     *uuid.UUID `json:"branch_id,omitempty"`
	CheckpointID *uuid.UUID `json:"checkpoint_id,omitempty"`
	Objective    string     `json:"objective,omitempty"`
}

// BuildContextPack assembles the current thinking state for use in ChatGPT/Claude/Gemini.
func (s *Service) BuildContextPack(ctx context.Context, a Actor, in BuildContextPackInput) (*domain.ContextPack, error) {
	c, err := s.engine.Build(ctx, contextengine.Request{UserID: a.UserID, IdeaID: in.IdeaID, BranchID: in.BranchID, CheckpointID: in.CheckpointID,
		Query: in.Objective, TokenBudget: 30000, Full: true})
	if err != nil {
		return nil, err
	}
	objective := strings.TrimSpace(in.Objective)
	if objective == "" {
		objective = "Continue developing this idea from its current state."
	}
	md := renderContextPackMarkdown(c, objective)
	pack := &domain.ContextPack{UserID: a.UserID, IdeaID: in.IdeaID, Objective: objective, ContentMarkdown: md,
		ContentJSON: contextPackJSON(c, objective), TokenEstimate: models.EstimateTokens(md)}
	if c.Branch != nil {
		pack.BranchID = &c.Branch.ID
	}
	if c.Checkpoint != nil {
		pack.CheckpointID = &c.Checkpoint.ID
	}
	pack.Title = c.Idea.Title + " — context pack"
	if c.Checkpoint != nil {
		pack.Title += " (" + c.Checkpoint.Label + ")"
	} else if c.Branch != nil {
		pack.Title += " (" + c.Branch.Name + ")"
	}
	if err := s.store.CreateContextPack(ctx, pack); err != nil {
		return nil, err
	}
	s.activity(ctx, s.store, a, &in.IdeaID, pack.BranchID, "context_pack.created", domain.EntityContextPack, &pack.ID, pack.Title, nil)
	return pack, nil
}

func renderContextPackMarkdown(c *contextengine.Context, objective string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# IdeaVault Context Pack: %s\n\n", c.Idea.Title)
	b.WriteString("> This is a structured snapshot of my thinking, exported from IdeaVault. Use it as background. ")
	b.WriteString("Labels like [D3] identify recorded decisions/assumptions/evidence/insights/questions/actions so we can refer to them precisely. ")
	b.WriteString("When we're done, I'll bring this conversation back into IdeaVault to merge what changed — so please be explicit when you propose NEW ideas, CHANGE or REJECT an existing labelled item.\n\n")
	fmt.Fprintf(&b, "## Current objective\n\n%s\n\n", objective)
	b.WriteString("## Idea summary\n\n")
	if c.Idea.Summary != "" {
		b.WriteString(c.Idea.Summary + "\n\n")
	}
	fmt.Fprintf(&b, "- **Status:** %s\n", c.Idea.Status)
	if c.Branch != nil {
		fmt.Fprintf(&b, "- **Branch:** %s", c.Branch.Name)
		if c.Branch.ForkedFromCheckpointNumber != nil {
			fmt.Fprintf(&b, " (forked from CP%d)", *c.Branch.ForkedFromCheckpointNumber)
		}
		b.WriteString("\n")
	}
	if c.Checkpoint != nil {
		fmt.Fprintf(&b, "- **Checkpoint:** %s — %s (%s)\n", c.Checkpoint.Label, c.Checkpoint.Title, c.Checkpoint.CreatedAt.Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "- **Exported:** %s\n\n", time.Now().UTC().Format(time.RFC3339))
	if c.Origin != "" {
		b.WriteString("## Origin (in my own words)\n\n> " + strings.ReplaceAll(trimTo(c.Origin, 2000), "\n", "\n> ") + "\n\n")
	}
	section := func(title string, items []domain.KnowledgeItem, render func(domain.KnowledgeItem) string) {
		fmt.Fprintf(&b, "## %s\n\n", title)
		if len(items) == 0 {
			b.WriteString("_None recorded._\n\n")
			return
		}
		for _, it := range items {
			b.WriteString(render(it))
		}
		b.WriteString("\n")
	}
	simple := func(it domain.KnowledgeItem) string {
		tag := ""
		if it.Origin == domain.OriginInterpretation {
			tag = " _(inferred)_"
		}
		return fmt.Sprintf("- **[%s]** %s%s\n", it.Label, it.Statement, tag)
	}
	section("Decisions", c.Decisions, func(it domain.KnowledgeItem) string {
		s := simple(it)
		if it.Decision != nil && it.Decision.Rationale != "" {
			s += "  - Why: " + it.Decision.Rationale + "\n"
		}
		if it.Decision != nil {
			for _, alt := range it.Decision.Alternatives {
				s += "  - Rejected alternative: " + alt.Option
				if alt.ReasonRejected != "" {
					s += " — " + alt.ReasonRejected
				}
				s += "\n"
			}
		}
		return s
	})
	section("Assumptions", c.Assumptions, func(it domain.KnowledgeItem) string {
		risk := ""
		if it.Assumption != nil {
			risk = ", " + strings.ToLower(it.Assumption.Risk) + " risk"
		}
		return fmt.Sprintf("- **[%s]** %s _(%s%s)_\n", it.Label, it.Statement, strings.ToLower(it.Status), risk)
	})
	section("Evidence", c.Evidence, func(it domain.KnowledgeItem) string {
		s := simple(it)
		if it.Evidence != nil {
			s = strings.TrimSuffix(s, "\n") + fmt.Sprintf(" _(%s)_", strings.ToLower(it.Evidence.Stance))
			if it.Evidence.URL != "" {
				s += " — " + it.Evidence.URL
			}
			s += "\n"
		}
		return s
	})
	section("Insights", c.Insights, simple)
	section("Open questions", c.Questions, simple)
	section("Actions", c.Actions, func(it domain.KnowledgeItem) string {
		box := "[ ]"
		if it.Status == "DONE" {
			box = "[x]"
		}
		return fmt.Sprintf("- %s **[%s]** %s\n", box, it.Label, it.Statement)
	})
	if len(c.Contradictions) > 0 {
		b.WriteString("## Known contradictions\n\n")
		for _, x := range c.Contradictions {
			fmt.Fprintf(&b, "- %s vs %s — %s\n", x.A, x.B, x.Rationale)
		}
		b.WriteString("\n")
	}
	if len(c.History) > 0 {
		b.WriteString("## How my thinking changed (history, not current)\n\n")
		for _, it := range c.History {
			fmt.Fprintf(&b, "- ~~[%s] %s~~ — %s\n", it.Label, it.Statement, strings.ToLower(it.Status))
		}
		b.WriteString("\n")
	}
	if len(c.RecentDevelopments) > 0 {
		b.WriteString("## Recent developments\n\n")
		for _, e := range c.RecentDevelopments {
			fmt.Fprintf(&b, "- %s — %s\n", e.CreatedAt.Format("2006-01-02"), e.Summary)
		}
		b.WriteString("\n")
	}
	if len(c.RelatedIdeas) > 0 {
		b.WriteString("## Related ideas\n\n")
		for _, r := range c.RelatedIdeas {
			fmt.Fprintf(&b, "- %s (%s)\n", r.Title, r.Why)
		}
		b.WriteString("\n")
	}
	b.WriteString("---\n_Please reference labels (e.g. \"I'd change [D3] because…\") so IdeaVault can merge your feedback precisely._\n")
	return b.String()
}

func contextPackJSON(c *contextengine.Context, objective string) map[string]any {
	conv := func(items []domain.KnowledgeItem) []map[string]any {
		out := []map[string]any{}
		for _, it := range items {
			m := map[string]any{"label": it.Label, "statement": it.Statement, "status": it.Status, "origin": it.Origin, "id": it.ID}
			if it.Details != "" {
				m["details"] = it.Details
			}
			if it.Decision != nil {
				m["rationale"] = it.Decision.Rationale
				m["alternatives"] = it.Decision.Alternatives
			}
			if it.Assumption != nil {
				m["risk"] = it.Assumption.Risk
			}
			if it.Evidence != nil {
				m["stance"] = it.Evidence.Stance
				m["url"] = it.Evidence.URL
			}
			out = append(out, m)
		}
		return out
	}
	out := map[string]any{
		"format":    "ideavault.context_pack.v1",
		"objective": objective,
		"idea":      map[string]any{"id": c.Idea.ID, "title": c.Idea.Title, "summary": c.Idea.Summary, "origin": c.Origin, "status": c.Idea.Status},
		"decisions": conv(c.Decisions), "assumptions": conv(c.Assumptions), "evidence": conv(c.Evidence), "insights": conv(c.Insights),
		"questions": conv(c.Questions), "actions": conv(c.Actions), "history": conv(c.History),
		"contradictions": c.Contradictions, "related_ideas": c.RelatedIdeas,
	}
	if c.Branch != nil {
		out["branch"] = map[string]any{"id": c.Branch.ID, "name": c.Branch.Name}
	}
	if c.Checkpoint != nil {
		out["checkpoint"] = map[string]any{"id": c.Checkpoint.ID, "label": c.Checkpoint.Label, "title": c.Checkpoint.Title}
	}
	var recent []map[string]any
	for _, e := range c.RecentDevelopments {
		recent = append(recent, map[string]any{"at": e.CreatedAt, "summary": e.Summary, "type": e.EventType})
	}
	out["recent_developments"] = recent
	return out
}
