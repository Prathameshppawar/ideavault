package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

func analyzeTools() []Tool {
	return []Tool{
		{Name: "analyze_conversation", Category: domain.ToolAnalyze,
			Description: "Extract candidate decisions/assumptions/evidence/insights/questions/actions from a stored conversation (does not save them; record the ones the user wants with record_* tools or they appear as proposals after imports).",
			Params:      schema(`{"type":"object","properties":{"conversation_id":{"type":"string"},` + ideaParam + `},"required":["conversation_id"]}`), Run: runAnalyzeConversation},
		{Name: "analyze_prompt", Category: domain.ToolAnalyze,
			Description: "Evaluate the quality of a prompt (context, goal, constraints, specificity, examples, expected output, clarity, completeness): what was good, weak, why it matters, and an improved prompt. Defaults to the user's previous message if text is omitted.",
			Params:      schema(`{"type":"object","properties":{"text":{"type":"string"},"message_id":{"type":"string"}}}`), Run: runAnalyzePrompt},
		{Name: "compare_branches", Category: domain.ToolAnalyze, Description: "Compare two branches of an idea: what exists only in each, what diverged, where they split.",
			Params: schema(`{"type":"object","properties":{` + ideaParam + `,"branch_a":{"type":"string"},"branch_b":{"type":"string"}},"required":["branch_a","branch_b"]}`), Run: runCompareBranches},
		{Name: "find_thinking_patterns", Category: domain.ToolAnalyze,
			Description: "Identify observable patterns in the user's thinking and prompting (reversals, unvalidated assumptions, recurring questions, strong reasoning, prompt habits). No psychological diagnosis.",
			Params:      schema(`{"type":"object","properties":{` + ideaParam + `,"scope":{"type":"string","enum":["vault","idea"]}}}`), Run: runThinking},
		{Name: "detect_contradictions", Category: domain.ToolAnalyze, Description: "Detect contradictions and changed decisions in an idea's thinking, grounded in recorded items; records confirmed contradictions as interpreted relationships.",
			Params: schema(`{"type":"object","properties":{` + ideaParam + `,` + branchParam + `}}`), Run: runContradictions},
		{Name: "build_context_pack", Category: domain.ToolAnalyze, Description: "Build a portable context pack (Markdown + JSON) of the current thinking to continue in ChatGPT/Claude/Gemini.",
			Params: schema(`{"type":"object","properties":{` + ideaParam + `,` + branchParam + `,"checkpoint":{"type":"string"},"objective":{"type":"string"}}}`), Run: runContextPack},
		{Name: "compare_checkpoint", Category: domain.ToolAnalyze, Description: "Diff two checkpoints of an idea: added, removed, changed and superseded items.",
			Params: schema(`{"type":"object","properties":{` + ideaParam + `,"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}`), Run: runCompareCheckpoints},
		{Name: "analyze_idea_evolution", Category: domain.ToolAnalyze, Description: "Explain how an idea evolved across checkpoints and branches.",
			Params: schema(`{"type":"object","properties":{` + ideaParam + `}}`), Run: runEvolution},
		{Name: "analyze_artifact", Category: domain.ToolAnalyze, Description: "Trace an artifact back to the thinking that produced it and answer why it says something.",
			Params: schema(`{"type":"object","properties":{"artifact_id":{"type":"string"},"question":{"type":"string"}},"required":["artifact_id"]}`), Run: runAnalyzeArtifact},
	}
}

