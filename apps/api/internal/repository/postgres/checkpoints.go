package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

const cpCols = `c.id, c.user_id, c.idea_id, c.branch_id, c.number, c.title, c.summary, c.kind, c.parent_checkpoint_id,
	c.content_hash, c.created_by, c.agent_run_id, c.created_at, b.name`

func scanCheckpoint(row pgx.Row, withSnapshot bool) (*domain.Checkpoint, error) {
	var c domain.Checkpoint
	var snap []byte
	dest := []any{&c.ID, &c.UserID, &c.IdeaID, &c.BranchID, &c.Number, &c.Title, &c.Summary, &c.Kind, &c.ParentCheckpointID,
		&c.ContentHash, &c.CreatedBy, &c.AgentRunID, &c.CreatedAt, &c.BranchName}
	if withSnapshot {
		dest = append(dest, &snap)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	c.Label = fmt.Sprintf("CP%d", c.Number)
	if withSnapshot && len(snap) > 0 {
		var s domain.Snapshot
		if err := json.Unmarshal(snap, &s); err != nil {
			return nil, fmt.Errorf("decode snapshot: %w", err)
		}
		for i := range s.Items {
			s.Items[i].Label = s.Items[i].Kind.Label(s.Items[i].RefNumber)
		}
		c.Snapshot = &s
		c.Counts = s.CountByKind()
	}
	return &c, nil
}

// NextCheckpointNumber allocates CP numbers per idea; caller holds LockIdeaKnowledge.
func (s *Store) NextCheckpointNumber(ctx context.Context, ideaID uuid.UUID) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT coalesce(max(number), 0) + 1 FROM checkpoints WHERE idea_id = $1`, ideaID).Scan(&n)
	return n, err
}

// InsertCheckpoint stores an immutable checkpoint and its item index.
func (s *Store) InsertCheckpoint(ctx context.Context, c *domain.Checkpoint) error {
	snap, err := json.Marshal(c.Snapshot)
	if err != nil {
		return err
	}
	return s.WithTx(ctx, func(tx *Store) error {
		err := tx.q.QueryRow(ctx, `
			INSERT INTO checkpoints (user_id, idea_id, branch_id, number, title, summary, kind, parent_checkpoint_id, snapshot, content_hash, created_by, agent_run_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id, created_at`,
			c.UserID, c.IdeaID, c.BranchID, c.Number, c.Title, c.Summary, c.Kind, c.ParentCheckpointID, snap, c.ContentHash, c.CreatedBy, c.AgentRunID).
			Scan(&c.ID, &c.CreatedAt)
		if err != nil {
			return mapErr(err, "checkpoint")
		}
		c.Label = fmt.Sprintf("CP%d", c.Number)
		if c.Snapshot != nil && len(c.Snapshot.Items) > 0 {
			rows := make([][]any, 0, len(c.Snapshot.Items))
			for _, it := range c.Snapshot.Items {
				rows = append(rows, []any{c.ID, it.ID, string(it.Kind), it.LineageID, it.RefNumber, it.Status})
			}
			if _, err := tx.q.CopyFrom(ctx, pgx.Identifier{"checkpoint_items"},
				[]string{"checkpoint_id", "item_id", "kind", "lineage_id", "ref_number", "status"}, pgx.CopyFromRows(rows)); err != nil {
				return err
			}
		}
		return tx.SetBranchHead(ctx, c.BranchID, c.ID)
	})
}

// GetCheckpoint fetches a checkpoint with its snapshot.
func (s *Store) GetCheckpoint(ctx context.Context, userID, id uuid.UUID) (*domain.Checkpoint, error) {
	c, err := scanCheckpoint(s.q.QueryRow(ctx, `SELECT `+cpCols+`, c.snapshot FROM checkpoints c JOIN branches b ON b.id = c.branch_id
		WHERE c.id = $1 AND c.user_id = $2`, id, userID), true)
	return c, mapErr(err, "checkpoint")
}

// GetCheckpointByNumber fetches CP<n> of an idea with its snapshot.
func (s *Store) GetCheckpointByNumber(ctx context.Context, userID, ideaID uuid.UUID, number int) (*domain.Checkpoint, error) {
	c, err := scanCheckpoint(s.q.QueryRow(ctx, `SELECT `+cpCols+`, c.snapshot FROM checkpoints c JOIN branches b ON b.id = c.branch_id
		WHERE c.idea_id = $1 AND c.number = $2 AND c.user_id = $3`, ideaID, number, userID), true)
	return c, mapErr(err, "checkpoint")
}

// ListCheckpoints lists an idea's checkpoints (optionally for one branch) without snapshots but with counts.
func (s *Store) ListCheckpoints(ctx context.Context, userID, ideaID uuid.UUID, branchID *uuid.UUID) ([]domain.Checkpoint, error) {
	b := &qb{}
	b.add("c.user_id = ?", userID)
	b.add("c.idea_id = ?", ideaID)
	if branchID != nil {
		b.add("c.branch_id = ?", *branchID)
	}
	rows, err := s.q.Query(ctx, `SELECT `+cpCols+`,
		(SELECT jsonb_object_agg(kind, n) FROM (SELECT kind, count(*) n FROM checkpoint_items ci WHERE ci.checkpoint_id = c.id GROUP BY kind) t)
		FROM checkpoints c JOIN branches b ON b.id = c.branch_id`+b.clause()+` ORDER BY c.number`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Checkpoint
	for rows.Next() {
		var c domain.Checkpoint
		var counts []byte
		if err := rows.Scan(&c.ID, &c.UserID, &c.IdeaID, &c.BranchID, &c.Number, &c.Title, &c.Summary, &c.Kind, &c.ParentCheckpointID,
			&c.ContentHash, &c.CreatedBy, &c.AgentRunID, &c.CreatedAt, &c.BranchName, &counts); err != nil {
			return nil, err
		}
		c.Label = fmt.Sprintf("CP%d", c.Number)
		c.Counts = map[string]int{}
		if len(counts) > 0 {
			_ = json.Unmarshal(counts, &c.Counts)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LatestCheckpoint returns the most recent checkpoint of a branch, or NotFound.
func (s *Store) LatestCheckpoint(ctx context.Context, userID, branchID uuid.UUID) (*domain.Checkpoint, error) {
	c, err := scanCheckpoint(s.q.QueryRow(ctx, `SELECT `+cpCols+`, c.snapshot FROM checkpoints c JOIN branches b ON b.id = c.branch_id
		WHERE c.branch_id = $1 AND c.user_id = $2 ORDER BY c.number DESC LIMIT 1`, branchID, userID), true)
	return c, mapErr(err, "checkpoint")
}

// CountCheckpoints counts checkpoints by user (dashboard).
func (s *Store) CountCheckpoints(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT count(*) FROM checkpoints WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}
