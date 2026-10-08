package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

const ideaCols = `i.id, i.user_id, i.title, i.slug, i.origin_text, i.summary, i.status, i.outcome, i.outcome_note,
	i.tags, i.default_branch_id, i.origin_source_id, i.origin_conversation_id, i.merged_into_idea_id, i.version,
	i.created_at, i.updated_at, i.last_activity_at, i.concluded_at`

func scanIdea(row pgx.Row) (*domain.Idea, error) {
	var it domain.Idea
	var outcome *string
	err := row.Scan(&it.ID, &it.UserID, &it.Title, &it.Slug, &it.OriginText, &it.Summary, &it.Status, &outcome, &it.OutcomeNote,
		&it.Tags, &it.DefaultBranchID, &it.OriginSourceID, &it.OriginConversationID, &it.MergedIntoIdeaID, &it.Version,
		&it.CreatedAt, &it.UpdatedAt, &it.LastActivityAt, &it.ConcludedAt)
	if err != nil {
		return nil, err
	}
	if outcome != nil {
		o := domain.Outcome(*outcome)
		it.Outcome = &o
	}
	it.Tags = nonNilStrings(it.Tags)
	return &it, nil
}

// uniqueIdeaSlug returns base or base-N so (user_id, slug) stays unique.
func (s *Store) uniqueIdeaSlug(ctx context.Context, userID uuid.UUID, base string) (string, error) {
	slug := base
	for i := 2; i < 1000; i++ {
		var exists bool
		if err := s.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ideas WHERE user_id = $1 AND slug = $2)`, userID, slug).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	return base + "-" + uuid.NewString()[:8], nil
}

// CreateIdea inserts an idea; ID/slug/timestamps are filled in.
func (s *Store) CreateIdea(ctx context.Context, it *domain.Idea) error {
	slug, err := s.uniqueIdeaSlug(ctx, it.UserID, domain.Slugify(it.Title))
	if err != nil {
		return err
	}
	it.Slug = slug
	if it.Status == "" {
		it.Status = domain.StatusExploring
	}
	if it.Version == 0 {
		it.Version = 1
	}
	row := s.q.QueryRow(ctx, `
		INSERT INTO ideas (user_id, title, slug, origin_text, summary, status, tags, origin_source_id, origin_conversation_id, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+strings.ReplaceAll(ideaCols, "i.", ""),
		it.UserID, it.Title, it.Slug, it.OriginText, it.Summary, it.Status, nonNilStrings(it.Tags), it.OriginSourceID, it.OriginConversationID, it.Version)
	got, err := scanIdea(row)
	if err != nil {
		return mapErr(err, "idea")
	}
	*it = *got
	return nil
}

// GetIdea fetches an idea owned by userID.
func (s *Store) GetIdea(ctx context.Context, userID, id uuid.UUID) (*domain.Idea, error) {
	it, err := scanIdea(s.q.QueryRow(ctx, `SELECT `+ideaCols+` FROM ideas i WHERE i.id = $1 AND i.user_id = $2`, id, userID))
	return it, mapErr(err, "idea")
}

// GetIdeaBySlug fetches an idea by slug.
func (s *Store) GetIdeaBySlug(ctx context.Context, userID uuid.UUID, slug string) (*domain.Idea, error) {
	it, err := scanIdea(s.q.QueryRow(ctx, `SELECT `+ideaCols+` FROM ideas i WHERE i.slug = $1 AND i.user_id = $2`, slug, userID))
	return it, mapErr(err, "idea")
}

// IdeaFilter selects ideas for listing.
type IdeaFilter struct {
	UserID   uuid.UUID
	Statuses []domain.IdeaStatus
	Query    string
	Tag      string
	Sort     string // recent | created | title | momentum
	Limit    int
	Offset   int
}

// ListIdeas lists ideas and returns the total count matching the filter.
func (s *Store) ListIdeas(ctx context.Context, f IdeaFilter) ([]domain.Idea, int, error) {
	b := &qb{}
	b.add("i.user_id = ?", f.UserID)
	if len(f.Statuses) > 0 {
		ss := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			ss[i] = string(st)
		}
		b.add("i.status = ANY(?)", ss)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		b.add("(i.search_tsv @@ websearch_to_tsquery('english', ?) OR i.title ILIKE ?)", q, "%"+q+"%")
	}
	if f.Tag != "" {
		b.add("? = ANY(i.tags)", f.Tag)
	}
	order := "i.last_activity_at DESC"
	switch f.Sort {
	case "created":
		order = "i.created_at DESC"
	case "title":
		order = "lower(i.title) ASC"
	case "momentum":
		order = `(SELECT count(*) FROM activity_events a WHERE a.idea_id = i.id AND a.created_at > now() - interval '7 days') DESC, i.last_activity_at DESC`
	}
	var total int
	if err := s.q.QueryRow(ctx, `SELECT count(*) FROM ideas i`+b.clause(), b.args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := clampLimit(f.Limit, 50, 500)
	sql := `SELECT ` + ideaCols + ` FROM ideas i` + b.clause() + ` ORDER BY ` + order +
		fmt.Sprintf(` LIMIT %d OFFSET %d`, limit, max(f.Offset, 0))
	rows, err := s.q.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Idea
	for rows.Next() {
		it, err := scanIdea(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *it)
	}
	return out, total, rows.Err()
}