func runAnalyzeConversation(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		ConversationID string `json:"conversation_id"`
		Idea           string `json:"idea"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	cid, err := uuid.Parse(a.ConversationID)
	if err != nil {
		return nil, domain.Invalid("conversation_id", "must be a conversation id")
	}
	msgs, err := tc.Svc.Store().ListMessages(ctx, tc.UserID, cid, 0, 2000)
	if err != nil {
		return nil, err
	}
	var em []service.ExtractMessage
	for i, m := range msgs {
		id := m.ID
		em = append(em, service.ExtractMessage{Index: i, Role: string(m.Role), Content: m.Content, MessageID: &id})
	}
	var ideaID *uuid.UUID
	title := ""
	if idea, err := tc.resolveIdea(ctx, a.Idea); err == nil {
		ideaID, title = &idea.ID, idea.Title
	}
	tc.Progress(fmt.Sprintf("Analyzing %s", plural(len(msgs), "message")))
	res, err := tc.Svc.ExtractKnowledge(ctx, tc.UserID, ideaID, title, em)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Summary: fmt.Sprintf("Analyzed conversation: %s found", plural(len(res.Items), "candidate item")), Untrusted: true,
		Data: map[string]any{"summary": res.Summary, "analyzer": res.Analyzer, "candidates": res.Items}}, nil
}

func runAnalyzePrompt(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		Text      string `json:"text"`
		MessageID string `json:"message_id"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	var mid *uuid.UUID
	if id, err := uuid.Parse(a.MessageID); err == nil {
		mid = &id
	}
	text := a.Text
	if text == "" && mid == nil {
		// Default: the user's previous message in this conversation.
		msgs, _ := tc.Svc.Store().RecentMessages(ctx, tc.UserID, tc.ConversationID, 10)
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == domain.RoleUser && (tc.UserMessageID == nil || msgs[i].ID != *tc.UserMessageID) {
				id := msgs[i].ID
				mid = &id
				break
			}
		}
		if mid == nil {
			text = tc.UserMessage
		}
	}
	pa, err := tc.Svc.AnalyzePrompt(ctx, tc.Actor, text, mid)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Summary: fmt.Sprintf("Analyzed prompt quality: %d/100", pa.Overall), Data: pa}, nil
}

func runCompareBranches(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		Idea    string `json:"idea"`
		BranchA string `json:"branch_a"`
		BranchB string `json:"branch_b"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	idea, err := tc.resolveIdea(ctx, a.Idea)
	if err != nil {
		return nil, err
	}
	ba, err := tc.resolveBranch(ctx, idea, a.BranchA)
	if err != nil {
		return nil, err
	}
	bb, err := tc.resolveBranch(ctx, idea, a.BranchB)
	if err != nil {
		return nil, err
	}
	cmp, err := tc.Svc.CompareBranches(ctx, tc.UserID, ba.ID, bb.ID, true)
	if err != nil {
		return nil, err
	}
	data := map[string]any{"a": ba.Name, "b": bb.Name, "only_in_a": compactItems(cmp.OnlyInA, 30), "only_in_b": compactItems(cmp.OnlyInB, 30), "shared": cmp.Shared,
		"narrative": cmp.Narrative, "analyzer": cmp.Analyzer}
	var div []map[string]any
	for _, d := range cmp.Diverged {
		div = append(div, map[string]any{"label": d.Before.Label, "statement": d.Before.Statement, "status_a": d.Before.Status, "status_b": d.After.Status})
	}
	data["diverged"] = div
	if cmp.CommonAncestor != nil {
		data["diverged_at"] = cmp.CommonAncestor.Label
	}
	return &ToolResult{Summary: fmt.Sprintf("Compared branches %s and %s", ba.Name, bb.Name), Data: data,
		Refs: []domain.EntityRef{{Type: domain.EntityBranch, ID: ba.ID, Title: ba.Name}, {Type: domain.EntityBranch, ID: bb.ID, Title: bb.Name}}}, nil
}

func runThinking(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		Idea  string `json:"idea"`
		Scope string `json:"scope"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	var ideaID *uuid.UUID
	if a.Scope == "idea" || a.Idea != "" {
		idea, err := tc.resolveIdea(ctx, a.Idea)
		if err != nil {
			return nil, err
		}
		ideaID = &idea.ID
	}
	ta, err := tc.Svc.AnalyzeThinking(ctx, tc.Actor, ideaID)
	if err != nil {
		return nil, err
	}
	var refs []domain.EntityRef
	for _, p := range ta.Patterns {
		refs = append(refs, p.Evidence...)
	}
	return &ToolResult{Summary: fmt.Sprintf("Found %s in your thinking", plural(len(ta.Patterns), "pattern")), Refs: refs, Data: ta}, nil
}

