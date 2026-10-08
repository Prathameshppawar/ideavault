package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/artifacts"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/contextengine"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// GenerateArtifactInput generates an artifact from an idea's thinking.
type GenerateArtifactInput struct {
	IdeaID       uuid.UUID           `json:"idea_id"`
	BranchID     *uuid.UUID          `json:"branch_id,omitempty"`
	CheckpointID *uuid.UUID          `json:"checkpoint_id,omitempty"`
	Type         domain.ArtifactType `json:"type"`
	Title        string              `json:"title,omitempty"`
	Instructions string              `json:"instructions,omitempty"`
	// Generator forces "template" (deterministic) or "llm"; default picks LLM when available.
	Generator string `json:"generator,omitempty"`
	// Model pins "provider/model" for LLM generation.
	Model string `json:"model,omitempty"`
}

// ArtifactResult is a generated artifact with its provenance.
type ArtifactResult struct {
	Artifact   *domain.Artifact         `json:"artifact"`
	Version    *domain.ArtifactVersion  `json:"version"`
	Provenance []domain.ProvenanceEntry `json:"provenance"`
}

type generated struct {
	markdown  string
	generator string
	prov      []domain.ProvenanceEntry
	cpID      *uuid.UUID
	branchID  *uuid.UUID
}

func (s *Service) generateMarkdown(ctx context.Context, a Actor, in GenerateArtifactInput, title string, onDelta func(string)) (*generated, error) {
	tpl := artifacts.Get(in.Type)
	cctx, err := s.engine.Build(ctx, contextengine.Request{UserID: a.UserID, IdeaID: in.IdeaID, BranchID: in.BranchID, CheckpointID: in.CheckpointID,
		Query: in.Instructions + " " + tpl.Name, TokenBudget: 24000, Full: true})
	if err != nil {
		return nil, err
	}
	g := &generated{}
	if cctx.Checkpoint != nil {
		g.cpID = &cctx.Checkpoint.ID
		g.branchID = &cctx.Checkpoint.BranchID
	} else if cctx.Branch != nil {
		g.branchID = &cctx.Branch.ID
	}
	useLLM := in.Generator != "template" && s.gw != nil && (in.Model != "" || s.gw.HasRealModel(models.TaskArtifact))
	if useLLM {
		gctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		call := models.Call{Task: models.TaskArtifact, UserID: &a.UserID, IdeaID: &in.IdeaID, AgentRunID: a.AgentRunID, Pin: in.Model,
			Request: models.Request{System: artifacts.SystemPrompt(tpl), Messages: []models.Message{{Role: models.RoleUser, Content: artifacts.UserPrompt(tpl, title, cctx, in.Instructions)}}, MaxTokens: 12000}}
		if onDelta != nil {
			call.OnDelta = onDelta
		}
		res, err := s.gw.Do(gctx, call)
		if err == nil && strings.TrimSpace(res.Content) != "" {
			g.markdown = strings.TrimSpace(res.Content)
			g.generator = "llm:" + res.ModelKey
		} else if in.Generator == "llm" {
			if err == nil {
				err = fmt.Errorf("model returned empty content")
			}
			return nil, err
		} else if err != nil {
			s.log.WarnContext(ctx, "LLM artifact generation failed; using template", "error", err)
		}
	}
	if g.markdown == "" {
		g.markdown = artifacts.RenderDeterministic(tpl, title, cctx, in.Instructions)
		g.generator = "template:v1"
	}
	if !strings.HasPrefix(strings.TrimSpace(g.markdown), "#") {
		g.markdown = "# " + title + "\n\n" + g.markdown
	}
	// Provenance: every item given to the generator is an input; cited labels are marked cited.
	cited, cps := artifacts.CitedLabels(g.markdown)
	citedSet := map[string]bool{}
	for _, l := range cited {
		citedSet[l] = true
	}
	for _, it := range cctx.Refs() {
		role := "input"
		if citedSet[it.Label] {
			role = "cited"
		}
		g.prov = append(g.prov, domain.ProvenanceEntry{EntityType: it.Kind.EntityType(), EntityID: it.ID, Label: it.Label, Role: role})
	}
	if cctx.Checkpoint != nil {
		g.prov = append(g.prov, domain.ProvenanceEntry{EntityType: domain.EntityCheckpoint, EntityID: cctx.Checkpoint.ID, Label: cctx.Checkpoint.Label, Role: "base_checkpoint"})
	}
	for _, l := range cps {
		for _, cp := range cctx.Checkpoints {
			if cp.Label == l && (cctx.Checkpoint == nil || cp.ID != cctx.Checkpoint.ID) {
				g.prov = append(g.prov, domain.ProvenanceEntry{EntityType: domain.EntityCheckpoint, EntityID: cp.ID, Label: cp.Label, Role: "cited"})
			}
		}
	}
	g.prov = append(g.prov, domain.ProvenanceEntry{EntityType: domain.EntityIdea, EntityID: in.IdeaID, Label: cctx.Idea.Title, Role: "input"})
	return g, nil
}

