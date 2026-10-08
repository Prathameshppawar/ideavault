package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// SearchHit is a unified full-text search result.
type SearchHit struct {
	EntityType     domain.EntityType `json:"entity_type"`
	EntityID       uuid.UUID         `json:"entity_id"`
	IdeaID         *uuid.UUID        `json:"idea_id,omitempty"`
	IdeaTitle      string            `json:"idea_title,omitempty"`
	BranchID       *uuid.UUID        `json:"branch_id,omitempty"`
	ConversationID *uuid.UUID        `json:"conversation_id,omitempty"`
	Title          string            `json:"title"`
	Snippet        string            `json:"snippet"`
	Label          string            `json:"label,omitempty"`
	Status         string            `json:"status,omitempty"`
	Role           string            `json:"role,omitempty"`
	Origin         string            `json:"origin,omitempty"`
	Untrusted      bool              `json:"untrusted,omitempty"`
	Rank           float64           `json:"rank"`
	CreatedAt      time.Time         `json:"created_at"`
}

// FTSQuery is a structured full-text query.
type FTSQuery struct {
	UserID   uuid.UUID
	Text     string
	Types    []domain.EntityType
	IdeaID   *uuid.UUID
	Statuses []string
	Since    *time.Time
	Until    *time.Time
	Order    string // relevance | oldest | newest
	Limit    int
}

func (q FTSQuery) wants(t domain.EntityType) bool {
	if len(q.Types) == 0 {
		return true
	}
	for _, x := range q.Types {
		if x == t {
			return true
		}
	}
	return false
}

const headlineOpts = `'StartSel=«,StopSel=»,MaxWords=28,MinWords=10,MaxFragments=2,FragmentDelimiter= … '`

