package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// CreateIdeaInput creates an Idea Space.
type CreateIdeaInput struct {
	Title          string     `json:"title"`
	OriginText     string     `json:"origin_text"`
	Summary        string     `json:"summary"`
	Tags           []string   `json:"tags"`
	Status         string     `json:"status,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	SourceID       *uuid.UUID `json:"source_id,omitempty"`
}

// IdeaCreated is the result of creating an idea.
type IdeaCreated struct {
	Idea   *domain.Idea   `json:"idea"`
	Branch *domain.Branch `json:"branch"`
}

// CreateIdea creates an Idea Space with its initial "Main" branch and preserves the originating conversation.
func (s *Service) CreateIdea(ctx context.Context, a Actor, in CreateIdeaInput) (*IdeaCreated, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = trimTo(firstLine(in.OriginText), 80)
	}
	if title == "" {
		return nil, domain.Invalid("title", "an idea needs a title or an origin text")
	}
	if len([]rune(title)) > 300 {
		return nil, domain.Invalid("title", "title is too long (max 300 characters)")
	}
	status := domain.StatusExploring
	if in.Status != "" {
		status = domain.IdeaStatus(strings.ToUpper(in.Status))
		if !status.Valid() {
			return nil, domain.Invalid("status", "unknown status")
		}
	}
	out := &IdeaCreated{}
	err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		idea := &domain.Idea{UserID: a.UserID, Title: title, OriginText: strings.TrimSpace(in.OriginText), Summary: strings.TrimSpace(in.Summary),
			Status: status, Tags: normalizeTags(in.Tags), OriginSourceID: in.SourceID}
		if err := tx.CreateIdea(ctx, idea); err != nil {
			return err
		}
		br := &domain.Branch{UserID: a.UserID, IdeaID: idea.ID, Name: "Main", Description: "The original line of thinking.", IsDefault: true}
		if err := tx.CreateBranch(ctx, br); err != nil {
			return err
		}
		if err := tx.SetDefaultBranch(ctx, a.UserID, idea.ID, br.ID); err != nil {
			return err
		}
		idea.DefaultBranchID = &br.ID
		if in.ConversationID != nil {
			conv, err := tx.GetConversation(ctx, a.UserID, *in.ConversationID)
			if err != nil {
				return err
			}
			if conv.IdeaID == nil || *conv.IdeaID == idea.ID {
				if err := tx.AttachConversation(ctx, a.UserID, conv.ID, &idea.ID, &br.ID); err != nil {
					return err
				}
			} else {
				// Conversation already belongs to another idea: reference it rather than moving it.
				if err := tx.LinkToBranch(ctx, br.ID, domain.EntityConversation, conv.ID, "referenced", nil); err != nil {
					return err
				}
			}
			if err := tx.SetIdeaOriginConversation(ctx, idea.ID, conv.ID); err != nil {
				return err
			}
			idea.OriginConversationID = &conv.ID
		}
		v := &domain.IdeaVersion{IdeaID: idea.ID, Version: 1, Title: idea.Title, Summary: idea.Summary, Status: idea.Status, Tags: idea.Tags,
			ChangedFields: []string{"created"}, ChangeReason: "Idea Space created", CreatedBy: a.Kind}
		if err := tx.InsertIdeaVersion(ctx, v, a.AgentRunID); err != nil {
			return err
		}
		s.activity(ctx, tx, a, &idea.ID, &br.ID, "idea.created", domain.EntityIdea, &idea.ID, "Idea Space created: "+idea.Title, nil)
		out.Idea, out.Branch = idea, br
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.enqueueEmbedding(ctx, a.UserID, domain.EntityIdea, out.Idea.ID)
	return out, nil
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] || len(t) > 40 {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// IdeaCandidate is a possible existing match for new-idea text.
type IdeaCandidate struct {
	Idea     domain.Idea `json:"idea"`
	Score    float64     `json:"score"`
	Lexical  float64     `json:"lexical"`
	Semantic float64     `json:"semantic"`
	Verdict  string      `json:"verdict"` // likely_duplicate | related | weak
}

// FindIdeaCandidates searches existing ideas that match text (lexical + semantic) to avoid duplicates.
func (s *Service) FindIdeaCandidates(ctx context.Context, userID uuid.UUID, text string, limit int) ([]IdeaCandidate, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	byID := map[uuid.UUID]*IdeaCandidate{}
	lex, err := s.store.FindSimilarIdeas(ctx, userID, trimTo(text, 500), 10)
	if err != nil {
		return nil, err
	}
	for _, m := range lex {
		m := m
		byID[m.Idea.ID] = &IdeaCandidate{Idea: m.Idea, Lexical: m.Score}
	}
	if s.gw != nil && s.gw.Embedder() != nil {
		vec, err := s.gw.EmbedQuery(ctx, &userID, trimTo(text, 4000))
		if err == nil {
			hits, err := s.store.SemanticSearch(ctx, userID, s.gw.Embedder().Model(), vec, []domain.EntityType{domain.EntityIdea}, nil, 10)
			if err == nil {
				for _, h := range hits {
					if c, ok := byID[h.EntityID]; ok {
						c.Semantic = max(c.Semantic, h.Score)
						continue
					}
					idea, err := s.store.GetIdea(ctx, userID, h.EntityID)
					if err != nil {
						continue
					}
					byID[h.EntityID] = &IdeaCandidate{Idea: *idea, Semantic: h.Score}
				}
			}
		}
	}
	var out []IdeaCandidate
	for _, c := range byID {
		c.Score = max(c.Lexical, c.Semantic*0.9)
		switch {
		case c.Lexical >= 0.45 || c.Semantic >= 0.82:
			c.Verdict = "likely_duplicate"
		case c.Lexical >= 0.2 || c.Semantic >= 0.6:
			c.Verdict = "related"
		default:
			c.Verdict = "weak"
		}
		if c.Verdict != "weak" {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// GetIdea returns an idea with stats.
func (s *Service) GetIdea(ctx context.Context, userID, id uuid.UUID) (*domain.Idea, error) {
	idea, err := s.store.GetIdea(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	stats, err := s.store.IdeaStats(ctx, userID, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	idea.Stats = stats[id]
	return idea, nil
}

// ResolveIdea finds an idea by id, slug or fuzzy title.
func (s *Service) ResolveIdea(ctx context.Context, userID uuid.UUID, ref string) (*domain.Idea, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, domain.Invalid("idea", "idea reference is empty")
	}
	if id, err := uuid.Parse(ref); err == nil {
		return s.GetIdea(ctx, userID, id)
	}
	if idea, err := s.store.GetIdeaBySlug(ctx, userID, domain.Slugify(ref)); err == nil {
		return s.GetIdea(ctx, userID, idea.ID)
	}
	matches, err := s.store.FindSimilarIdeas(ctx, userID, ref, 3)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 || matches[0].Score < 0.15 {
		return nil, domain.NotFound(fmt.Sprintf("idea matching %q", ref))
	}
	return s.GetIdea(ctx, userID, matches[0].Idea.ID)
}

// ListIdeas lists ideas with stats.
func (s *Service) ListIdeas(ctx context.Context, f postgres.IdeaFilter) ([]domain.Idea, int, error) {
	ideas, total, err := s.store.ListIdeas(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]uuid.UUID, len(ideas))
	for i, it := range ideas {
		ids[i] = it.ID
	}
	stats, err := s.store.IdeaStats(ctx, f.UserID, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range ideas {
		ideas[i].Stats = stats[ideas[i].ID]
	}
	return ideas, total, nil
}

// UpdateIdeaInput patches an idea's identity. Every change creates an immutable IdeaVersion.
type UpdateIdeaInput struct {
	Title   *string  `json:"title,omitempty"`
	Summary *string  `json:"summary,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Status  *string  `json:"status,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

// UpdateIdea applies changes and records a new version.
func (s *Service) UpdateIdea(ctx context.Context, a Actor, id uuid.UUID, in UpdateIdeaInput) (*domain.Idea, error) {
	var out *domain.Idea
	err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		idea, err := tx.GetIdea(ctx, a.UserID, id)
		if err != nil {
			return err
		}
		var changed []string
		if in.Title != nil {
			t := strings.TrimSpace(*in.Title)
			if t == "" || len([]rune(t)) > 300 {
				return domain.Invalid("title", "title must be 1–300 characters")
			}
			if t != idea.Title {
				idea.Title = t
				changed = append(changed, "title")
			}
		}
		if in.Summary != nil && strings.TrimSpace(*in.Summary) != idea.Summary {
			idea.Summary = strings.TrimSpace(*in.Summary)
			changed = append(changed, "summary")
		}
		if in.Tags != nil {
			idea.Tags = normalizeTags(in.Tags)
			changed = append(changed, "tags")
		}
		prevStatus := idea.Status
		if in.Status != nil {
			st := domain.IdeaStatus(strings.ToUpper(strings.TrimSpace(*in.Status)))
			if !st.Valid() {
				return domain.Invalid("status", "unknown status")
			}
			if st == domain.StatusMerged {
				return domain.Invalid("status", "use the merge/conclude operation to merge ideas")
			}
			if !domain.CanTransition(idea.Status, st) {
				return domain.Invalid("status", fmt.Sprintf("cannot move from %s to %s", idea.Status, st))
			}
			if st != idea.Status {
				idea.Status = st
				changed = append(changed, "status")
				if st.IsOpen() {
					idea.ConcludedAt = nil
				}
			}
		}
		if len(changed) == 0 {
			out = idea
			return nil
		}
		idea.Version++
		if err := tx.UpdateIdea(ctx, idea); err != nil {
			return err
		}
		reason := in.Reason
		if reason == "" {
			reason = "Updated " + strings.Join(changed, ", ")
		}
		if err := tx.InsertIdeaVersion(ctx, &domain.IdeaVersion{IdeaID: idea.ID, Version: idea.Version, Title: idea.Title, Summary: idea.Summary,
			Status: idea.Status, Outcome: idea.Outcome, Tags: idea.Tags, ChangedFields: changed, ChangeReason: reason, CreatedBy: a.Kind}, a.AgentRunID); err != nil {
			return err
		}
		ev := "idea.updated"
		summary := reason
		if prevStatus != idea.Status {
			ev = "idea.status_changed"
			summary = fmt.Sprintf("Status %s → %s", prevStatus, idea.Status)
		}
		s.activity(ctx, tx, a, &idea.ID, nil, ev, domain.EntityIdea, &idea.ID, summary, map[string]any{"changed": changed})
		out = idea
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.enqueueEmbedding(ctx, a.UserID, domain.EntityIdea, id)
	return out, nil
}

// DeleteIdea permanently deletes an idea and all its history (DESTRUCTIVE; callers must confirm).
func (s *Service) DeleteIdea(ctx context.Context, a Actor, id uuid.UUID) error {
	return s.store.WithTx(ctx, func(tx *postgres.Store) error {
		idea, err := tx.GetIdea(ctx, a.UserID, id)
		if err != nil {
			return err
		}
		if err := tx.DeleteIdea(ctx, a.UserID, id); err != nil {
			return err
		}
		s.activity(ctx, tx, a, nil, nil, "idea.deleted", domain.EntityIdea, nil, "Deleted idea: "+idea.Title, map[string]any{"idea_id": id.String()})
		return nil
	})
}

// IdeaOverview is the data behind the Idea page.
type IdeaOverview struct {
	Idea           *domain.Idea                                    `json:"idea"`
	Branch         *domain.Branch                                  `json:"branch"`
	Branches       []domain.Branch                                 `json:"branches"`
	Checkpoints    []domain.Checkpoint                             `json:"checkpoints"`
	Knowledge      map[domain.KnowledgeKind][]domain.KnowledgeItem `json:"knowledge"`
	Proposed       []domain.KnowledgeItem                          `json:"proposed"`
	Relationships  []domain.Relationship                           `json:"relationships"`
	Artifacts      []domain.Artifact                               `json:"artifacts"`
	Conversations  []domain.Conversation                           `json:"conversations"`
	RecentActivity []domain.ActivityEvent                          `json:"recent_activity"`
	Conclusions    []domain.ConclusionRecord                       `json:"conclusions"`
	Inheritance    []domain.InheritanceRecord                      `json:"inheritance"`
	RelatedIdeas   []RelatedIdea                                   `json:"related_ideas"`
}

// RelatedIdea is an idea linked through the relationship graph.
type RelatedIdea struct {
	ID      uuid.UUID      `json:"id"`
	Title   string         `json:"title"`
	RelType domain.RelType `json:"rel_type"`
	Origin  domain.Origin  `json:"origin"`
}

// IdeaOverview assembles the idea page for a branch (default branch if nil).
func (s *Service) IdeaOverview(ctx context.Context, userID, ideaID uuid.UUID, branchID *uuid.UUID) (*IdeaOverview, error) {
	idea, err := s.GetIdea(ctx, userID, ideaID)
	if err != nil {
		return nil, err
	}
	br, err := s.resolveBranch(ctx, userID, idea, branchID)
	if err != nil {
		return nil, err
	}
	ov := &IdeaOverview{Idea: idea, Branch: br, Knowledge: map[domain.KnowledgeKind][]domain.KnowledgeItem{}}
	if ov.Branches, err = s.store.ListBranches(ctx, userID, ideaID); err != nil {
		return nil, err
	}
	if ov.Checkpoints, err = s.store.ListCheckpoints(ctx, userID, ideaID, nil); err != nil {
		return nil, err
	}
	items, err := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, BranchID: &br.ID, ReviewStates: []domain.ReviewState{domain.ReviewAccepted, domain.ReviewProposed}, Limit: 2000})
	if err != nil {
		return nil, err
	}
	for _, k := range items {
		if k.ReviewState == domain.ReviewProposed {
			ov.Proposed = append(ov.Proposed, k)
			continue
		}
		ov.Knowledge[k.Kind] = append(ov.Knowledge[k.Kind], k)
	}
	if ov.Relationships, err = s.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: userID, IdeaID: &ideaID, BranchID: &br.ID}); err != nil {
		return nil, err
	}
	if ov.Artifacts, err = s.store.ListArtifacts(ctx, postgres.ArtifactFilter{UserID: userID, IdeaID: &ideaID}); err != nil {
		return nil, err
	}
	if ov.Conversations, err = s.store.ListConversations(ctx, postgres.ConversationFilter{UserID: userID, IdeaID: &ideaID, Limit: 100}); err != nil {
		return nil, err
	}
	if ov.RecentActivity, err = s.store.ListActivity(ctx, postgres.ActivityFilter{UserID: userID, IdeaID: &ideaID, Limit: 30}); err != nil {
		return nil, err
	}
	if ov.Conclusions, err = s.store.ListConclusions(ctx, userID, ideaID); err != nil {
		return nil, err
	}
	if ov.Inheritance, err = s.store.ListInheritance(ctx, userID, br.ID); err != nil {
		return nil, err
	}
	rels, err := s.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: userID, EntityID: &ideaID})
	if err != nil {
		return nil, err
	}
	for _, r := range rels {
		if r.FromType != domain.EntityIdea || r.ToType != domain.EntityIdea {
			continue
		}
		other, label := r.ToID, r.ToLabel
		if r.ToID == ideaID {
			other, label = r.FromID, r.FromLabel
		}
		ov.RelatedIdeas = append(ov.RelatedIdeas, RelatedIdea{ID: other, Title: label, RelType: r.RelType, Origin: r.Origin})
	}
	return ov, nil
}

