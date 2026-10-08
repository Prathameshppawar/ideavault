package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// CreateContextPack stores a context pack.
func (s *Store) CreateContextPack(ctx context.Context, p *domain.ContextPack) error {
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO context_packs (user_id, idea_id, branch_id, checkpoint_id, title, objective, content_markdown, content_json, token_estimate)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at`,
		p.UserID, p.IdeaID, p.BranchID, p.CheckpointID, p.Title, p.Objective, p.ContentMarkdown, toJSON(p.ContentJSON), p.TokenEstimate).
		Scan(&p.ID, &p.CreatedAt), "context pack")
}

// GetContextPack fetches a context pack.
func (s *Store) GetContextPack(ctx context.Context, userID, id uuid.UUID) (*domain.ContextPack, error) {
	var p domain.ContextPack
	var j []byte
	err := s.q.QueryRow(ctx, `SELECT id, user_id, idea_id, branch_id, checkpoint_id, title, objective, content_markdown, content_json, token_estimate, created_at
		FROM context_packs WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&p.ID, &p.UserID, &p.IdeaID, &p.BranchID, &p.CheckpointID, &p.Title, &p.Objective, &p.ContentMarkdown, &j, &p.TokenEstimate, &p.CreatedAt)
	if err != nil {
		return nil, mapErr(err, "context pack")
	}
	p.ContentJSON = jsonMap(j)
	return &p, nil
}

