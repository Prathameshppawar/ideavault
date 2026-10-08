package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// TodayView is the "Thinking / Today" dashboard.
type TodayView struct {
	ActiveIdeas          []domain.Idea           `json:"active_ideas"`
	RecentChanges        []domain.ActivityEvent  `json:"recent_changes"`
	OpenQuestions        []domain.KnowledgeItem  `json:"open_questions"`
	RiskyAssumptions     []domain.KnowledgeItem  `json:"risky_assumptions"`
	OpenActions          []domain.KnowledgeItem  `json:"open_actions"`
	RecentDecisions      []domain.KnowledgeItem  `json:"recent_decisions"`
	RecentInsights       []domain.KnowledgeItem  `json:"recent_insights"`
	PendingProposals     int                     `json:"pending_proposals"`
	PendingConfirmations []domain.ToolCallRecord `json:"pending_confirmations"`
	Counts               map[string]int          `json:"counts"`
	IdeaTitles           map[string]string       `json:"idea_titles"`
}

// Today assembles open loops and recent thinking across all ideas.
func (s *Service) Today(ctx context.Context, userID uuid.UUID) (*TodayView, error) {
	v := &TodayView{Counts: map[string]int{}, IdeaTitles: map[string]string{}}
	open := []domain.IdeaStatus{domain.StatusExploring, domain.StatusActive, domain.StatusDecided, domain.StatusReadyToImplement}
	var err error
	if v.ActiveIdeas, _, err = s.ListIdeas(ctx, postgres.IdeaFilter{UserID: userID, Statuses: open, Limit: 8}); err != nil {
		return nil, err
	}
	since := time.Now().Add(-72 * time.Hour)
	if v.RecentChanges, err = s.store.ListActivity(ctx, postgres.ActivityFilter{UserID: userID, Since: &since, Limit: 30}); err != nil {
		return nil, err
	}
	live := func(kinds []domain.KnowledgeKind, statuses []string, limit int) []domain.KnowledgeItem {
		items, _ := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, Kinds: kinds, Statuses: statuses, LiveOnly: true, Limit: limit * 3})
		// De-duplicate lineages (forks copy items), preferring the original item over a
		// fork's copy whenever the original is still live; keep recency order.
		hasOriginal := map[uuid.UUID]bool{}
		for _, it := range items {
			if it.InheritedFromID == nil {
				hasOriginal[it.LineageID] = true
			}
		}
		seen := map[uuid.UUID]bool{}
		var out []domain.KnowledgeItem
		for _, it := range items {
			if seen[it.LineageID] || (it.InheritedFromID != nil && hasOriginal[it.LineageID]) {
				continue
			}
			seen[it.LineageID] = true
			out = append(out, it)
			if len(out) >= limit {
				break
			}
		}
		return out
	}
	v.OpenQuestions = live([]domain.KnowledgeKind{domain.KindQuestion}, []string{"OPEN"}, 8)
	risky := live([]domain.KnowledgeKind{domain.KindAssumption}, []string{"UNVALIDATED", "VALIDATING"}, 20)
	sort.SliceStable(risky, func(i, j int) bool { return riskOf(risky[i]) > riskOf(risky[j]) })
	if len(risky) > 8 {
		risky = risky[:8]
	}
	v.RiskyAssumptions = risky
	v.OpenActions = live([]domain.KnowledgeKind{domain.KindAction}, []string{"TODO", "IN_PROGRESS"}, 8)
	v.RecentDecisions = live([]domain.KnowledgeKind{domain.KindDecision}, nil, 6)
	v.RecentInsights = live([]domain.KnowledgeKind{domain.KindInsight}, nil, 6)
	props, _ := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, ReviewStates: []domain.ReviewState{domain.ReviewProposed}, Limit: 1000})
	v.PendingProposals = len(props)
	v.PendingConfirmations, _ = s.store.PendingConfirmations(ctx, userID)
	counts, _ := s.store.KnowledgeCounts(ctx, userID)
	for k, m := range counts {
		for _, n := range m {
			v.Counts[k] += n
		}
	}
	_, total, _ := s.store.ListIdeas(ctx, postgres.IdeaFilter{UserID: userID, Limit: 1})
	v.Counts["ideas"] = total
	v.Counts["checkpoints"], _ = s.store.CountCheckpoints(ctx, userID)
	for _, xs := range [][]domain.KnowledgeItem{v.OpenQuestions, v.RiskyAssumptions, v.OpenActions, v.RecentDecisions, v.RecentInsights} {
		for _, it := range xs {
			if _, ok := v.IdeaTitles[it.IdeaID.String()]; !ok {
				if idea, err := s.store.GetIdea(ctx, userID, it.IdeaID); err == nil {
					v.IdeaTitles[it.IdeaID.String()] = idea.Title
				}
			}
		}
	}
	return v, nil
}

