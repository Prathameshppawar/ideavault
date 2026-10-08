// Package contextengine builds the smallest useful context for a request about an
// idea: relevant decisions, evidence, insights, questions, contradictions, related
// ideas and message windows — ranked by relevance and trimmed to a token budget.
package contextengine

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// Request asks for context about an idea.
type Request struct {
	UserID       uuid.UUID
	IdeaID       uuid.UUID
	BranchID     *uuid.UUID
	CheckpointID *uuid.UUID
	Query        string // the user's request; drives relevance ranking
	// TokenBudget bounds the rendered context (default 6000).
	TokenBudget int
	// IncludeMessages adds relevant conversation windows.
	IncludeMessages bool
	// Full disables relevance trimming (context packs, artifacts).
	Full bool
}

// Context is the assembled, ranked context.
type Context struct {
	Idea               domain.Idea            `json:"idea"`
	Branch             *domain.Branch         `json:"branch,omitempty"`
	Checkpoint         *domain.Checkpoint     `json:"checkpoint,omitempty"`
	Origin             string                 `json:"origin"`
	Decisions          []domain.KnowledgeItem `json:"decisions"`
	Assumptions        []domain.KnowledgeItem `json:"assumptions"`
	Evidence           []domain.KnowledgeItem `json:"evidence"`
	Insights           []domain.KnowledgeItem `json:"insights"`
	Questions          []domain.KnowledgeItem `json:"questions"`
	Actions            []domain.KnowledgeItem `json:"actions"`
	History            []domain.KnowledgeItem `json:"history"` // superseded/reversed items (how thinking changed)
	Contradictions     []Contradiction        `json:"contradictions"`
	RelatedIdeas       []RelatedIdea          `json:"related_ideas"`
	RelevantMessages   []MessageWindow        `json:"relevant_messages"`
	RecentDevelopments []domain.ActivityEvent `json:"recent_developments"`
	Checkpoints        []domain.Checkpoint    `json:"checkpoints"`
	TokenEstimate      int                    `json:"token_estimate"`
	Trimmed            int                    `json:"trimmed"` // items dropped to fit the budget
}

// Contradiction is a known conflicting pair.
type Contradiction struct {
	A         string `json:"a"`
	B         string `json:"b"`
	Rationale string `json:"rationale"`
}

// RelatedIdea is another idea related through the graph or semantics.
type RelatedIdea struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Why   string    `json:"why"`
}

// MessageWindow is a few relevant messages from a conversation.
type MessageWindow struct {
	ConversationID uuid.UUID        `json:"conversation_id"`
	Title          string           `json:"title"`
	Untrusted      bool             `json:"untrusted"`
	Messages       []domain.Message `json:"messages"`
	Score          float64          `json:"score"`
}

// Engine builds contexts.
type Engine struct {
	store *postgres.Store
	gw    *models.Gateway
}

// New returns an engine.
func New(store *postgres.Store, gw *models.Gateway) *Engine { return &Engine{store: store, gw: gw} }