// ListContextPacks lists packs for an idea (or all), newest first, without heavy content.
func (s *Store) ListContextPacks(ctx context.Context, userID uuid.UUID, ideaID *uuid.UUID, limit int) ([]domain.ContextPack, error) {
	b := &qb{}
	b.add("user_id = ?", userID)
	if ideaID != nil {
		b.add("idea_id = ?", *ideaID)
	}
	rows, err := s.q.Query(ctx, `SELECT id, user_id, idea_id, branch_id, checkpoint_id, title, objective, token_estimate, created_at FROM context_packs`+
		b.clause()+fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, clampLimit(limit, 50, 500)), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ContextPack
	for rows.Next() {
		var p domain.ContextPack
		if err := rows.Scan(&p.ID, &p.UserID, &p.IdeaID, &p.BranchID, &p.CheckpointID, &p.Title, &p.Objective, &p.TokenEstimate, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateDelta stores a delta and its items.
func (s *Store) CreateDelta(ctx context.Context, userID uuid.UUID, d *domain.Delta) error {
	return s.WithTx(ctx, func(tx *Store) error {
		err := tx.q.QueryRow(ctx, `
			INSERT INTO deltas (user_id, idea_id, branch_id, conversation_id, source_id, context_pack_id, status, summary, analyzer)
			VALUES ($1,$2,$3,$4,$5,$6,'PENDING_REVIEW',$7,$8) RETURNING id, status, created_at`,
			userID, d.IdeaID, d.BranchID, d.ConversationID, d.SourceID, d.ContextPackID, d.Summary, d.Analyzer).
			Scan(&d.ID, &d.Status, &d.CreatedAt)
		if err != nil {
			return mapErr(err, "delta")
		}
		for i := range d.Items {
			it := &d.Items[i]
			it.DeltaID = d.ID
			it.Position = i
			if err := tx.q.QueryRow(ctx, `
				INSERT INTO delta_items (delta_id, classification, kind, statement, details, rationale, source_excerpt, target_item_id, confidence, selected, position)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
				d.ID, it.Classification, it.Kind, sanitizeText(it.Statement), sanitizeText(it.Details), sanitizeText(it.Rationale), sanitizeText(it.SourceExcerpt),
				it.TargetItemID, it.Confidence, it.Selected, it.Position).Scan(&it.ID); err != nil {
				return mapErr(err, "delta item")
			}
		}
		return nil
	})
}

// GetDelta fetches a delta with items (and target labels).
func (s *Store) GetDelta(ctx context.Context, userID, id uuid.UUID) (*domain.Delta, error) {
	var d domain.Delta
	err := s.q.QueryRow(ctx, `SELECT id, idea_id, branch_id, conversation_id, source_id, context_pack_id, status, summary, analyzer, merge_checkpoint_id, created_at, resolved_at
		FROM deltas WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&d.ID, &d.IdeaID, &d.BranchID, &d.ConversationID, &d.SourceID, &d.ContextPackID, &d.Status, &d.Summary, &d.Analyzer, &d.MergeCheckpointID, &d.CreatedAt, &d.ResolvedAt)
	if err != nil {
		return nil, mapErr(err, "delta")
	}
	rows, err := s.q.Query(ctx, `SELECT di.id, di.delta_id, di.classification, di.kind, di.statement, di.details, di.rationale, di.source_excerpt,
		di.target_item_id, di.confidence, di.selected, di.merged_item_id, di.position,
		coalesce((CASE k.kind WHEN 'decision' THEN 'D' WHEN 'assumption' THEN 'A' WHEN 'evidence' THEN 'E' WHEN 'insight' THEN 'I' WHEN 'question' THEN 'Q' WHEN 'action' THEN 'T' END) || k.ref_number, ''),
		coalesce(k.statement, '')
		FROM delta_items di LEFT JOIN knowledge_items k ON k.id = di.target_item_id WHERE di.delta_id = $1 ORDER BY di.position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it domain.DeltaItem
		if err := rows.Scan(&it.ID, &it.DeltaID, &it.Classification, &it.Kind, &it.Statement, &it.Details, &it.Rationale, &it.SourceExcerpt,
			&it.TargetItemID, &it.Confidence, &it.Selected, &it.MergedItemID, &it.Position, &it.TargetLabel, &it.TargetStatement); err != nil {
			return nil, err
		}
		d.Items = append(d.Items, it)
	}
	return &d, rows.Err()
}

// ListDeltas lists deltas for an idea.
func (s *Store) ListDeltas(ctx context.Context, userID uuid.UUID, ideaID *uuid.UUID) ([]domain.Delta, error) {
	b := &qb{}
	b.add("d.user_id = ?", userID)
	if ideaID != nil {
		b.add("d.idea_id = ?", *ideaID)
	}
	rows, err := s.q.Query(ctx, `SELECT d.id, d.idea_id, d.branch_id, d.conversation_id, d.source_id, d.context_pack_id, d.status, d.summary, d.analyzer,
		d.merge_checkpoint_id, d.created_at, d.resolved_at FROM deltas d`+b.clause()+` ORDER BY d.created_at DESC LIMIT 100`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Delta
	for rows.Next() {
		var d domain.Delta
		if err := rows.Scan(&d.ID, &d.IdeaID, &d.BranchID, &d.ConversationID, &d.SourceID, &d.ContextPackID, &d.Status, &d.Summary, &d.Analyzer,
			&d.MergeCheckpointID, &d.CreatedAt, &d.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ResolveDeltaItem records the merged item for a delta item.
func (s *Store) ResolveDeltaItem(ctx context.Context, itemID uuid.UUID, selected bool, mergedItemID *uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE delta_items SET selected = $2, merged_item_id = $3 WHERE id = $1`, itemID, selected, mergedItemID)
	return err
}

// ResolveDelta closes a delta.
func (s *Store) ResolveDelta(ctx context.Context, userID, id uuid.UUID, status string, cpID *uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE deltas SET status = $3, merge_checkpoint_id = $4, resolved_at = now() WHERE id = $1 AND user_id = $2`, id, userID, status, cpID)
	return err
}

// CreateAnalysis stores an analysis result.
func (s *Store) CreateAnalysis(ctx context.Context, userID uuid.UUID, a *domain.Analysis, agentRunID *uuid.UUID) error {
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO analyses (user_id, kind, idea_id, branch_id, subject_type, subject_id, input, result, analyzer, agent_run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id, created_at`,
		userID, a.Kind, a.IdeaID, a.BranchID, a.SubjectType, a.SubjectID, toJSON(a.Input), toJSON(a.Result), a.Analyzer, agentRunID).
		Scan(&a.ID, &a.CreatedAt), "analysis")
}

// ListAnalyses lists analyses by kind.
func (s *Store) ListAnalyses(ctx context.Context, userID uuid.UUID, kind string, ideaID *uuid.UUID, limit int) ([]domain.Analysis, error) {
	b := &qb{}
	b.add("user_id = ?", userID)
	if kind != "" {
		b.add("kind = ?", kind)
	}
	if ideaID != nil {
		b.add("idea_id = ?", *ideaID)
	}
	rows, err := s.q.Query(ctx, `SELECT id, kind, idea_id, branch_id, subject_type, subject_id, input, result, analyzer, created_at FROM analyses`+
		b.clause()+fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, clampLimit(limit, 20, 200)), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Analysis
	for rows.Next() {
		var a domain.Analysis
		var in, res []byte
		if err := rows.Scan(&a.ID, &a.Kind, &a.IdeaID, &a.BranchID, &a.SubjectType, &a.SubjectID, &in, &res, &a.Analyzer, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Input = jsonMap(in)
		a.Result = jsonMap(res)
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateConclusion records an idea conclusion.
func (s *Store) CreateConclusion(ctx context.Context, userID uuid.UUID, c *domain.ConclusionRecord) error {
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO idea_conclusions (user_id, idea_id, branch_id, checkpoint_id, outcome, note, learned, decisions, unresolved, previous_status, new_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id, created_at`,
		userID, c.IdeaID, c.BranchID, c.CheckpointID, c.Outcome, c.Note, c.Learned, c.Decisions, c.Unresolved, c.PreviousStatus, c.NewStatus).
		Scan(&c.ID, &c.CreatedAt), "conclusion")
}

// ListConclusions lists an idea's conclusions.
func (s *Store) ListConclusions(ctx context.Context, userID, ideaID uuid.UUID) ([]domain.ConclusionRecord, error) {
	rows, err := s.q.Query(ctx, `SELECT id, idea_id, branch_id, checkpoint_id, outcome, note, learned, decisions, unresolved, previous_status, new_status, created_at
		FROM idea_conclusions WHERE idea_id = $1 AND user_id = $2 ORDER BY created_at`, ideaID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ConclusionRecord
	for rows.Next() {
		var c domain.ConclusionRecord
		if err := rows.Scan(&c.ID, &c.IdeaID, &c.BranchID, &c.CheckpointID, &c.Outcome, &c.Note, &c.Learned, &c.Decisions, &c.Unresolved, &c.PreviousStatus, &c.NewStatus, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
