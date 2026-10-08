package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

const branchCols = `b.id, b.user_id, b.idea_id, b.name, b.slug, b.description, b.status, b.is_default, b.parent_branch_id,
	b.forked_from_checkpoint_id, b.fork_mode, b.head_checkpoint_id, b.created_at, b.updated_at,
	(SELECT c.number FROM checkpoints c WHERE c.id = b.forked_from_checkpoint_id),
	(SELECT count(*) FROM checkpoints c WHERE c.branch_id = b.id),
	(SELECT count(*) FROM knowledge_items k WHERE k.branch_id = b.id AND k.review_state = 'ACCEPTED')`

func scanBranch(row pgx.Row) (*domain.Branch, error) {
	var b domain.Branch
	var mode *string
	err := row.Scan(&b.ID, &b.UserID, &b.IdeaID, &b.Name, &b.Slug, &b.Description, &b.Status, &b.IsDefault, &b.ParentBranchID,
		&b.ForkedFromCheckpointID, &mode, &b.HeadCheckpointID, &b.CreatedAt, &b.UpdatedAt,
		&b.ForkedFromCheckpointNumber, &b.CheckpointCount, &b.KnowledgeCount)
	if err != nil {
		return nil, err
	}
	if mode != nil {
		m := domain.ForkMode(*mode)
		b.ForkMode = &m
	}
	return &b, nil
}

// CreateBranch inserts a branch, de-duplicating its slug within the idea.
func (s *Store) CreateBranch(ctx context.Context, b *domain.Branch) error {
	base := domain.Slugify(b.Name)
	slug := base
	for i := 2; ; i++ {
		var exists bool
		if err := s.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM branches WHERE idea_id = $1 AND slug = $2)`, b.IdeaID, slug).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	b.Slug = slug
	if b.Status == "" {
		b.Status = domain.BranchActive
	}
	var mode *string
	if b.ForkMode != nil {
		m := string(*b.ForkMode)
		mode = &m
	}
	err := s.q.QueryRow(ctx, `
		INSERT INTO branches (user_id, idea_id, name, slug, description, status, is_default, parent_branch_id, forked_from_checkpoint_id, fork_mode)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at`,
		b.UserID, b.IdeaID, b.Name, b.Slug, b.Description, b.Status, b.IsDefault, b.ParentBranchID, b.ForkedFromCheckpointID, mode).
		Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)
	return mapErr(err, "branch")
}

// GetBranch fetches a branch owned by userID.
func (s *Store) GetBranch(ctx context.Context, userID, id uuid.UUID) (*domain.Branch, error) {
	b, err := scanBranch(s.q.QueryRow(ctx, `SELECT `+branchCols+` FROM branches b WHERE b.id = $1 AND b.user_id = $2`, id, userID))
	return b, mapErr(err, "branch")
}

// FindBranch resolves a branch within an idea by id, slug or (case-insensitive) name.
func (s *Store) FindBranch(ctx context.Context, userID, ideaID uuid.UUID, ref string) (*domain.Branch, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		return s.GetBranch(ctx, userID, id)
	}
	b, err := scanBranch(s.q.QueryRow(ctx, `SELECT `+branchCols+` FROM branches b
		WHERE b.idea_id = $1 AND b.user_id = $2 AND (b.slug = $3 OR lower(b.name) = lower($4) OR b.name ILIKE $5)
		ORDER BY (b.slug = $3) DESC, (lower(b.name) = lower($4)) DESC, b.created_at LIMIT 1`,
		ideaID, userID, domain.Slugify(ref), ref, "%"+ref+"%"))
	return b, mapErr(err, "branch")
}

// ListBranches lists an idea's branches oldest first.
func (s *Store) ListBranches(ctx context.Context, userID, ideaID uuid.UUID) ([]domain.Branch, error) {
	rows, err := s.q.Query(ctx, `SELECT `+branchCols+` FROM branches b WHERE b.idea_id = $1 AND b.user_id = $2 ORDER BY b.is_default DESC, b.created_at`, ideaID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Branch
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// UpdateBranch persists name/description/status.
func (s *Store) UpdateBranch(ctx context.Context, b *domain.Branch) error {
	tag, err := s.q.Exec(ctx, `UPDATE branches SET name = $3, description = $4, status = $5 WHERE id = $1 AND user_id = $2`,
		b.ID, b.UserID, b.Name, b.Description, b.Status)
	if err != nil {
		return mapErr(err, "branch")
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("branch")
	}
	return nil
}

// SetBranchHead records the latest checkpoint of a branch.
func (s *Store) SetBranchHead(ctx context.Context, branchID, checkpointID uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE branches SET head_checkpoint_id = $2 WHERE id = $1`, branchID, checkpointID)
	return err
}