// Build assembles a context for the request.
func (e *Engine) Build(ctx context.Context, r Request) (*Context, error) {
	if r.TokenBudget <= 0 {
		r.TokenBudget = 6000
	}
	idea, err := e.store.GetIdea(ctx, r.UserID, r.IdeaID)
	if err != nil {
		return nil, err
	}
	out := &Context{Idea: *idea, Origin: idea.OriginText}
	var items []domain.KnowledgeItem
	var rels []domain.Relationship
	if r.CheckpointID != nil {
		cp, err := e.store.GetCheckpoint(ctx, r.UserID, *r.CheckpointID)
		if err != nil {
			return nil, err
		}
		if cp.IdeaID != idea.ID {
			return nil, domain.Invalid("checkpoint", "checkpoint belongs to another idea")
		}
		out.Checkpoint = cp
		items = cp.Snapshot.Items
		rels = cp.Snapshot.Relationships
		if br, err := e.store.GetBranch(ctx, r.UserID, cp.BranchID); err == nil {
			out.Branch = br
		}
	} else {
		bid := idea.DefaultBranchID
		if r.BranchID != nil {
			bid = r.BranchID
		}
		if bid == nil {
			return nil, domain.Conflict("idea has no branch")
		}
		br, err := e.store.GetBranch(ctx, r.UserID, *bid)
		if err != nil {
			return nil, err
		}
		if br.IdeaID != idea.ID {
			return nil, domain.Invalid("branch_id", "branch belongs to another idea")
		}
		out.Branch = br
		items, err = e.store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: r.UserID, BranchID: &br.ID, ReviewStates: []domain.ReviewState{domain.ReviewAccepted}, Limit: 5000})
		if err != nil {
			return nil, err
		}
		rels, err = e.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: r.UserID, IdeaID: &idea.ID, BranchID: &br.ID, Limit: 2000})
		if err != nil {
			return nil, err
		}
	}

	scores := e.relevance(ctx, r, idea.ID, items)
	ranked := rankItems(items, scores)
	labels := map[uuid.UUID]string{}
	for _, it := range items {
		labels[it.ID] = it.Label
	}
	for _, rel := range rels {
		if rel.RelType == domain.RelContradicts {
			out.Contradictions = append(out.Contradictions, Contradiction{A: firstNonEmpty(labels[rel.FromID], rel.FromLabel), B: firstNonEmpty(labels[rel.ToID], rel.ToLabel), Rationale: rel.Rationale})
		}
	}

	// Budget: reserve for header, then fill by rank.
	budget := r.TokenBudget - 300 - models.EstimateTokens(idea.Summary+idea.OriginText)
	used := 0
	for _, it := range ranked {
		cost := itemTokens(it)
		if !r.Full && used+cost > budget*7/10 {
			out.Trimmed++
			continue
		}
		used += cost
		if !it.IsLive() {
			out.History = append(out.History, it)
			continue
		}
		switch it.Kind {
		case domain.KindDecision:
			out.Decisions = append(out.Decisions, it)
		case domain.KindAssumption:
			out.Assumptions = append(out.Assumptions, it)
		case domain.KindEvidence:
			out.Evidence = append(out.Evidence, it)
		case domain.KindInsight:
			out.Insights = append(out.Insights, it)
		case domain.KindQuestion:
			out.Questions = append(out.Questions, it)
		case domain.KindAction:
			out.Actions = append(out.Actions, it)
		}
	}
	for _, xs := range [][]domain.KnowledgeItem{out.Decisions, out.Assumptions, out.Evidence, out.Insights, out.Questions, out.Actions, out.History} {
		sort.SliceStable(xs, func(i, j int) bool { return xs[i].RefNumber < xs[j].RefNumber })
	}

	// Related ideas: explicit graph edges first, then semantic neighbours.
	if graph, err := e.store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: r.UserID, EntityID: &idea.ID, Limit: 50}); err == nil {
		for _, rel := range graph {
			if rel.FromType == domain.EntityIdea && rel.ToType == domain.EntityIdea {
				id, title := rel.ToID, rel.ToLabel
				if id == idea.ID {
					id, title = rel.FromID, rel.FromLabel
				}
				out.RelatedIdeas = append(out.RelatedIdeas, RelatedIdea{ID: id, Title: title, Why: string(rel.RelType)})
			}
		}
	}
	if len(out.RelatedIdeas) < 3 && e.gw != nil && e.gw.Embedder() != nil {
		if vec, err := e.gw.EmbedQuery(ctx, &r.UserID, idea.Title+". "+idea.Summary); err == nil {
			hits, _ := e.store.SemanticSearch(ctx, r.UserID, e.gw.Embedder().Model(), vec, []domain.EntityType{domain.EntityIdea}, nil, 5)
			for _, h := range hits {
				if h.EntityID == idea.ID || h.Score < 0.55 || containsIdea(out.RelatedIdeas, h.EntityID) {
					continue
				}
				if other, err := e.store.GetIdea(ctx, r.UserID, h.EntityID); err == nil {
					out.RelatedIdeas = append(out.RelatedIdeas, RelatedIdea{ID: other.ID, Title: other.Title, Why: fmt.Sprintf("semantically similar (%.2f)", h.Score)})
				}
			}
		}
	}

	if r.IncludeMessages && strings.TrimSpace(r.Query) != "" {
		out.RelevantMessages = e.messageWindows(ctx, r, idea.ID, budget-used)
	}
	since := time.Now().Add(-14 * 24 * time.Hour)
	if out.Checkpoint == nil && out.Branch != nil {
		out.RecentDevelopments, _ = e.store.ListActivity(ctx, postgres.ActivityFilter{UserID: r.UserID, IdeaID: &idea.ID, Since: &since, Limit: 12})
	}
	out.Checkpoints, _ = e.store.ListCheckpoints(ctx, r.UserID, idea.ID, nil)
	out.TokenEstimate = models.EstimateTokens(out.Render())
	return out, nil
}