func riskOf(it domain.KnowledgeItem) int {
	if it.Assumption == nil {
		return 0
	}
	return map[string]int{"LOW": 0, "MEDIUM": 1, "HIGH": 2}[it.Assumption.Risk]
}

// TimelineView is thinking activity over time.
type TimelineView struct {
	Buckets    []postgres.ActivityBucket `json:"buckets"`
	Milestones []domain.ActivityEvent    `json:"milestones"`
	From       time.Time                 `json:"from"`
	To         time.Time                 `json:"to"`
}

// Timeline returns daily activity buckets plus milestone events.
func (s *Service) Timeline(ctx context.Context, userID uuid.UUID, ideaID *uuid.UUID, days int) (*TimelineView, error) {
	if days <= 0 || days > 730 {
		days = 90
	}
	from := time.Now().AddDate(0, 0, -days)
	buckets, err := s.store.ActivityTimeline(ctx, userID, ideaID, from)
	if err != nil {
		return nil, err
	}
	ms, err := s.store.ListActivity(ctx, postgres.ActivityFilter{UserID: userID, IdeaID: ideaID, Since: &from, Limit: 500, OldestFirst: true,
		EventTypes: []string{"idea.created", "checkpoint.created", "branch.created", "decision.recorded", "decision.superseded", "decision.reversed", "idea.concluded", "artifact.created", "idea.status_changed", "delta.merged", "conversation.imported"}})
	if err != nil {
		return nil, err
	}
	return &TimelineView{Buckets: buckets, Milestones: ms, From: from, To: time.Now()}, nil
}

// DecisionsView lists decisions with their evolution.
type DecisionsView struct {
	Decisions     []DecisionEntry `json:"decisions"`
	Reversals     int             `json:"reversals"`
	Supersessions int             `json:"supersessions"`
}

// DecisionEntry is a decision with its idea and replacement.
type DecisionEntry struct {
	domain.KnowledgeItem
	IdeaTitle  string            `json:"idea_title"`
	BranchName string            `json:"branch_name"`
	ReplacedBy *domain.EntityRef `json:"replaced_by,omitempty"`
	// EvidenceCount is how much evidence is linked to the item.
	EvidenceCount int `json:"evidence_count"`
}