// SearchFTS runs PostgreSQL full-text search across ideas, conversations, messages,
// knowledge items and artifacts.
func (s *Store) SearchFTS(ctx context.Context, q FTSQuery) ([]SearchHit, error) {
	text := strings.TrimSpace(q.Text)
	per := clampLimit(q.Limit, 20, 200)
	var hits []SearchHit
	order := "rank DESC, created_at DESC"
	switch q.Order {
	case "oldest":
		order = "created_at ASC"
	case "newest":
		order = "created_at DESC"
	}
	common := func(b *qb, alias, timeCol string) {
		b.add(alias+".user_id = ?", q.UserID)
		if q.Since != nil {
			b.add(alias+"."+timeCol+" >= ?", *q.Since)
		}
		if q.Until != nil {
			b.add(alias+"."+timeCol+" <= ?", *q.Until)
		}
	}
	tsq := func(b *qb) string { return fmt.Sprintf("websearch_to_tsquery('english', %s)", b.arg(text)) }

	run := func(sql string, args []any, et domain.EntityType) error {
		rows, err := s.q.Query(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("search %s: %w", et, err)
		}
		defer rows.Close()
		for rows.Next() {
			h := SearchHit{EntityType: et}
			var et2 string
			if err := rows.Scan(&et2, &h.EntityID, &h.IdeaID, &h.IdeaTitle, &h.BranchID, &h.ConversationID, &h.Title, &h.Snippet, &h.Label,
				&h.Status, &h.Role, &h.Origin, &h.Untrusted, &h.Rank, &h.CreatedAt); err != nil {
				return err
			}
			if et2 != "" {
				h.EntityType = domain.EntityType(et2)
			}
			hits = append(hits, h)
		}
		return rows.Err()
	}

	// Ideas
	if q.wants(domain.EntityIdea) {
		b := &qb{}
		common(b, "i", "created_at")
		rank, snippet := "0::float8", "left(i.summary, 240)"
		if text != "" {
			t := tsq(b)
			p := b.arg("%" + text + "%")
			b.where = append(b.where, fmt.Sprintf("(i.search_tsv @@ %s OR i.title ILIKE %s)", t, p))
			rank = fmt.Sprintf("ts_rank_cd(i.search_tsv, %s, 32)::float8 + CASE WHEN i.title ILIKE %s THEN 0.5 ELSE 0 END", t, p)
			snippet = fmt.Sprintf("ts_headline('english', i.title || '. ' || i.summary || ' ' || i.origin_text, %s, %s)", t, headlineOpts)
		}
		if q.IdeaID != nil {
			b.add("i.id = ?", *q.IdeaID)
		}
		if len(q.Statuses) > 0 {
			b.add("i.status = ANY(?)", q.Statuses)
		}
		sql := fmt.Sprintf(`SELECT 'idea', i.id, i.id, i.title, i.default_branch_id, NULL::uuid, i.title, %s, '', i.status, '', '', false, %s AS rank, i.created_at
			FROM ideas i%s ORDER BY %s LIMIT %d`, snippet, rank, b.clause(), order, per)
		if err := run(sql, b.args, domain.EntityIdea); err != nil {
			return nil, err
		}
	}

	// Knowledge items
	kinds := []string{}
	for _, k := range domain.AllKnowledgeKinds {
		if q.wants(k.EntityType()) {
			kinds = append(kinds, string(k))
		}
	}
	if len(kinds) > 0 {
		b := &qb{}
		common(b, "k", "created_at")
		b.add("k.kind = ANY(?)", kinds)
		b.add("k.review_state <> 'REJECTED'")
		rank, snippet := "0::float8", "left(k.statement, 240)"
		if text != "" {
			t := tsq(b)
			p := b.arg("%" + text + "%")
			b.where = append(b.where, fmt.Sprintf("(k.search_tsv @@ %s OR k.statement ILIKE %s)", t, p))
			rank = fmt.Sprintf("ts_rank_cd(k.search_tsv, %s, 32)::float8", t)
			snippet = fmt.Sprintf("ts_headline('english', k.statement || ' ' || k.details, %s, %s)", t, headlineOpts)
		}
		if q.IdeaID != nil {
			b.add("k.idea_id = ?", *q.IdeaID)
		}
		if len(q.Statuses) > 0 {
			b.add("k.status = ANY(?)", q.Statuses)
		}
		sql := fmt.Sprintf(`SELECT k.kind, k.id, k.idea_id, i.title, k.branch_id, k.source_conversation_id, k.statement, %s,
			(CASE k.kind WHEN 'decision' THEN 'D' WHEN 'assumption' THEN 'A' WHEN 'evidence' THEN 'E' WHEN 'insight' THEN 'I' WHEN 'question' THEN 'Q' ELSE 'T' END) || k.ref_number,
			k.status, '', k.origin, false, %s AS rank, k.created_at
			FROM knowledge_items k JOIN ideas i ON i.id = k.idea_id%s ORDER BY %s LIMIT %d`, snippet, rank, b.clause(), order, per)
		if err := run(sql, b.args, domain.EntityDecision); err != nil {
			return nil, err
		}
	}

	// Conversations
	if q.wants(domain.EntityConversation) {
		b := &qb{}
		common(b, "c", "started_at")
		rank, snippet := "0::float8", "left(c.summary, 240)"
		if text != "" {
			t := tsq(b)
			p := b.arg("%" + text + "%")
			b.where = append(b.where, fmt.Sprintf("(c.search_tsv @@ %s OR c.title ILIKE %s)", t, p))
			rank = fmt.Sprintf("ts_rank_cd(c.search_tsv, %s, 32)::float8", t)
		}
		if q.IdeaID != nil {
			b.add("c.idea_id = ?", *q.IdeaID)
		}
		sql := fmt.Sprintf(`SELECT 'conversation', c.id, c.idea_id, coalesce(i.title, ''), c.branch_id, c.id, c.title, %s, '', c.provider, '', c.origin,
			c.origin <> 'native', %s AS rank, c.started_at AS created_at
			FROM conversations c LEFT JOIN ideas i ON i.id = c.idea_id%s ORDER BY %s LIMIT %d`, snippet, rank, b.clause(), order, per)
		if err := run(sql, b.args, domain.EntityConversation); err != nil {
			return nil, err
		}
	}

	// Messages (rank inside, headline outside to bound ts_headline cost)
	if q.wants(domain.EntityMessage) && text != "" {
		b := &qb{}
		common(b, "m", "created_at")
		t := tsq(b)
		b.where = append(b.where, fmt.Sprintf("m.search_tsv @@ %s", t))
		b.add("m.role IN ('user','assistant')")
		if q.IdeaID != nil {
			b.add("c.idea_id = ?", *q.IdeaID)
		}
		sql := fmt.Sprintf(`SELECT 'message', x.id, x.idea_id, coalesce(i.title, ''), x.branch_id, x.conversation_id, x.ctitle,
			ts_headline('english', left(x.content, 20000), %s, %s), '#' || x.position, '', x.role, CASE WHEN x.untrusted THEN 'imported' ELSE 'native' END,
			x.untrusted, x.rank, x.created_at
			FROM (SELECT m.id, m.content, m.position, m.role, m.untrusted, m.created_at, m.conversation_id, c.idea_id, c.branch_id, c.title AS ctitle,
				ts_rank_cd(m.search_tsv, %s, 32)::float8 AS rank
				FROM messages m JOIN conversations c ON c.id = m.conversation_id%s ORDER BY %s LIMIT %d) x
			LEFT JOIN ideas i ON i.id = x.idea_id`, t, headlineOpts, t, b.clause(), order, per)
		if err := run(sql, b.args, domain.EntityMessage); err != nil {
			return nil, err
		}
	}

	// Artifacts
	if q.wants(domain.EntityArtifact) {
		b := &qb{}
		common(b, "a", "created_at")
		rank, snippet := "0::float8", "left(a.content_markdown, 240)"
		if text != "" {
			t := tsq(b)
			p := b.arg("%" + text + "%")
			b.where = append(b.where, fmt.Sprintf("(a.search_tsv @@ %s OR a.title ILIKE %s)", t, p))
			rank = fmt.Sprintf("ts_rank_cd(a.search_tsv, %s, 32)::float8", t)
			snippet = fmt.Sprintf("ts_headline('english', left(a.content_markdown, 20000), %s, %s)", t, headlineOpts)
		}
		if q.IdeaID != nil {
			b.add("a.idea_id = ?", *q.IdeaID)
		}
		sql := fmt.Sprintf(`SELECT 'artifact', a.id, a.idea_id, i.title, a.branch_id, NULL::uuid, a.title, %s, 'v' || a.current_version, a.status, a.type, '', false, %s AS rank, a.created_at
			FROM artifacts a JOIN ideas i ON i.id = a.idea_id%s ORDER BY %s LIMIT %d`, snippet, rank, b.clause(), order, per)
		if err := run(sql, b.args, domain.EntityArtifact); err != nil {
			return nil, err
		}
	}

	switch q.Order {
	case "oldest":
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].CreatedAt.Before(hits[j].CreatedAt) })
	case "newest":
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].CreatedAt.After(hits[j].CreatedAt) })
	default:
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Rank > hits[j].Rank })
	}
	return hits, nil
}