// GenerateArtifact creates a new artifact (version 1) from recorded thinking with full provenance.
func (s *Service) GenerateArtifact(ctx context.Context, a Actor, in GenerateArtifactInput, onDelta func(string)) (*ArtifactResult, error) {
	t, ok := domain.ParseArtifactType(string(in.Type))
	if !ok {
		return nil, domain.Invalid("type", "unknown artifact type")
	}
	in.Type = t
	idea, err := s.store.GetIdea(ctx, a.UserID, in.IdeaID)
	if err != nil {
		return nil, err
	}
	if in.CheckpointID == nil {
		if _, err := s.resolveBranch(ctx, a.UserID, idea, in.BranchID); err != nil {
			return nil, err
		}
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = idea.Title + " — " + artifacts.Get(t).Name
	}
	g, err := s.generateMarkdown(ctx, a, in, title, onDelta)
	if err != nil {
		return nil, err
	}
	art := &domain.Artifact{UserID: a.UserID, IdeaID: idea.ID, BranchID: g.branchID, CheckpointID: g.cpID, Type: t, Title: trimTo(title, 300),
		ContentMarkdown: g.markdown, Status: domain.ArtifactDraft, Generator: g.generator, Instructions: in.Instructions}
	ver := &domain.ArtifactVersion{Title: art.Title, ContentMarkdown: g.markdown, ChangeNote: "Generated", CreatedBy: actorKind(a), Generator: g.generator,
		SourceCheckpointID: g.cpID, AgentRunID: a.AgentRunID}
	if err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		if err := tx.CreateArtifact(ctx, art, ver, g.prov); err != nil {
			return err
		}
		target := domain.EntityIdea
		targetID := idea.ID
		if g.cpID != nil {
			target, targetID = domain.EntityCheckpoint, *g.cpID
		}
		if err := tx.CreateRelationship(ctx, a.UserID, &domain.Relationship{FromType: domain.EntityArtifact, FromID: art.ID, ToType: target, ToID: targetID,
			RelType: domain.RelGeneratedFrom, IdeaID: &idea.ID, BranchID: g.branchID, Origin: domain.OriginSource, CreatedBy: a.Kind, AgentRunID: a.AgentRunID}); err != nil {
			return err
		}
		s.activity(ctx, tx, a, &idea.ID, g.branchID, "artifact.created", domain.EntityArtifact, &art.ID,
			fmt.Sprintf("%s generated: %s", t.HumanName(), art.Title), map[string]any{"generator": g.generator})
		return nil
	}); err != nil {
		return nil, err
	}
	s.enqueueEmbedding(ctx, a.UserID, domain.EntityArtifact, art.ID)
	full, err := s.store.GetArtifact(ctx, a.UserID, art.ID)
	if err != nil {
		return nil, err
	}
	prov, _ := s.store.ArtifactProvenance(ctx, a.UserID, art.ID, 0)
	return &ArtifactResult{Artifact: full, Version: ver, Provenance: prov}, nil
}

func actorKind(a Actor) domain.Actor {
	if a.Kind == domain.ActorAgent {
		return domain.ActorAgent
	}
	if a.Kind == domain.ActorUser {
		return domain.ActorUser
	}
	return domain.ActorSystem
}

// CreateArtifactInput creates a hand-written artifact.
type CreateArtifactInput struct {
	IdeaID          uuid.UUID           `json:"idea_id"`
	BranchID        *uuid.UUID          `json:"branch_id,omitempty"`
	CheckpointID    *uuid.UUID          `json:"checkpoint_id,omitempty"`
	Type            domain.ArtifactType `json:"type"`
	Title           string              `json:"title"`
	ContentMarkdown string              `json:"content_markdown"`
}