// SetDefaultBranch marks a branch as the idea's default (main) branch.
func (s *Store) SetDefaultBranch(ctx context.Context, userID, ideaID, branchID uuid.UUID) error {
	if _, err := s.q.Exec(ctx, `UPDATE branches SET is_default = false WHERE idea_id = $1 AND user_id = $2 AND is_default AND id <> $3`, ideaID, userID, branchID); err != nil {
		return err
	}
	if _, err := s.q.Exec(ctx, `UPDATE branches SET is_default = true WHERE id = $1 AND user_id = $2`, branchID, userID); err != nil {
		return err
	}
	_, err := s.q.Exec(ctx, `UPDATE ideas SET default_branch_id = $2 WHERE id = $1 AND user_id = $3`, ideaID, branchID, userID)
	return err
}

// InsertInheritance records what a fork/merge inherited.
func (s *Store) InsertInheritance(ctx context.Context, r *domain.InheritanceRecord) error {
	return s.q.QueryRow(ctx, `
		INSERT INTO branch_inheritance (branch_id, entity_type, source_entity_id, new_entity_id, via, source_checkpoint_id, source_branch_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		r.BranchID, r.EntityType, r.SourceEntityID, r.NewEntityID, r.Via, r.SourceCheckpointID, r.SourceBranchID).
		Scan(&r.ID, &r.CreatedAt)
}

// ListInheritance returns the inheritance records of a branch with labels resolved.
func (s *Store) ListInheritance(ctx context.Context, userID, branchID uuid.UUID) ([]domain.InheritanceRecord, error) {
	rows, err := s.q.Query(ctx, `
		SELECT r.id, r.branch_id, r.entity_type, r.source_entity_id, r.new_entity_id, r.via, r.source_checkpoint_id, r.source_branch_id, r.created_at,
			coalesce(CASE WHEN k.id IS NOT NULL THEN
				(CASE k.kind WHEN 'decision' THEN 'D' WHEN 'assumption' THEN 'A' WHEN 'evidence' THEN 'E' WHEN 'insight' THEN 'I' WHEN 'question' THEN 'Q' ELSE 'T' END) || k.ref_number END, ''),
			coalesce(k.statement, cv.title, ar.title, src.title, '')
		FROM branch_inheritance r
		JOIN branches b ON b.id = r.branch_id
		LEFT JOIN knowledge_items k ON k.id = r.source_entity_id
		LEFT JOIN conversations cv ON cv.id = r.source_entity_id
		LEFT JOIN artifacts ar ON ar.id = r.source_entity_id
		LEFT JOIN sources src ON src.id = r.source_entity_id
		WHERE r.branch_id = $1 AND b.user_id = $2
		ORDER BY r.created_at, r.entity_type`, branchID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InheritanceRecord
	for rows.Next() {
		var r domain.InheritanceRecord
		if err := rows.Scan(&r.ID, &r.BranchID, &r.EntityType, &r.SourceEntityID, &r.NewEntityID, &r.Via, &r.SourceCheckpointID, &r.SourceBranchID, &r.CreatedAt, &r.Label, &r.Title); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LinkToBranch associates a shared entity (conversation/source/artifact) with a branch.
func (s *Store) LinkToBranch(ctx context.Context, branchID uuid.UUID, entityType domain.EntityType, entityID uuid.UUID, linkType string, cpID *uuid.UUID) error {
	_, err := s.q.Exec(ctx, `
		INSERT INTO branch_links (branch_id, entity_type, entity_id, link_type, source_checkpoint_id)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, branchID, entityType, entityID, linkType, cpID)
	return err
}

// BranchLinkedIDs returns entity ids of a type linked (not owned) to a branch.
func (s *Store) BranchLinkedIDs(ctx context.Context, branchID uuid.UUID, entityType domain.EntityType) ([]uuid.UUID, error) {
	rows, err := s.q.Query(ctx, `SELECT entity_id FROM branch_links WHERE branch_id = $1 AND entity_type = $2`, branchID, entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