// ResolveEntities loads display metadata for semantic hits that FTS did not return.
func (s *Store) ResolveEntities(ctx context.Context, userID uuid.UUID, refs []SemanticHit) ([]SearchHit, error) {
	var out []SearchHit
	for _, r := range refs {
		h := SearchHit{EntityType: r.EntityType, EntityID: r.EntityID, IdeaID: r.IdeaID, Snippet: truncate(r.Content, 240), Rank: r.Score}
		var err error
		switch {
		case r.EntityType == domain.EntityIdea:
			err = s.q.QueryRow(ctx, `SELECT title, title, status, created_at, default_branch_id FROM ideas WHERE id = $1 AND user_id = $2`, r.EntityID, userID).
				Scan(&h.Title, &h.IdeaTitle, &h.Status, &h.CreatedAt, &h.BranchID)
		case r.EntityType.IsKnowledge():
			err = s.q.QueryRow(ctx, `SELECT k.statement, i.title, k.status, k.created_at, k.branch_id, k.origin,
				(CASE k.kind WHEN 'decision' THEN 'D' WHEN 'assumption' THEN 'A' WHEN 'evidence' THEN 'E' WHEN 'insight' THEN 'I' WHEN 'question' THEN 'Q' ELSE 'T' END) || k.ref_number
				FROM knowledge_items k JOIN ideas i ON i.id = k.idea_id WHERE k.id = $1 AND k.user_id = $2`, r.EntityID, userID).
				Scan(&h.Title, &h.IdeaTitle, &h.Status, &h.CreatedAt, &h.BranchID, &h.Origin, &h.Label)
		case r.EntityType == domain.EntityMessage:
			var cid uuid.UUID
			err = s.q.QueryRow(ctx, `SELECT c.title, coalesce(i.title, ''), m.role, m.created_at, c.id, '#' || m.position, m.untrusted
				FROM messages m JOIN conversations c ON c.id = m.conversation_id LEFT JOIN ideas i ON i.id = c.idea_id WHERE m.id = $1 AND m.user_id = $2`, r.EntityID, userID).
				Scan(&h.Title, &h.IdeaTitle, &h.Role, &h.CreatedAt, &cid, &h.Label, &h.Untrusted)
			h.ConversationID = &cid
		case r.EntityType == domain.EntityArtifact:
			err = s.q.QueryRow(ctx, `SELECT a.title, i.title, a.status, a.created_at, 'v' || a.current_version FROM artifacts a JOIN ideas i ON i.id = a.idea_id
				WHERE a.id = $1 AND a.user_id = $2`, r.EntityID, userID).Scan(&h.Title, &h.IdeaTitle, &h.Status, &h.CreatedAt, &h.Label)
		default:
			continue
		}
		if err != nil {
			continue // entity deleted since embedding; skip
		}
		out = append(out, h)
	}
	return out, nil
}