// Decisions returns decision history across ideas (or one idea), newest first.
func (s *Service) Decisions(ctx context.Context, userID uuid.UUID, ideaID *uuid.UUID) (*DecisionsView, error) {
	items, err := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, IdeaID: ideaID, Kinds: []domain.KnowledgeKind{domain.KindDecision},
		ReviewStates: []domain.ReviewState{domain.ReviewAccepted}, Limit: 2000})
	if err != nil {
		return nil, err
	}
	rels, _ := s.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: userID, IdeaID: ideaID, RelTypes: []domain.RelType{domain.RelSupports, domain.RelChallenges}, Limit: 5000})
	evCount := map[uuid.UUID]int{}
	for _, r := range rels {
		evCount[r.ToID]++
	}
	byID := map[uuid.UUID]domain.KnowledgeItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	titles := map[uuid.UUID]string{}
	branches := map[uuid.UUID]string{}
	v := &DecisionsView{}
	seenLineage := map[uuid.UUID]bool{}
	for _, it := range items {
		// Show inherited copies only once (their origin is already listed).
		if it.InheritedFromID != nil {
			if _, ok := byID[*it.InheritedFromID]; ok {
				continue
			}
		}
		if seenLineage[it.ID] {
			continue
		}
		seenLineage[it.ID] = true
		e := DecisionEntry{KnowledgeItem: it, EvidenceCount: evCount[it.ID]}
		if t, ok := titles[it.IdeaID]; ok {
			e.IdeaTitle = t
		} else if idea, err := s.store.GetIdea(ctx, userID, it.IdeaID); err == nil {
			titles[it.IdeaID] = idea.Title
			e.IdeaTitle = idea.Title
		}
		if n, ok := branches[it.BranchID]; ok {
			e.BranchName = n
		} else if br, err := s.store.GetBranch(ctx, userID, it.BranchID); err == nil {
			branches[it.BranchID] = br.Name
			e.BranchName = br.Name
		}
		if it.SupersededByID != nil {
			if nb, ok := byID[*it.SupersededByID]; ok {
				r := nb.Ref()
				e.ReplacedBy = &r
			}
			if it.Status == "REVERSED" {
				v.Reversals++
			} else {
				v.Supersessions++
			}
		}
		v.Decisions = append(v.Decisions, e)
	}
	return v, nil
}

// LearningView lists insights and reused lessons.
type LearningView struct {
	Insights    []DecisionEntry        `json:"insights"`
	Lessons     []domain.Relationship  `json:"reused_lessons"`
	Validated   []domain.KnowledgeItem `json:"validated_assumptions"`
	Invalidated []domain.KnowledgeItem `json:"invalidated_assumptions"`
}

// Learning returns insights and assumption outcomes across the vault.
func (s *Service) Learning(ctx context.Context, userID uuid.UUID) (*LearningView, error) {
	ins, err := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, Kinds: []domain.KnowledgeKind{domain.KindInsight}, ReviewStates: []domain.ReviewState{domain.ReviewAccepted}, Limit: 1000})
	if err != nil {
		return nil, err
	}
	v := &LearningView{}
	titles := map[uuid.UUID]string{}
	for _, it := range ins {
		if it.InheritedFromID != nil {
			continue
		}
		e := DecisionEntry{KnowledgeItem: it}
		if t, ok := titles[it.IdeaID]; ok {
			e.IdeaTitle = t
		} else if idea, err := s.store.GetIdea(ctx, userID, it.IdeaID); err == nil {
			titles[it.IdeaID] = idea.Title
			e.IdeaTitle = idea.Title
		}
		v.Insights = append(v.Insights, e)
	}
	v.Lessons, _ = s.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: userID, RelTypes: []domain.RelType{domain.RelReusesLessonFrom, domain.RelInspiredBy, domain.RelEvolvedFrom}})
	v.Validated, _ = s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, Kinds: []domain.KnowledgeKind{domain.KindAssumption}, Statuses: []string{"VALIDATED"}, ReviewStates: []domain.ReviewState{domain.ReviewAccepted}, Limit: 200})
	v.Invalidated, _ = s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, Kinds: []domain.KnowledgeKind{domain.KindAssumption}, Statuses: []string{"INVALIDATED"}, ReviewStates: []domain.ReviewState{domain.ReviewAccepted}, Limit: 200})
	return v, nil
}

// Momentum ranks ideas by recent change velocity.
func (s *Service) Momentum(ctx context.Context, userID uuid.UUID, days int) ([]postgres.IdeaMomentum, error) {
	if days <= 0 {
		days = 7
	}
	return s.store.Momentum(ctx, userID, time.Duration(days)*24*time.Hour, 30)
}

// ---------- Graph ----------

