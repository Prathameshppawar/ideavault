package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

const artifactCols = `a.id, a.user_id, a.idea_id, a.branch_id, a.checkpoint_id, a.type, a.title, a.content_markdown, a.status,
	a.current_version, a.generator, a.instructions, a.created_at, a.updated_at, coalesce(i.title, ''), coalesce(b.name, ''),
	coalesce('CP' || c.number, '')`

const artifactFrom = ` FROM artifacts a JOIN ideas i ON i.id = a.idea_id LEFT JOIN branches b ON b.id = a.branch_id LEFT JOIN checkpoints c ON c.id = a.checkpoint_id`

func scanArtifact(row pgx.Row) (*domain.Artifact, error) {
	var a domain.Artifact
	err := row.Scan(&a.ID, &a.UserID, &a.IdeaID, &a.BranchID, &a.CheckpointID, &a.Type, &a.Title, &a.ContentMarkdown, &a.Status,
		&a.CurrentVersion, &a.Generator, &a.Instructions, &a.CreatedAt, &a.UpdatedAt, &a.IdeaTitle, &a.BranchName, &a.CheckpointLabel)
	return &a, err
}

// CreateArtifact inserts an artifact and its first version with provenance.
func (s *Store) CreateArtifact(ctx context.Context, a *domain.Artifact, v *domain.ArtifactVersion, prov []domain.ProvenanceEntry) error {
	return s.WithTx(ctx, func(tx *Store) error {
		if a.Status == "" {
			a.Status = domain.ArtifactDraft
		}
		a.CurrentVersion = 1
		err := tx.q.QueryRow(ctx, `
			INSERT INTO artifacts (user_id, idea_id, branch_id, checkpoint_id, type, title, content_markdown, status, current_version, generator, instructions)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10) RETURNING id, created_at, updated_at`,
			a.UserID, a.IdeaID, a.BranchID, a.CheckpointID, a.Type, a.Title, sanitizeText(a.ContentMarkdown), a.Status, a.Generator, a.Instructions).
			Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
		if err != nil {
			return mapErr(err, "artifact")
		}
		v.ArtifactID = a.ID
		v.Version = 1
		return tx.insertArtifactVersion(ctx, v, prov)
	})
}

func (s *Store) insertArtifactVersion(ctx context.Context, v *domain.ArtifactVersion, prov []domain.ProvenanceEntry) error {
	err := s.q.QueryRow(ctx, `
		INSERT INTO artifact_versions (artifact_id, version, title, content_markdown, change_note, created_by, generator, source_checkpoint_id, agent_run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at`,
		v.ArtifactID, v.Version, v.Title, sanitizeText(v.ContentMarkdown), v.ChangeNote, v.CreatedBy, v.Generator, v.SourceCheckpointID, v.AgentRunID).
		Scan(&v.ID, &v.CreatedAt)
	if err != nil {
		return mapErr(err, "artifact version")
	}
	seen := map[string]bool{}
	for _, p := range prov {
		key := string(p.EntityType) + p.EntityID.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := s.q.Exec(ctx, `INSERT INTO artifact_provenance (artifact_version_id, entity_type, entity_id, label, role) VALUES ($1,$2,$3,$4,$5)`,
			v.ID, p.EntityType, p.EntityID, p.Label, p.Role); err != nil {
			return err
		}
	}
	return nil
}

// AddArtifactVersion appends a new immutable version and updates the artifact head.
func (s *Store) AddArtifactVersion(ctx context.Context, userID uuid.UUID, a *domain.Artifact, v *domain.ArtifactVersion, prov []domain.ProvenanceEntry) error {
	return s.WithTx(ctx, func(tx *Store) error {
		if err := tx.lockKey(ctx, "artifact", a.ID.String()); err != nil {
			return err
		}
		var cur int
		if err := tx.q.QueryRow(ctx, `SELECT current_version FROM artifacts WHERE id = $1 AND user_id = $2`, a.ID, userID).Scan(&cur); err != nil {
			return mapErr(err, "artifact")
		}
		v.ArtifactID = a.ID
		v.Version = cur + 1
		if err := tx.insertArtifactVersion(ctx, v, prov); err != nil {
			return err
		}
		a.CurrentVersion = v.Version
		a.Title = v.Title
		a.ContentMarkdown = v.ContentMarkdown
		_, err := tx.q.Exec(ctx, `UPDATE artifacts SET title = $3, content_markdown = $4, current_version = $5, generator = $6,
			checkpoint_id = coalesce($7, checkpoint_id) WHERE id = $1 AND user_id = $2`,
			a.ID, userID, v.Title, sanitizeText(v.ContentMarkdown), v.Version, v.Generator, v.SourceCheckpointID)
		return err
	})
}