// UpdateIdea persists mutable identity fields (caller records an IdeaVersion).
func (s *Store) UpdateIdea(ctx context.Context, it *domain.Idea) error {
	var outcome *string
	if it.Outcome != nil {
		o := string(*it.Outcome)
		outcome = &o
	}
	tag, err := s.q.Exec(ctx, `
		UPDATE ideas SET title = $3, summary = $4, status = $5, outcome = $6, outcome_note = $7, tags = $8,
			default_branch_id = $9, merged_into_idea_id = $10, version = $11, concluded_at = $12, last_activity_at = now()
		WHERE id = $1 AND user_id = $2`,
		it.ID, it.UserID, it.Title, it.Summary, it.Status, outcome, it.OutcomeNote, nonNilStrings(it.Tags),
		it.DefaultBranchID, it.MergedIntoIdeaID, it.Version, it.ConcludedAt)
	if err != nil {
		return mapErr(err, "idea")
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("idea")
	}
	return nil
}

// SetIdeaOriginConversation links the originating conversation.
func (s *Store) SetIdeaOriginConversation(ctx context.Context, ideaID, convID uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE ideas SET origin_conversation_id = $2 WHERE id = $1 AND origin_conversation_id IS NULL`, ideaID, convID)
	return err
}

// TouchIdea bumps last_activity_at.
func (s *Store) TouchIdea(ctx context.Context, ideaID uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE ideas SET last_activity_at = now() WHERE id = $1`, ideaID)
	return err
}