func runContradictions(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		Idea   string `json:"idea"`
		Branch string `json:"branch"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	idea, err := tc.resolveIdea(ctx, a.Idea)
	if err != nil {
		return nil, err
	}
	br, err := tc.resolveBranch(ctx, idea, a.Branch)
	if err != nil {
		return nil, err
	}
	fs, analyzer, err := tc.Svc.DetectContradictions(ctx, tc.Actor, idea.ID, &br.ID)
	if err != nil {
		return nil, err
	}
	var refs []domain.EntityRef
	n := 0
	for _, f := range fs {
		refs = append(refs, f.A, f.B)
		if f.Kind == "contradiction" {
			n++
		}
	}
	return &ToolResult{Summary: fmt.Sprintf("Found %s and %s", plural(n, "contradiction"), plural(len(fs)-n, "change in thinking")), Refs: refs,
		Data: map[string]any{"findings": fs, "analyzer": analyzer}}, nil
}

func runContextPack(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		Idea       string `json:"idea"`
		Branch     string `json:"branch"`
		Checkpoint string `json:"checkpoint"`
		Objective  string `json:"objective"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	idea, err := tc.resolveIdea(ctx, a.Idea)
	if err != nil {
		return nil, err
	}
	in := service.BuildContextPackInput{IdeaID: idea.ID, Objective: a.Objective}
	if a.Checkpoint != "" {
		cp, err := tc.resolveCheckpoint(ctx, idea, a.Checkpoint)
		if err != nil {
			return nil, err
		}
		in.CheckpointID = &cp.ID
	} else {
		br, err := tc.resolveBranch(ctx, idea, a.Branch)
		if err != nil {
			return nil, err
		}
		in.BranchID = &br.ID
	}
	pack, err := tc.Svc.BuildContextPack(ctx, tc.Actor, in)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Summary: fmt.Sprintf("Built context pack (~%d tokens)", pack.TokenEstimate),
		Refs: []domain.EntityRef{{Type: domain.EntityContextPack, ID: pack.ID, Title: pack.Title}},
		Data: map[string]any{"id": pack.ID, "title": pack.Title, "token_estimate": pack.TokenEstimate, "link": link(domain.EntityContextPack, pack.ID),
			"preview": trim(pack.ContentMarkdown, 1500), "note": "The full pack can be copied or downloaded (Markdown/JSON) from the link."}}, nil
}

func runCompareCheckpoints(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		Idea string `json:"idea"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	idea, err := tc.resolveIdea(ctx, a.Idea)
	if err != nil {
		return nil, err
	}
	from, err := tc.resolveCheckpoint(ctx, idea, a.From)
	if err != nil {
		return nil, err
	}
	to, err := tc.resolveCheckpoint(ctx, idea, a.To)
	if err != nil {
		return nil, err
	}
	d, err := tc.Svc.CompareCheckpoints(ctx, tc.UserID, from.ID, to.ID)
	if err != nil {
		return nil, err
	}
	var changed, sup []map[string]any
	for _, c := range d.Changed {
		changed = append(changed, map[string]any{"label": c.After.Label, "statement": c.After.Statement, "before": c.Before.Status, "after": c.After.Status})
	}
	for _, x := range d.Superseded {
		sup = append(sup, map[string]any{"old": x.Old.Label + " " + x.Old.Statement, "new": x.New.Label + " " + x.New.Statement})
	}
	return &ToolResult{Summary: fmt.Sprintf("Compared %s → %s: +%d, -%d, %d changed", from.Label, to.Label, len(d.Added), len(d.Removed), len(d.Changed)),
		Data: map[string]any{"from": from.Label, "to": to.Label, "added": compactItems(d.Added, 40), "removed": compactItems(d.Removed, 40), "changed": changed, "superseded": sup, "unchanged": d.Unchanged}}, nil
}

func runEvolution(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		Idea string `json:"idea"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	idea, err := tc.resolveIdea(ctx, a.Idea)
	if err != nil {
		return nil, err
	}
	ev, err := tc.Svc.AnalyzeEvolution(ctx, tc.Actor, idea.ID)
	if err != nil {
		return nil, err
	}
	var steps []map[string]any
	for _, s := range ev.Steps {
		m := map[string]any{"checkpoint": s.Checkpoint.Label, "title": s.Checkpoint.Title, "branch": s.Checkpoint.BranchName, "summary": trim(s.Checkpoint.Summary, 300)}
		if s.Diff != nil {
			m["added"], m["superseded"], m["changed"] = len(s.Diff.Added), len(s.Diff.Superseded), len(s.Diff.Changed)
		}
		steps = append(steps, m)
	}
	return &ToolResult{Summary: fmt.Sprintf("Analyzed evolution across %s", plural(len(ev.Steps), "checkpoint")),
		Data: map[string]any{"narrative": ev.Narrative, "steps": steps, "versions": len(ev.Versions), "branches": len(ev.Branches), "analyzer": ev.Analyzer}}, nil
}

