package service

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// SearchRequest is a natural-language search over the vault.
type SearchRequest struct {
	Query  string              `json:"query"`
	Types  []domain.EntityType `json:"types,omitempty"`
	IdeaID *uuid.UUID          `json:"idea_id,omitempty"`
	Limit  int                 `json:"limit,omitempty"`
	Mode   string              `json:"mode,omitempty"` // hybrid (default) | fts | semantic
	Order  string              `json:"order,omitempty"`
}

// QueryInterpretation explains how a natural-language query was understood.
type QueryInterpretation struct {
	Terms       string              `json:"terms"`
	Types       []domain.EntityType `json:"types,omitempty"`
	Statuses    []string            `json:"statuses,omitempty"`
	Order       string              `json:"order"`
	SimilarTo   string              `json:"similar_to,omitempty"`
	Explanation string              `json:"explanation"`
}

// SearchResponse holds fused results.
type SearchResponse struct {
	Interpretation QueryInterpretation  `json:"interpretation"`
	Results        []postgres.SearchHit `json:"results"`
	SimilarIdeas   []IdeaCandidate      `json:"similar_ideas,omitempty"`
	Semantic       bool                 `json:"semantic"`
}

var (
	reFirst    = regexp.MustCompile(`(?i)\b(first|earliest|originally|initially|began|start(ed)? (talking|thinking))\b`)
	reLatest   = regexp.MustCompile(`(?i)\b(latest|most recent|recently|last time|newest)\b`)
	reNeverVal = regexp.MustCompile(`(?i)\b(never|not( yet)?|un)[ -]?(been )?validated\b|\bunvalidated\b|\bunproven\b|\bnever tested\b`)
	reOpenQ    = regexp.MustCompile(`(?i)\b(open|unanswered|unresolved) questions?\b`)
	reReversed = regexp.MustCompile(`(?i)\b(reversed|overturned|changed|superseded) decisions?\b|\bdecisions? (we|i) (reversed|changed)\b`)
	reSimilar  = regexp.MustCompile(`(?i)\b(?:ideas? )?(?:similar to|like|related to)\s+(.+)$`)
	reFiller   = regexp.MustCompile(`(?i)\b(where did i|where have i|when did i|did i ever|find|show me|show|list|what|which|all|my|me|the|about|talk(ed|ing)?|think(ing)?|thought|first|earliest|latest|recent(ly)?|most|ever|have|has|been|never|not|yet|validated|unvalidated|related to|similar to|related|decisions?|assumptions?|evidence|insights?|questions?|actions?|ideas?|artifacts?|conversations?|messages?|open|unanswered|unresolved|reversed|changed|superseded|that|we|i|did|do|does|are|is|were|was|any|for|on|of|in|a|an|to|discuss(ed)?|mention(ed)?)\b`)
)

// InterpretQuery maps natural language to structured filters.
func InterpretQuery(q string) QueryInterpretation {
	in := QueryInterpretation{Order: "relevance"}
	lower := strings.ToLower(q)
	var why []string
	addType := func(t domain.EntityType) {
		for _, x := range in.Types {
			if x == t {
				return
			}
		}
		in.Types = append(in.Types, t)
	}
	if m := reSimilar.FindStringSubmatch(q); m != nil && regexp.MustCompile(`(?i)\bideas?\b`).MatchString(q) {
		in.SimilarTo = strings.Trim(strings.TrimSpace(m[1]), "?.!")
		addType(domain.EntityIdea)
		why = append(why, "looking for ideas similar to “"+in.SimilarTo+"”")
	}
	if reFirst.MatchString(q) {
		in.Order = "oldest"
		why = append(why, "earliest mentions first")
		addType(domain.EntityMessage)
		addType(domain.EntityConversation)
		addType(domain.EntityIdea)
	} else if reLatest.MatchString(q) {
		in.Order = "newest"
		why = append(why, "newest first")
	}
	if reNeverVal.MatchString(q) {
		addType(domain.EntityAssumption)
		in.Statuses = append(in.Statuses, "UNVALIDATED", "VALIDATING")
		why = append(why, "assumptions that are not validated")
	}
	if reOpenQ.MatchString(q) {
		addType(domain.EntityQuestion)
		in.Statuses = append(in.Statuses, "OPEN")
		why = append(why, "open questions")
	}
	if reReversed.MatchString(q) {
		addType(domain.EntityDecision)
		in.Statuses = append(in.Statuses, "SUPERSEDED", "REVERSED")
		why = append(why, "decisions that changed")
	}
	typeWords := []struct {
		re *regexp.Regexp
		t  domain.EntityType
	}{
		{regexp.MustCompile(`(?i)\bdecisions?\b|\bdecided\b`), domain.EntityDecision},
		{regexp.MustCompile(`(?i)\bassumptions?\b`), domain.EntityAssumption},
		{regexp.MustCompile(`(?i)\bevidence\b|\bresearch\b`), domain.EntityEvidence},
		{regexp.MustCompile(`(?i)\binsights?\b|\blessons?\b|\blearn(ed|ings?)\b`), domain.EntityInsight},
		{regexp.MustCompile(`(?i)\bquestions?\b`), domain.EntityQuestion},
		{regexp.MustCompile(`(?i)\bactions?\b|\btasks?\b|\bnext steps?\b|\btodos?\b`), domain.EntityAction},
		{regexp.MustCompile(`(?i)\bartifacts?\b|\bdocuments?\b|\bplans?\b|\bprompts?\b|\bspecs?\b`), domain.EntityArtifact},
		{regexp.MustCompile(`(?i)\bconversations?\b|\bchats?\b`), domain.EntityConversation},
	}
	if in.SimilarTo == "" {
		for _, tw := range typeWords {
			if tw.re.MatchString(q) {
				addType(tw.t)
			}
		}
	}
	if len(in.Types) > 0 && in.Order == "oldest" && len(in.Statuses) == 0 && in.SimilarTo == "" {
		// "where did I first talk about X" is about conversations; keep ideas/messages/conversations only.
		var keep []domain.EntityType
		for _, t := range in.Types {
			if t == domain.EntityMessage || t == domain.EntityConversation || t == domain.EntityIdea {
				keep = append(keep, t)
			}
		}
		if len(keep) > 0 {
			in.Types = keep
		}
	}
	terms := q
	if in.SimilarTo != "" {
		terms = in.SimilarTo
	} else {
		terms = reFiller.ReplaceAllString(lower, " ")
	}
	terms = strings.Join(strings.Fields(strings.Trim(terms, "?.!,")), " ")
	in.Terms = terms
	if len(why) == 0 {
		why = append(why, "relevance across the whole vault")
	}
	if terms != "" {
		why = append(why, "matching “"+terms+"”")
	}
	in.Explanation = strings.Join(why, "; ")
	return in
}