// CreateArtifact stores a user-authored Markdown artifact (version 1).
func (s *Service) CreateArtifact(ctx context.Context, a Actor, in CreateArtifactInput) (*ArtifactResult, error) {
	t, ok := domain.ParseArtifactType(string(in.Type))
	if !ok {
		t = domain.ArtifactCustom
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, domain.Invalid("title", "title is required")
	}
	if len(in.ContentMarkdown) > 2<<20 {
		return nil, domain.Invalid("content_markdown", "artifact is too large (max 2 MiB)")
	}
	idea, err := s.store.GetIdea(ctx, a.UserID, in.IdeaID)
	if err != nil {
		return nil, err
	}
	br, err := s.resolveBranch(ctx, a.UserID, idea, in.BranchID)
	if err != nil {
		return nil, err
	}
	art := &domain.Artifact{UserID: a.UserID, IdeaID: idea.ID, BranchID: &br.ID, CheckpointID: in.CheckpointID, Type: t, Title: trimTo(title, 300),
		ContentMarkdown: in.ContentMarkdown, Generator: "user"}
	ver := &domain.ArtifactVersion{Title: art.Title, ContentMarkdown: in.ContentMarkdown, ChangeNote: "Created", CreatedBy: actorKind(a), Generator: "user", SourceCheckpointID: in.CheckpointID, AgentRunID: a.AgentRunID}
	prov := []domain.ProvenanceEntry{{EntityType: domain.EntityIdea, EntityID: idea.ID, Label: idea.Title, Role: "input"}}
	cited, _ := artifacts.CitedLabels(in.ContentMarkdown)
	for _, l := range cited {
		if k, n, ok := domain.ParseRefLabel(l); ok {
			if it, err := s.store.FindKnowledgeByRef(ctx, a.UserID, idea.ID, &br.ID, k, n); err == nil {
				prov = append(prov, domain.ProvenanceEntry{EntityType: it.Kind.EntityType(), EntityID: it.ID, Label: it.Label, Role: "cited"})
			}
		}
	}
	if err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		if err := tx.CreateArtifact(ctx, art, ver, prov); err != nil {
			return err
		}
		s.activity(ctx, tx, a, &idea.ID, &br.ID, "artifact.created", domain.EntityArtifact, &art.ID, "Artifact created: "+art.Title, nil)
		return nil
	}); err != nil {
		return nil, err
	}
	s.enqueueEmbedding(ctx, a.UserID, domain.EntityArtifact, art.ID)
	full, _ := s.store.GetArtifact(ctx, a.UserID, art.ID)
	p, _ := s.store.ArtifactProvenance(ctx, a.UserID, art.ID, 0)
	return &ArtifactResult{Artifact: full, Version: ver, Provenance: p}, nil
}

// UpdateArtifactInput edits an artifact; every edit is a new immutable version.
type UpdateArtifactInput struct {
	Title           *string `json:"title,omitempty"`
	ContentMarkdown *string `json:"content_markdown,omitempty"`
	ChangeNote      string  `json:"change_note,omitempty"`
	Status          *string `json:"status,omitempty"`
}

// UpdateArtifact appends a new version (content/title) and/or changes status.
func (s *Service) UpdateArtifact(ctx context.Context, a Actor, id uuid.UUID, in UpdateArtifactInput) (*ArtifactResult, error) {
	art, err := s.store.GetArtifact(ctx, a.UserID, id)
	if err != nil {
		return nil, err
	}
	var ver *domain.ArtifactVersion
	if in.Title != nil || in.ContentMarkdown != nil {
		title := art.Title
		if in.Title != nil && strings.TrimSpace(*in.Title) != "" {
			title = trimTo(*in.Title, 300)
		}
		content := art.ContentMarkdown
		if in.ContentMarkdown != nil {
			content = *in.ContentMarkdown
		}
		if len(content) > 2<<20 {
			return nil, domain.Invalid("content_markdown", "artifact is too large (max 2 MiB)")
		}
		if title == art.Title && content == art.ContentMarkdown {
			return nil, domain.Invalid("content_markdown", "no changes")
		}
		note := in.ChangeNote
		if note == "" {
			note = "Edited"
		}
		ver = &domain.ArtifactVersion{Title: title, ContentMarkdown: content, ChangeNote: note, CreatedBy: actorKind(a), Generator: "user", AgentRunID: a.AgentRunID}
		prov := []domain.ProvenanceEntry{{EntityType: domain.EntityIdea, EntityID: art.IdeaID, Label: art.IdeaTitle, Role: "input"}}
		cited, _ := artifacts.CitedLabels(content)
		for _, l := range cited {
			if k, n, ok := domain.ParseRefLabel(l); ok {
				if it, err := s.store.FindKnowledgeByRef(ctx, a.UserID, art.IdeaID, art.BranchID, k, n); err == nil {
					prov = append(prov, domain.ProvenanceEntry{EntityType: it.Kind.EntityType(), EntityID: it.ID, Label: it.Label, Role: "cited"})
				}
			}
		}
		if err := s.store.AddArtifactVersion(ctx, a.UserID, art, ver, prov); err != nil {
			return nil, err
		}
		s.activity(ctx, s.store, a, &art.IdeaID, art.BranchID, "artifact.versioned", domain.EntityArtifact, &art.ID, fmt.Sprintf("%s v%d: %s", art.Title, ver.Version, note), nil)
	}
	if in.Status != nil {
		st := domain.ArtifactStatus(strings.ToUpper(*in.Status))
		if st != domain.ArtifactDraft && st != domain.ArtifactFinal && st != domain.ArtifactArchived {
			return nil, domain.Invalid("status", "status must be DRAFT, FINAL or ARCHIVED")
		}
		if err := s.store.UpdateArtifactStatus(ctx, a.UserID, id, st); err != nil {
			return nil, err
		}
	}
	s.enqueueEmbedding(ctx, a.UserID, domain.EntityArtifact, id)
	full, err := s.store.GetArtifact(ctx, a.UserID, id)
	if err != nil {
		return nil, err
	}
	prov, _ := s.store.ArtifactProvenance(ctx, a.UserID, id, 0)
	return &ArtifactResult{Artifact: full, Version: ver, Provenance: prov}, nil
}