// GraphNode is a node in the universe/knowledge graph.
type GraphNode struct {
	ID     string            `json:"id"`
	Type   domain.EntityType `json:"type"`
	Label  string            `json:"label"`
	Title  string            `json:"title"`
	IdeaID string            `json:"idea_id,omitempty"`
	Status string            `json:"status,omitempty"`
	Weight int               `json:"weight"`
	Origin string            `json:"origin,omitempty"`
}

// GraphEdge is a typed edge.
type GraphEdge struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Type       string `json:"type"`
	Origin     string `json:"origin,omitempty"`
	Structural bool   `json:"structural"`
}

// Graph is a renderable graph.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphFilter scopes the graph.
type GraphFilter struct {
	IdeaID               *uuid.UUID
	BranchID             *uuid.UUID
	IncludeConversations bool
	IncludeArtifacts     bool
	IncludeHistory       bool // include superseded/rejected items
	Kinds                []domain.KnowledgeKind
}

// BuildGraph assembles nodes (ideas, branches, knowledge, conversations, artifacts) and edges.
func (s *Service) BuildGraph(ctx context.Context, userID uuid.UUID, f GraphFilter) (*Graph, error) {
	g := &Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	nodes := map[string]bool{}
	addNode := func(n GraphNode) {
		if !nodes[n.ID] {
			nodes[n.ID] = true
			g.Nodes = append(g.Nodes, n)
		}
	}
	addEdge := func(e GraphEdge) {
		if nodes[e.Source] && nodes[e.Target] {
			g.Edges = append(g.Edges, e)
		}
	}
	var ideas []domain.Idea
	if f.IdeaID != nil {
		idea, err := s.GetIdea(ctx, userID, *f.IdeaID)
		if err != nil {
			return nil, err
		}
		ideas = []domain.Idea{*idea}
	} else {
		var err error
		ideas, _, err = s.ListIdeas(ctx, postgres.IdeaFilter{UserID: userID, Limit: 300})
		if err != nil {
			return nil, err
		}
	}
	branchFilter := map[uuid.UUID]bool{}
	for _, idea := range ideas {
		w := 1
		if idea.Stats != nil {
			w = 1 + idea.Stats.Decisions + idea.Stats.Insights + idea.Stats.Checkpoints
		}
		addNode(GraphNode{ID: idea.ID.String(), Type: domain.EntityIdea, Label: idea.Title, Title: idea.Summary, IdeaID: idea.ID.String(), Status: string(idea.Status), Weight: w})
		brs, _ := s.store.ListBranches(ctx, userID, idea.ID)
		for _, b := range brs {
			if f.BranchID != nil && b.ID != *f.BranchID {
				continue
			}
			if f.IdeaID == nil && !b.IsDefault {
				continue // universe view: one branch per idea keeps the graph readable
			}
			branchFilter[b.ID] = true
			if f.IdeaID != nil {
				addNode(GraphNode{ID: b.ID.String(), Type: domain.EntityBranch, Label: b.Name, IdeaID: idea.ID.String(), Status: string(b.Status), Weight: 1 + b.KnowledgeCount/3})
				parent := idea.ID.String()
				if b.ParentBranchID != nil {
					parent = b.ParentBranchID.String()
				}
				addEdge(GraphEdge{ID: "br:" + b.ID.String(), Source: parent, Target: b.ID.String(), Type: "has_branch", Structural: true})
			}
		}
	}
	kf := postgres.KnowledgeFilter{UserID: userID, IdeaID: f.IdeaID, Kinds: f.Kinds, ReviewStates: []domain.ReviewState{domain.ReviewAccepted}, Limit: 3000}
	if !f.IncludeHistory {
		kf.LiveOnly = true
	}
	items, err := s.store.ListKnowledge(ctx, kf)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if !branchFilter[it.BranchID] {
			continue
		}
		addNode(GraphNode{ID: it.ID.String(), Type: it.Kind.EntityType(), Label: it.Label, Title: it.Statement, IdeaID: it.IdeaID.String(), Status: it.Status, Weight: 1, Origin: string(it.Origin)})
		parent := it.IdeaID.String()
		if f.IdeaID != nil {
			parent = it.BranchID.String()
		}
		addEdge(GraphEdge{ID: "k:" + it.ID.String(), Source: parent, Target: it.ID.String(), Type: "contains", Structural: true})
	}
	if f.IncludeConversations {
		convs, _ := s.store.ListConversations(ctx, postgres.ConversationFilter{UserID: userID, IdeaID: f.IdeaID, Limit: 300})
		for _, c := range convs {
			if c.IdeaID == nil {
				continue
			}
			addNode(GraphNode{ID: c.ID.String(), Type: domain.EntityConversation, Label: trimTo(c.Title, 40), Title: c.Title, IdeaID: c.IdeaID.String(), Status: string(c.Origin), Weight: 1})
			addEdge(GraphEdge{ID: "c:" + c.ID.String(), Source: c.IdeaID.String(), Target: c.ID.String(), Type: "discussed_in", Structural: true})
		}
	}
	if f.IncludeArtifacts {
		arts, _ := s.store.ListArtifacts(ctx, postgres.ArtifactFilter{UserID: userID, IdeaID: f.IdeaID, Limit: 300})
		for _, a := range arts {
			addNode(GraphNode{ID: a.ID.String(), Type: domain.EntityArtifact, Label: trimTo(a.Title, 40), Title: a.Type.HumanName(), IdeaID: a.IdeaID.String(), Status: string(a.Status), Weight: 1})
			addEdge(GraphEdge{ID: "a:" + a.ID.String(), Source: a.IdeaID.String(), Target: a.ID.String(), Type: "produced", Structural: true})
		}
	}
	rels, _ := s.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: userID, IdeaID: f.IdeaID, Limit: 5000})
	if f.IdeaID == nil {
		// also idea↔idea edges without idea scope
		extra, _ := s.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: userID, RelTypes: []domain.RelType{domain.RelSimilarTo, domain.RelEvolvedFrom, domain.RelInspiredBy, domain.RelReusesLessonFrom, domain.RelMergedInto, domain.RelRelatedTo}, Limit: 2000})
		rels = append(rels, extra...)
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range rels {
		if seen[r.ID] || r.RelType == domain.RelInheritedFrom {
			continue
		}
		seen[r.ID] = true
		addEdge(GraphEdge{ID: r.ID.String(), Source: r.FromID.String(), Target: r.ToID.String(), Type: string(r.RelType), Origin: string(r.Origin)})
	}
	return g, nil
}