// Search runs natural-language hybrid search (full-text + semantic, reciprocal-rank fused).
func (s *Service) Search(ctx context.Context, userID uuid.UUID, r SearchRequest) (*SearchResponse, error) {
	q := strings.TrimSpace(r.Query)
	if q == "" && len(r.Types) == 0 {
		return nil, domain.Invalid("query", "query is required")
	}
	interp := InterpretQuery(q)
	if len(r.Types) > 0 {
		interp.Types = r.Types
	}
	if r.Order != "" {
		interp.Order = r.Order
	}
	limit := r.Limit
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	resp := &SearchResponse{Interpretation: interp}
	if interp.SimilarTo != "" {
		target := interp.SimilarTo
		if idea, err := s.ResolveIdea(ctx, userID, target); err == nil {
			target = idea.Title + ". " + idea.Summary + " " + idea.OriginText
			cands, err := s.FindIdeaCandidates(ctx, userID, target, 10)
			if err == nil {
				for _, c := range cands {
					if c.Idea.ID != idea.ID {
						resp.SimilarIdeas = append(resp.SimilarIdeas, c)
					}
				}
			}
		} else if cands, err := s.FindIdeaCandidates(ctx, userID, target, 10); err == nil {
			resp.SimilarIdeas = cands
		}
	}
	// Full-text
	var fts []postgres.SearchHit
	if r.Mode != "semantic" {
		var err error
		fts, err = s.store.SearchFTS(ctx, postgres.FTSQuery{UserID: userID, Text: interp.Terms, Types: interp.Types, IdeaID: r.IdeaID,
			Statuses: interp.Statuses, Order: interp.Order, Limit: limit})
		if err != nil {
			return nil, err
		}
		// If the interpreted terms were too narrow, fall back to the raw query.
		if len(fts) == 0 && interp.Terms != q && q != "" && len(interp.Statuses) == 0 {
			fts, _ = s.store.SearchFTS(ctx, postgres.FTSQuery{UserID: userID, Text: q, Types: interp.Types, IdeaID: r.IdeaID, Order: interp.Order, Limit: limit})
		}
	}
	// Semantic
	var sem []postgres.SearchHit
	if r.Mode != "fts" && interp.Terms != "" && s.gw != nil && s.gw.Embedder() != nil && len(interp.Statuses) == 0 {
		if vec, err := s.gw.EmbedQuery(ctx, &userID, interp.Terms); err == nil {
			types := interp.Types
			hits, err := s.store.SemanticSearch(ctx, userID, s.gw.Embedder().Model(), vec, types, r.IdeaID, limit)
			if err == nil {
				var keep []postgres.SemanticHit
				for _, h := range hits {
					if h.Score >= 0.25 {
						keep = append(keep, h)
					}
				}
				sem, _ = s.store.ResolveEntities(ctx, userID, keep)
				resp.Semantic = true
			}
		}
	}
	resp.Results = fuse(fts, sem, interp.Order, limit)
	return resp, nil
}

// fuse merges result lists with reciprocal rank fusion, keeping chronological order when requested.
func fuse(a, b []postgres.SearchHit, order string, limit int) []postgres.SearchHit {
	type acc struct {
		hit   postgres.SearchHit
		score float64
	}
	m := map[uuid.UUID]*acc{}
	add := func(list []postgres.SearchHit, w float64) {
		for i, h := range list {
			x, ok := m[h.EntityID]
			if !ok {
				x = &acc{hit: h}
				m[h.EntityID] = x
			}
			x.score += w / float64(60+i+1)
			if x.hit.Snippet == "" || (strings.Contains(h.Snippet, "«") && !strings.Contains(x.hit.Snippet, "«")) {
				x.hit.Snippet = h.Snippet
			}
		}
	}
	add(a, 1.0)
	add(b, 0.9)
	out := make([]postgres.SearchHit, 0, len(m))
	for _, x := range m {
		x.hit.Rank = x.score
		out = append(out, x.hit)
	}
	switch order {
	case "oldest":
		sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	case "newest":
		sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	default:
		sort.SliceStable(out, func(i, j int) bool { return out[i].Rank > out[j].Rank })
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