// UpdateArtifactStatus sets DRAFT/FINAL/ARCHIVED.
func (s *Store) UpdateArtifactStatus(ctx context.Context, userID, id uuid.UUID, status domain.ArtifactStatus) error {
	tag, err := s.q.Exec(ctx, `UPDATE artifacts SET status = $3 WHERE id = $1 AND user_id = $2`, id, userID, status)
	if err != nil {
		return mapErr(err, "artifact")
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("artifact")
	}
	return nil
}

// GetArtifact fetches an artifact.
func (s *Store) GetArtifact(ctx context.Context, userID, id uuid.UUID) (*domain.Artifact, error) {
	a, err := scanArtifact(s.q.QueryRow(ctx, `SELECT `+artifactCols+artifactFrom+` WHERE a.id = $1 AND a.user_id = $2`, id, userID))
	return a, mapErr(err, "artifact")
}

// ArtifactFilter selects artifacts.
type ArtifactFilter struct {
	UserID   uuid.UUID
	IdeaID   *uuid.UUID
	BranchID *uuid.UUID
	Type     domain.ArtifactType
	Status   domain.ArtifactStatus
	Query    string
	Limit    int
}

// ListArtifacts lists artifacts newest first.
func (s *Store) ListArtifacts(ctx context.Context, f ArtifactFilter) ([]domain.Artifact, error) {
	b := &qb{}
	b.add("a.user_id = ?", f.UserID)
	if f.IdeaID != nil {
		b.add("a.idea_id = ?", *f.IdeaID)
	}
	if f.BranchID != nil {
		p := b.arg(*f.BranchID)
		b.where = append(b.where, fmt.Sprintf("(a.branch_id = %s OR a.id IN (SELECT entity_id FROM branch_links WHERE branch_id = %s AND entity_type = 'artifact'))", p, p))
	}
	if f.Type != "" {
		b.add("a.type = ?", f.Type)
	}
	if f.Status != "" {
		b.add("a.status = ?", f.Status)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		b.add("(a.search_tsv @@ websearch_to_tsquery('english', ?) OR a.title ILIKE ?)", q, "%"+q+"%")
	}
	rows, err := s.q.Query(ctx, `SELECT `+artifactCols+artifactFrom+b.clause()+fmt.Sprintf(` ORDER BY a.updated_at DESC LIMIT %d`, clampLimit(f.Limit, 100, 1000)), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		a.ContentMarkdown = truncate(a.ContentMarkdown, 400)
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ListArtifactVersions returns all versions newest first (content included).
func (s *Store) ListArtifactVersions(ctx context.Context, userID, artifactID uuid.UUID) ([]domain.ArtifactVersion, error) {
	rows, err := s.q.Query(ctx, `SELECT v.id, v.artifact_id, v.version, v.title, v.content_markdown, v.change_note, v.created_by, v.generator,
		v.source_checkpoint_id, v.agent_run_id, v.created_at
		FROM artifact_versions v JOIN artifacts a ON a.id = v.artifact_id WHERE v.artifact_id = $1 AND a.user_id = $2 ORDER BY v.version DESC`, artifactID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ArtifactVersion
	for rows.Next() {
		var v domain.ArtifactVersion
		if err := rows.Scan(&v.ID, &v.ArtifactID, &v.Version, &v.Title, &v.ContentMarkdown, &v.ChangeNote, &v.CreatedBy, &v.Generator,
			&v.SourceCheckpointID, &v.AgentRunID, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ArtifactProvenance returns the provenance of a specific version (0 = current) with live titles/statuses.
func (s *Store) ArtifactProvenance(ctx context.Context, userID, artifactID uuid.UUID, version int) ([]domain.ProvenanceEntry, error) {
	rows, err := s.q.Query(ctx, `
		SELECT p.entity_type, p.entity_id, p.label, p.role,
			coalesce(k.statement, c.title, cv.title, i.title, ''), coalesce(k.status, '')
		FROM artifact_provenance p
		JOIN artifact_versions v ON v.id = p.artifact_version_id
		JOIN artifacts a ON a.id = v.artifact_id
		LEFT JOIN knowledge_items k ON k.id = p.entity_id
		LEFT JOIN checkpoints c ON c.id = p.entity_id
		LEFT JOIN conversations cv ON cv.id = p.entity_id
		LEFT JOIN ideas i ON i.id = p.entity_id
		WHERE a.id = $1 AND a.user_id = $2 AND v.version = CASE WHEN $3 > 0 THEN $3 ELSE a.current_version END
		ORDER BY p.role, p.entity_type, p.label`, artifactID, userID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ProvenanceEntry
	for rows.Next() {
		var p domain.ProvenanceEntry
		if err := rows.Scan(&p.EntityType, &p.EntityID, &p.Label, &p.Role, &p.Title, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteArtifact permanently removes an artifact and its versions (destructive; requires a transaction).
func (s *Store) DeleteArtifact(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.AllowHistoryDelete(ctx); err != nil {
		return err
	}
	tag, err := s.q.Exec(ctx, `DELETE FROM artifacts WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("artifact")
	}
	return nil
}