func runAnalyzeArtifact(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
	var a struct {
		ArtifactID string `json:"artifact_id"`
		Question   string `json:"question"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(a.ArtifactID)
	if err != nil {
		return nil, domain.Invalid("artifact_id", "must be an artifact id")
	}
	ex, err := tc.Svc.ExplainArtifact(ctx, tc.UserID, id, firstNonEmpty(a.Question, tc.UserMessage), 0)
	if err != nil {
		return nil, err
	}
	var prov []map[string]any
	for _, p := range ex.Provenance {
		prov = append(prov, map[string]any{"type": p.EntityType, "label": p.Label, "role": p.Role, "title": trim(p.Title, 120), "status": p.Status})
	}
	return &ToolResult{Summary: fmt.Sprintf("Traced %s to %s", ex.Artifact.Title, plural(len(ex.Items), "recorded item")), Refs: refsOf(ex.Items, 15),
		Data: map[string]any{"artifact": ex.Artifact.Title, "version": ex.Version, "answer": ex.Answer, "analyzer": ex.Analyzer, "provenance": prov}}, nil
}

func deltaTool() Tool {
	return Tool{Name: "analyze_external_conversation", Category: domain.ToolAnalyze,
		Description: "Analyze a conversation the user had in another AI tool (pasted text or public share link) against an idea's current branch. Produces a delta of NEW / CHANGED / REJECTED / UNCHANGED items for the user to review and merge in the UI. Nothing is merged automatically.",
		Params:      schema(`{"type":"object","properties":{` + ideaParam + `,` + branchParam + `,"text":{"type":"string"},"url":{"type":"string"},"provider":{"type":"string"}}}`),
		Run: func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (*ToolResult, error) {
			var a struct {
				Idea     string `json:"idea"`
				Branch   string `json:"branch"`
				Text     string `json:"text"`
				URL      string `json:"url"`
				Provider string `json:"provider"`
			}
			if err := decode(raw, &a); err != nil {
				return nil, err
			}
			idea, err := tc.resolveIdea(ctx, a.Idea)
			if err != nil {
				return nil, err
			}
			br, err := tc.resolveBranch(ctx, idea, a.Branch)
			if err != nil {
				return nil, err
			}
			tc.Progress("Comparing external conversation with " + br.Name)
			d, err := tc.Svc.AnalyzeDelta(ctx, tc.Actor, service.AnalyzeDeltaInput{IdeaID: idea.ID, BranchID: &br.ID, Text: a.Text, URL: a.URL, Provider: a.Provider})
			if err != nil {
				return nil, err
			}
			counts := map[string]int{}
			var items []map[string]any
			for _, it := range d.Items {
				counts[string(it.Classification)]++
				m := map[string]any{"change": it.Classification, "kind": it.Kind, "statement": trim(it.Statement, 240)}
				if it.TargetLabel != "" {
					m["target"] = it.TargetLabel
				}
				items = append(items, m)
			}
			return &ToolResult{Summary: fmt.Sprintf("Delta: %d new, %d changed, %d rejected, %d unchanged", counts["NEW"], counts["CHANGED"], counts["REJECTED"], counts["UNCHANGED"]), Untrusted: true,
				Refs: []domain.EntityRef{{Type: domain.EntityIdea, ID: idea.ID, Title: idea.Title}},
				Data: map[string]any{"delta_id": d.ID, "review_link": "iv://delta/" + d.ID.String(), "summary": d.Summary, "analyzer": d.Analyzer, "counts": counts, "items": items,
					"note": "Nothing has been merged. The user reviews and merges selected changes."}}, nil
		}}
}