// ---------- Journey ----------

// JourneyNode is one clickable step in an idea's journey.
type JourneyNode struct {
	ID             string             `json:"id"`
	Kind           string             `json:"kind"` // origin | checkpoint | branch | decision | artifact | conclusion | import | status
	Title          string             `json:"title"`
	At             time.Time          `json:"at"`
	BranchID       *uuid.UUID         `json:"branch_id,omitempty"`
	BranchName     string             `json:"branch_name,omitempty"`
	Source         *JourneySource     `json:"source,omitempty"`         // what actually happened / was said (SOURCE)
	Interpretation string             `json:"interpretation,omitempty"` // what IdeaVault inferred (INTERPRETATION)
	Prompt         string             `json:"prompt,omitempty"`         // the user's prompt that triggered it
	Refs           []domain.EntityRef `json:"refs,omitempty"`
	Provenance     map[string]any     `json:"provenance,omitempty"`
}

// JourneySource is verbatim source material behind a journey step.
type JourneySource struct {
	Excerpt        string     `json:"excerpt"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	MessageID      *uuid.UUID `json:"message_id,omitempty"`
	Untrusted      bool       `json:"untrusted"`
}

// Journey builds the visual journey of an idea: Origin → … → Current.
func (s *Service) Journey(ctx context.Context, userID, ideaID uuid.UUID) ([]JourneyNode, error) {
	idea, err := s.store.GetIdea(ctx, userID, ideaID)
	if err != nil {
		return nil, err
	}
	var nodes []JourneyNode
	origin := JourneyNode{ID: "origin", Kind: "origin", Title: "Origin", At: idea.CreatedAt, Interpretation: idea.Summary,
		Provenance: map[string]any{"idea_version": 1}}
	if idea.OriginText != "" {
		origin.Source = &JourneySource{Excerpt: trimTo(idea.OriginText, 2000), ConversationID: idea.OriginConversationID}
	}
	if idea.OriginConversationID != nil {
		if msgs, err := s.store.ListMessages(ctx, userID, *idea.OriginConversationID, 0, 2); err == nil && len(msgs) > 0 {
			if origin.Source == nil {
				origin.Source = &JourneySource{Excerpt: trimTo(msgs[0].Content, 2000), ConversationID: idea.OriginConversationID}
			}
			origin.Source.MessageID = &msgs[0].ID
			origin.Source.Untrusted = msgs[0].Untrusted
			origin.Prompt = trimTo(msgs[0].Content, 1200)
		}
	}
	nodes = append(nodes, origin)
	branches, _ := s.store.ListBranches(ctx, userID, ideaID)
	bname := map[uuid.UUID]string{}
	for _, b := range branches {
		bname[b.ID] = b.Name
	}
	cps, _ := s.store.ListCheckpoints(ctx, userID, ideaID, nil)
	for _, c := range cps {
		n := JourneyNode{ID: c.ID.String(), Kind: "checkpoint", Title: c.Label + " · " + c.Title, At: c.CreatedAt, BranchID: &c.BranchID, BranchName: c.BranchName,
			Interpretation: c.Summary, Provenance: map[string]any{"checkpoint_id": c.ID, "content_hash": c.ContentHash, "kind": c.Kind, "created_by": c.CreatedBy, "counts": c.Counts}}
		if c.Kind == domain.CheckpointConclusion {
			n.Kind = "conclusion"
		}
		n.Refs = append(n.Refs, domain.EntityRef{Type: domain.EntityCheckpoint, ID: c.ID, Label: c.Label, Title: c.Title})
		if full, err := s.store.GetCheckpoint(ctx, userID, c.ID); err == nil {
			for _, it := range full.Snapshot.Items {
				if it.Kind == domain.KindDecision && it.IsLive() {
					n.Refs = append(n.Refs, it.Ref())
				}
			}
			if full.Snapshot.Context != "" {
				n.Source = &JourneySource{Excerpt: full.Snapshot.Context}
			}
		}
		if c.AgentRunID != nil {
			n.Prompt = s.promptForRun(ctx, userID, *c.AgentRunID)
		}
		nodes = append(nodes, n)
	}
	for _, b := range branches {
		if b.IsDefault || b.ForkedFromCheckpointID == nil {
			continue
		}
		label := "Branch: " + b.Name
		if b.ForkedFromCheckpointNumber != nil {
			label += fmt.Sprintf(" (from CP%d)", *b.ForkedFromCheckpointNumber)
		}
		inh, _ := s.store.ListInheritance(ctx, userID, b.ID)
		selected := 0
		for _, r := range inh {
			if r.Via == "selection" {
				selected++
			}
		}
		n := JourneyNode{ID: b.ID.String(), Kind: "branch", Title: label, At: b.CreatedAt, BranchID: &b.ID, BranchName: b.Name, Interpretation: b.Description,
			Provenance: map[string]any{"forked_from_checkpoint_id": b.ForkedFromCheckpointID, "inherited": len(inh), "selected_from_later": selected, "fork_mode": b.ForkMode}}
		nodes = append(nodes, n)
	}
	// Key decisions (first recorded per lineage) with their source excerpts.
	decs, _ := s.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: userID, IdeaID: &ideaID, Kinds: []domain.KnowledgeKind{domain.KindDecision}, ReviewStates: []domain.ReviewState{domain.ReviewAccepted}, Limit: 200, OldestFirst: true})
	for _, d := range decs {
		if d.InheritedFromID != nil {
			continue
		}
		n := JourneyNode{ID: d.ID.String(), Kind: "decision", Title: d.Label + " · " + trimTo(d.Statement, 90), At: d.CreatedAt, BranchID: &d.BranchID, BranchName: bname[d.BranchID],
			Refs: []domain.EntityRef{d.Ref()}, Provenance: map[string]any{"origin": d.Origin, "created_by": d.CreatedBy, "status": d.Status}}
		if d.Origin == domain.OriginInterpretation {
			n.Interpretation = d.Statement
		}
		if d.SourceExcerpt != "" || d.SourceMessageID != nil {
			n.Source = &JourneySource{Excerpt: d.SourceExcerpt, ConversationID: d.SourceConversationID, MessageID: d.SourceMessageID}
			if d.SourceMessageID != nil {
				if m, err := s.store.GetMessage(ctx, userID, *d.SourceMessageID); err == nil {
					n.Source.Untrusted = m.Untrusted
				}
			}
		}
		if d.Decision != nil && d.Decision.Rationale != "" {
			n.Provenance["rationale"] = d.Decision.Rationale
		}
		if d.AgentRunID != nil {
			n.Prompt = s.promptForRun(ctx, userID, *d.AgentRunID)
		}
		nodes = append(nodes, n)
	}
	arts, _ := s.store.ListArtifacts(ctx, postgres.ArtifactFilter{UserID: userID, IdeaID: &ideaID})
	for _, a := range arts {
		nodes = append(nodes, JourneyNode{ID: a.ID.String(), Kind: "artifact", Title: a.Type.HumanName() + ": " + a.Title, At: a.CreatedAt, BranchID: a.BranchID,
			Refs:       []domain.EntityRef{{Type: domain.EntityArtifact, ID: a.ID, Label: fmt.Sprintf("v%d", a.CurrentVersion), Title: a.Title}},
			Provenance: map[string]any{"generator": a.Generator, "checkpoint": a.CheckpointLabel}})
	}
	imps, _ := s.store.ListActivity(ctx, postgres.ActivityFilter{UserID: userID, IdeaID: &ideaID, EventTypes: []string{"conversation.imported", "delta.merged", "idea.status_changed"}, Limit: 100, OldestFirst: true})
	for _, e := range imps {
		kind := "import"
		if e.EventType == "idea.status_changed" {
			kind = "status"
		}
		nodes = append(nodes, JourneyNode{ID: e.ID.String(), Kind: kind, Title: e.Summary, At: e.CreatedAt, BranchID: e.BranchID, Provenance: e.Metadata})
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].At.Before(nodes[j].At) })
	nodes = append(nodes, JourneyNode{ID: "current", Kind: "current", Title: "Now · " + strings.ReplaceAll(strings.ToLower(string(idea.Status)), "_", " "), At: time.Now(), Interpretation: idea.Summary})
	return nodes, nil
}

func (s *Service) promptForRun(ctx context.Context, userID, runID uuid.UUID) string {
	run, err := s.store.GetAgentRun(ctx, userID, runID)
	if err != nil || run.UserMessageID == nil {
		return ""
	}
	if m, err := s.store.GetMessage(ctx, userID, *run.UserMessageID); err == nil {
		return trimTo(m.Content, 1200)
	}
	return ""
}

// Archive lists inactive ideas (parked, abandoned, concluded, merged).
func (s *Service) Archive(ctx context.Context, userID uuid.UUID) ([]domain.Idea, error) {
	ideas, _, err := s.ListIdeas(ctx, postgres.IdeaFilter{UserID: userID, Statuses: []domain.IdeaStatus{domain.StatusParked, domain.StatusAbandoned, domain.StatusConcluded, domain.StatusMerged}, Limit: 500})
	return ideas, err
}