// RegenerateArtifactInput regenerates an artifact as a new version.
type RegenerateArtifactInput struct {
	Instructions string     `json:"instructions,omitempty"`
	CheckpointID *uuid.UUID `json:"checkpoint_id,omitempty"`
	BranchID     *uuid.UUID `json:"branch_id,omitempty"`
	Generator    string     `json:"generator,omitempty"`
	Model        string     `json:"model,omitempty"`
}

// RegenerateArtifact rebuilds an artifact from current (or checkpointed) thinking as a new version.
func (s *Service) RegenerateArtifact(ctx context.Context, a Actor, id uuid.UUID, in RegenerateArtifactInput, onDelta func(string)) (*ArtifactResult, error) {
	art, err := s.store.GetArtifact(ctx, a.UserID, id)
	if err != nil {
		return nil, err
	}
	instr := in.Instructions
	if instr == "" {
		instr = art.Instructions
	}
	branch := in.BranchID
	if branch == nil {
		branch = art.BranchID
	}
	g, err := s.generateMarkdown(ctx, a, GenerateArtifactInput{IdeaID: art.IdeaID, BranchID: branch, CheckpointID: in.CheckpointID, Type: art.Type,
		Instructions: instr, Generator: in.Generator, Model: in.Model}, art.Title, onDelta)
	if err != nil {
		return nil, err
	}
	ver := &domain.ArtifactVersion{Title: art.Title, ContentMarkdown: g.markdown, ChangeNote: "Regenerated" + suffixIf(in.Instructions, ": "),
		CreatedBy: actorKind(a), Generator: g.generator, SourceCheckpointID: g.cpID, AgentRunID: a.AgentRunID}
	if err := s.store.AddArtifactVersion(ctx, a.UserID, art, ver, g.prov); err != nil {
		return nil, err
	}
	s.activity(ctx, s.store, a, &art.IdeaID, art.BranchID, "artifact.regenerated", domain.EntityArtifact, &art.ID, fmt.Sprintf("%s regenerated (v%d)", art.Title, ver.Version), nil)
	s.enqueueEmbedding(ctx, a.UserID, domain.EntityArtifact, id)
	full, _ := s.store.GetArtifact(ctx, a.UserID, id)
	prov, _ := s.store.ArtifactProvenance(ctx, a.UserID, id, 0)
	return &ArtifactResult{Artifact: full, Version: ver, Provenance: prov}, nil
}

func suffixIf(s, sep string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return sep + trimTo(s, 120)
}

// DeleteArtifact permanently deletes an artifact and its history (DESTRUCTIVE).
func (s *Service) DeleteArtifact(ctx context.Context, a Actor, id uuid.UUID) error {
	art, err := s.store.GetArtifact(ctx, a.UserID, id)
	if err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx *postgres.Store) error {
		if err := tx.DeleteArtifact(ctx, a.UserID, id); err != nil {
			return err
		}
		s.activity(ctx, tx, a, &art.IdeaID, nil, "artifact.deleted", domain.EntityArtifact, nil, "Deleted artifact: "+art.Title, nil)
		return nil
	})
}