func containsIdea(xs []RelatedIdea, id uuid.UUID) bool {
	for _, x := range xs {
		if x.ID == id {
			return true
		}
	}
	return false
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// relevance scores items for the query: semantic similarity (if available) + lexical overlap.
func (e *Engine) relevance(ctx context.Context, r Request, ideaID uuid.UUID, items []domain.KnowledgeItem) map[uuid.UUID]float64 {
	scores := map[uuid.UUID]float64{}
	q := strings.TrimSpace(r.Query)
	if q == "" {
		return scores
	}
	qt := tokenSet(q)
	for _, it := range items {
		scores[it.ID] = overlap(qt, tokenSet(it.Statement+" "+it.Details)) * 0.6
	}
	if e.gw != nil && e.gw.Embedder() != nil {
		vec, err := e.gw.EmbedQuery(ctx, &r.UserID, q)
		if err == nil {
			var kinds []domain.EntityType
			for _, k := range domain.AllKnowledgeKinds {
				kinds = append(kinds, k.EntityType())
			}
			hits, err := e.store.SemanticSearch(ctx, r.UserID, e.gw.Embedder().Model(), vec, kinds, &ideaID, 200)
			if err == nil {
				// Map by lineage too: snapshot/forked copies share content with embedded originals.
				byID := map[uuid.UUID]float64{}
				for _, h := range hits {
					byID[h.EntityID] = math.Max(byID[h.EntityID], h.Score)
				}
				for _, it := range items {
					s := byID[it.ID]
					if it.InheritedFromID != nil {
						s = math.Max(s, byID[*it.InheritedFromID])
					}
					s = math.Max(s, byID[it.LineageID])
					scores[it.ID] += s
				}
			}
		}
	}
	return scores
}

func (e *Engine) messageWindows(ctx context.Context, r Request, ideaID uuid.UUID, tokenRoom int) []MessageWindow {
	if tokenRoom < 300 {
		return nil
	}
	var wins []MessageWindow
	seen := map[uuid.UUID]bool{}
	add := func(m *domain.Message, score float64) {
		if seen[m.ID] || len(wins) >= 4 {
			return
		}
		win, err := e.store.MessageWindow(ctx, r.UserID, m.ConversationID, m.Position, 1)
		if err != nil {
			return
		}
		conv, _ := e.store.GetConversation(ctx, r.UserID, m.ConversationID)
		mw := MessageWindow{ConversationID: m.ConversationID, Untrusted: m.Untrusted, Score: score}
		if conv != nil {
			mw.Title = conv.Title
		}
		for _, wm := range win {
			seen[wm.ID] = true
			wm.Content = trim(wm.Content, 1200)
			mw.Messages = append(mw.Messages, wm)
			tokenRoom -= models.EstimateTokens(wm.Content)
		}
		if tokenRoom > 0 {
			wins = append(wins, mw)
		}
	}
	if e.gw != nil && e.gw.Embedder() != nil {
		if vec, err := e.gw.EmbedQuery(ctx, &r.UserID, r.Query); err == nil {
			hits, _ := e.store.SemanticSearch(ctx, r.UserID, e.gw.Embedder().Model(), vec, []domain.EntityType{domain.EntityMessage}, &ideaID, 8)
			for _, h := range hits {
				if h.Score < 0.35 {
					continue
				}
				if m, err := e.store.GetMessage(ctx, r.UserID, h.EntityID); err == nil {
					add(m, h.Score)
				}
			}
		}
	}
	if len(wins) == 0 {
		hits, _ := e.store.SearchFTS(ctx, postgres.FTSQuery{UserID: r.UserID, Text: keywords(r.Query), Types: []domain.EntityType{domain.EntityMessage}, IdeaID: &ideaID, Limit: 4})
		for _, h := range hits {
			if m, err := e.store.GetMessage(ctx, r.UserID, h.EntityID); err == nil {
				add(m, h.Rank)
			}
		}
	}
	return wins
}

func rankItems(items []domain.KnowledgeItem, scores map[uuid.UUID]float64) []domain.KnowledgeItem {
	out := append([]domain.KnowledgeItem(nil), items...)
	now := time.Now()
	weight := func(it domain.KnowledgeItem) float64 {
		w := scores[it.ID] * 3
		if it.IsLive() {
			w += 1
		}
		switch it.Kind {
		case domain.KindDecision:
			w += 0.8
		case domain.KindQuestion, domain.KindAssumption:
			w += 0.5
		case domain.KindInsight:
			w += 0.4
		}
		age := now.Sub(it.CreatedAt).Hours() / 24
		w += 0.5 / (1 + age/14)
		return w
	}
	sort.SliceStable(out, func(i, j int) bool { return weight(out[i]) > weight(out[j]) })
	return out
}

func itemTokens(it domain.KnowledgeItem) int {
	n := models.EstimateTokens(it.Statement) + 12
	if it.Decision != nil {
		n += models.EstimateTokens(it.Decision.Rationale)
	}
	return n
}

var stop = map[string]bool{"the": true, "a": true, "an": true, "and": true, "or": true, "of": true, "to": true, "in": true, "on": true, "for": true,
	"is": true, "are": true, "was": true, "we": true, "i": true, "it": true, "this": true, "that": true, "with": true, "why": true, "what": true,
	"did": true, "do": true, "does": true, "how": true, "be": true, "my": true, "our": true, "about": true, "from": true, "at": true, "as": true}

func tokenSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) > 2 && !stop[w] {
			out[strings.TrimSuffix(w, "s")] = true
		}
	}
	return out
}

func overlap(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return float64(n) / float64(len(a))
}

func keywords(q string) string {
	var ws []string
	for w := range tokenSet(q) {
		ws = append(ws, w)
	}
	sort.Strings(ws)
	return strings.Join(ws, " OR ")
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