// InsertIdeaVersion appends an immutable idea version.
func (s *Store) InsertIdeaVersion(ctx context.Context, v *domain.IdeaVersion, agentRunID *uuid.UUID) error {
	var outcome *string
	if v.Outcome != nil {
		o := string(*v.Outcome)
		outcome = &o
	}
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO idea_versions (idea_id, version, title, summary, status, outcome, tags, changed_fields, change_reason, created_by, agent_run_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id, created_at`,
		v.IdeaID, v.Version, v.Title, v.Summary, v.Status, outcome, nonNilStrings(v.Tags), nonNilStrings(v.ChangedFields),
		v.ChangeReason, v.CreatedBy, agentRunID).Scan(&v.ID, &v.CreatedAt), "idea version")
}

// ListIdeaVersions returns the idea's version history (oldest first).
func (s *Store) ListIdeaVersions(ctx context.Context, userID, ideaID uuid.UUID) ([]domain.IdeaVersion, error) {
	rows, err := s.q.Query(ctx, `
		SELECT v.id, v.idea_id, v.version, v.title, v.summary, v.status, v.outcome, v.tags, v.changed_fields, v.change_reason, v.created_by, v.created_at
		FROM idea_versions v JOIN ideas i ON i.id = v.idea_id
		WHERE v.idea_id = $1 AND i.user_id = $2 ORDER BY v.version`, ideaID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.IdeaVersion
	for rows.Next() {
		var v domain.IdeaVersion
		var outcome *string
		if err := rows.Scan(&v.ID, &v.IdeaID, &v.Version, &v.Title, &v.Summary, &v.Status, &outcome, &v.Tags, &v.ChangedFields, &v.ChangeReason, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		if outcome != nil {
			o := domain.Outcome(*outcome)
			v.Outcome = &o
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// IdeaStats computes aggregate counts for the given ideas (default-branch knowledge).
func (s *Store) IdeaStats(ctx context.Context, userID uuid.UUID, ideaIDs []uuid.UUID) (map[uuid.UUID]*domain.IdeaStats, error) {
	out := map[uuid.UUID]*domain.IdeaStats{}
	if len(ideaIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.Query(ctx, `
		SELECT i.id,
			(SELECT count(*) FROM branches b WHERE b.idea_id = i.id),
			(SELECT count(*) FROM checkpoints c WHERE c.idea_id = i.id),
			(SELECT count(*) FROM conversations cv WHERE cv.idea_id = i.id),
			coalesce(k.decisions, 0), coalesce(k.assumptions, 0), coalesce(k.evidence, 0), coalesce(k.insights, 0),
			coalesce(k.open_questions, 0), coalesce(k.open_actions, 0),
			(SELECT count(*) FROM artifacts a WHERE a.idea_id = i.id AND a.status <> 'ARCHIVED'),
			(SELECT count(*) FROM knowledge_items p WHERE p.idea_id = i.id AND p.review_state = 'PROPOSED'),
			(SELECT count(*) FROM activity_events e WHERE e.idea_id = i.id AND e.created_at > now() - interval '7 days')
		FROM ideas i
		LEFT JOIN LATERAL (
			SELECT count(*) FILTER (WHERE kind = 'decision' AND status = 'ACTIVE') AS decisions,
			       count(*) FILTER (WHERE kind = 'assumption' AND status IN ('UNVALIDATED','VALIDATING','VALIDATED')) AS assumptions,
			       count(*) FILTER (WHERE kind = 'evidence' AND status IN ('ACTIVE','DISPUTED')) AS evidence,
			       count(*) FILTER (WHERE kind = 'insight' AND status = 'ACTIVE') AS insights,
			       count(*) FILTER (WHERE kind = 'question' AND status = 'OPEN') AS open_questions,
			       count(*) FILTER (WHERE kind = 'action' AND status IN ('TODO','IN_PROGRESS')) AS open_actions
			FROM knowledge_items ki
			WHERE ki.branch_id = i.default_branch_id AND ki.review_state = 'ACCEPTED'
		) k ON true
		WHERE i.user_id = $1 AND i.id = ANY($2)`, userID, ideaIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		st := &domain.IdeaStats{}
		if err := rows.Scan(&id, &st.Branches, &st.Checkpoints, &st.Conversations, &st.Decisions, &st.Assumptions, &st.Evidence,
			&st.Insights, &st.OpenQuestions, &st.OpenActions, &st.Artifacts, &st.Proposed, &st.Momentum); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

// IdeaMatch is a candidate duplicate/similar idea with a lexical similarity score.
type IdeaMatch struct {
	Idea  domain.Idea `json:"idea"`
	Score float64     `json:"score"`
}

// FindSimilarIdeas ranks ideas by trigram title similarity and full-text relevance.
func (s *Store) FindSimilarIdeas(ctx context.Context, userID uuid.UUID, text string, limit int) ([]IdeaMatch, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	rows, err := s.q.Query(ctx, `
		WITH q AS (SELECT websearch_to_tsquery('english', $2) AS tsq, plainto_tsquery('english', $2) AS ptsq)
		SELECT `+ideaCols+`,
			greatest(similarity(i.title, $2), word_similarity(i.title, $2)) * 0.6
			+ least(ts_rank_cd(i.search_tsv, q.ptsq, 32) * 2, 0.4) AS score
		FROM ideas i, q
		WHERE i.user_id = $1 AND (i.title % $2 OR word_similarity(i.title, $2) > 0.3 OR i.search_tsv @@ q.ptsq OR i.search_tsv @@ q.tsq)
		ORDER BY score DESC LIMIT $3`, userID, text, clampLimit(limit, 5, 50))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IdeaMatch
	for rows.Next() {
		var it domain.Idea
		var outcome *string
		var score float64
		if err := rows.Scan(&it.ID, &it.UserID, &it.Title, &it.Slug, &it.OriginText, &it.Summary, &it.Status, &outcome, &it.OutcomeNote,
			&it.Tags, &it.DefaultBranchID, &it.OriginSourceID, &it.OriginConversationID, &it.MergedIntoIdeaID, &it.Version,
			&it.CreatedAt, &it.UpdatedAt, &it.LastActivityAt, &it.ConcludedAt, &score); err != nil {
			return nil, err
		}
		if outcome != nil {
			o := domain.Outcome(*outcome)
			it.Outcome = &o
		}
		out = append(out, IdeaMatch{Idea: it, Score: score})
	}
	return out, rows.Err()
}

// DeleteIdea permanently deletes an idea and its history. Must run in a transaction.
func (s *Store) DeleteIdea(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.AllowHistoryDelete(ctx); err != nil {
		return err
	}
	// Clear embeddings/relationships that reference the idea's entities polymorphically.
	if _, err := s.q.Exec(ctx, `DELETE FROM relationships WHERE user_id = $1 AND (idea_id = $2 OR (from_type = 'idea' AND from_id = $2) OR (to_type = 'idea' AND to_id = $2))`, userID, id); err != nil {
		return err
	}
	tag, err := s.q.Exec(ctx, `DELETE FROM ideas WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return mapErr(err, "idea")
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("idea")
	}
	return nil
}