// ArtifactExplanation traces an artifact to the thinking that produced it.
type ArtifactExplanation struct {
	Artifact   *domain.Artifact         `json:"artifact"`
	Version    int                      `json:"version"`
	Provenance []domain.ProvenanceEntry `json:"provenance"`
	Items      []domain.KnowledgeItem   `json:"items"`
	Checkpoint *domain.Checkpoint       `json:"checkpoint,omitempty"`
	Answer     string                   `json:"answer,omitempty"`
	Analyzer   string                   `json:"analyzer,omitempty"`
}

// ExplainArtifact answers "why does this artifact say X?" using its recorded provenance.
func (s *Service) ExplainArtifact(ctx context.Context, userID, id uuid.UUID, question string, version int) (*ArtifactExplanation, error) {
	art, err := s.store.GetArtifact(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	prov, err := s.store.ArtifactProvenance(ctx, userID, id, version)
	if err != nil {
		return nil, err
	}
	ex := &ArtifactExplanation{Artifact: art, Version: art.CurrentVersion, Provenance: prov}
	if version > 0 {
		ex.Version = version
	}
	var ids []uuid.UUID
	for _, p := range prov {
		if p.EntityType.IsKnowledge() {
			ids = append(ids, p.EntityID)
		}
		if p.Role == "base_checkpoint" {
			ex.Checkpoint, _ = s.store.GetCheckpoint(ctx, userID, p.EntityID)
			if ex.Checkpoint != nil {
				ex.Checkpoint.Snapshot = nil
			}
		}
	}
	if len(ids) > 0 {
		ex.Items, _ = s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, IDs: ids, Limit: 2000})
	}
	if strings.TrimSpace(question) == "" {
		return ex, nil
	}
	if s.gw == nil || !s.gw.HasRealModel(models.TaskSynthesis) {
		ex.Answer = deterministicArtifactAnswer(question, art, ex.Items)
		ex.Analyzer = "deterministic"
		return ex, nil
	}
	var b strings.Builder
	for _, it := range ex.Items {
		fmt.Fprintf(&b, "[%s] (%s) %s", it.Label, it.Status, trimTo(it.Statement, 300))
		if it.Decision != nil && it.Decision.Rationale != "" {
			fmt.Fprintf(&b, " — rationale: %s", trimTo(it.Decision.Rationale, 300))
		}
		b.WriteString("\n")
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := s.gw.Do(cctx, models.Call{Task: models.TaskSynthesis, UserID: &userID, IdeaID: &art.IdeaID, Request: models.Request{
		System: "Explain why an artifact says something, using ONLY the provenance items listed (the recorded thinking that produced it). Cite labels like [D3]. If the artifact's statement is not supported by any listed item, say so plainly.",
		Messages: []models.Message{{Role: models.RoleUser, Content: fmt.Sprintf("Question: %s\n\nArtifact (v%d):\n<artifact>\n%s\n</artifact>\n\nProvenance items:\n%s",
			question, ex.Version, trimTo(art.ContentMarkdown, 16000), b.String())}},
		MaxTokens: 2000,
	}})
	if err != nil {
		ex.Answer = deterministicArtifactAnswer(question, art, ex.Items)
		ex.Analyzer = "deterministic"
		return ex, nil
	}
	ex.Answer = strings.TrimSpace(res.Content)
	ex.Analyzer = "llm:" + res.ModelKey
	return ex, nil
}

func deterministicArtifactAnswer(question string, art *domain.Artifact, items []domain.KnowledgeItem) string {
	qt := strings.Fields(strings.ToLower(question))
	var hits []string
	for _, it := range items {
		text := strings.ToLower(it.Statement)
		n := 0
		for _, w := range qt {
			w = strings.Trim(w, `?!.,;:"'()[]`) // "…the marketplace?" must still match "marketplace"
			if len(w) > 3 && strings.Contains(text, w) {
				n++
			}
		}
		if n > 0 {
			line := fmt.Sprintf("- [%s] %s", it.Label, it.Statement)
			if it.Decision != nil && it.Decision.Rationale != "" {
				line += " — because " + it.Decision.Rationale
			}
			hits = append(hits, line)
		}
	}
	if len(hits) == 0 {
		return fmt.Sprintf("No recorded item in this artifact's provenance mentions that directly. %s was generated from %d recorded item(s); open its provenance to inspect them.", art.Title, len(items))
	}
	return "These recorded items in the artifact's provenance relate to your question:\n" + strings.Join(hits, "\n")
}