// LinkIdeas creates an idea↔idea relationship (similar_to, evolved_from, inspired_by, reuses_lesson_from...).
func (s *Service) LinkIdeas(ctx context.Context, a Actor, fromID, toID uuid.UUID, rel domain.RelType, rationale string, origin domain.Origin) (*domain.Relationship, error) {
	return s.CreateRelationship(ctx, a, CreateRelationshipInput{FromType: domain.EntityIdea, FromID: fromID, ToType: domain.EntityIdea, ToID: toID,
		RelType: rel, Rationale: rationale, Origin: origin})
}

// SuggestTitle produces a short title for idea text (LLM when available, else first line).
func (s *Service) SuggestTitle(ctx context.Context, userID uuid.UUID, text string) string {
	fallback := trimTo(firstLine(text), 70)
	if s.gw == nil || !s.gw.HasRealModel(models.TaskTitle) {
		return heuristicTitle(text, fallback)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := s.gw.Do(ctx, models.Call{Task: models.TaskTitle, UserID: &userID, Request: models.Request{
		System:    "You name ideas. Reply with a concise, specific title of 2-6 words in Title Case. No quotes, no punctuation at the end.",
		Messages:  []models.Message{{Role: models.RoleUser, Content: "<idea_text>\n" + trimTo(text, 3000) + "\n</idea_text>"}},
		MaxTokens: 400,
	}})
	if err != nil {
		return heuristicTitle(text, fallback)
	}
	t := strings.Trim(strings.TrimSpace(firstLine(res.Content)), `"'.`)
	if t == "" || len([]rune(t)) > 90 {
		return heuristicTitle(text, fallback)
	}
	return t
}

// heuristicTitle extracts a title from phrasing like "an idea for a biker community platform where…".
func heuristicTitle(text, fallback string) string {
	lower := strings.ToLower(text)
	for _, marker := range []string{"idea for a ", "idea for an ", "idea for ", "idea about ", "idea: ", "build a ", "build an ", "create a ", "create an "} {
		if i := strings.Index(lower, marker); i >= 0 {
			rest := text[i+len(marker):]
			for _, stop := range []string{" where ", " that ", " which ", " so ", " to help", ".", ",", "\n", " for people", " where"} {
				if j := strings.Index(strings.ToLower(rest), stop); j > 0 {
					rest = rest[:j]
				}
			}
			rest = strings.TrimSpace(rest)
			if n := len(strings.Fields(rest)); n >= 1 && n <= 8 {
				return titleCase(rest)
			}
		}
	}
	if fallback == "" {
		return "Untitled idea"
	}
	return fallback
}

func titleCase(s string) string {
	small := map[string]bool{"a": true, "an": true, "the": true, "for": true, "and": true, "of": true, "to": true, "in": true, "on": true, "with": true}
	words := strings.Fields(s)
	for i, w := range words {
		if i > 0 && small[strings.ToLower(w)] {
			words[i] = strings.ToLower(w)
			continue
		}
		r := []rune(w)
		if len(r) > 0 && r[0] >= 'a' && r[0] <= 'z' {
			r[0] -= 32
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
