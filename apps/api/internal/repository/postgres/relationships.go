package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

const relCols = `r.id, r.from_type, r.from_id, r.to_type, r.to_id, r.rel_type, r.idea_id, r.branch_id, r.origin, r.confidence,
	r.rationale, r.source_message_id, r.created_by, r.agent_run_id, r.created_at`

// labelExpr renders a short human label for a polymorphic entity reference.
func labelExpr(typeCol, idCol string) string {
	return fmt.Sprintf(`coalesce(
		(SELECT (CASE k.kind WHEN 'decision' THEN 'D' WHEN 'assumption' THEN 'A' WHEN 'evidence' THEN 'E' WHEN 'insight' THEN 'I' WHEN 'question' THEN 'Q' ELSE 'T' END) || k.ref_number || ' ' || left(k.statement, 80)
			FROM knowledge_items k WHERE %[1]s IN ('decision','assumption','evidence','insight','question','action') AND k.id = %[2]s),
		(SELECT left(i.title, 80) FROM ideas i WHERE %[1]s = 'idea' AND i.id = %[2]s),
		(SELECT 'CP' || c.number || ' ' || left(c.title, 60) FROM checkpoints c WHERE %[1]s = 'checkpoint' AND c.id = %[2]s),
		(SELECT left(a.title, 80) FROM artifacts a WHERE %[1]s = 'artifact' AND a.id = %[2]s),
		(SELECT left(b.name, 80) FROM branches b WHERE %[1]s = 'branch' AND b.id = %[2]s),
		(SELECT left(cv.title, 80) FROM conversations cv WHERE %[1]s = 'conversation' AND cv.id = %[2]s),
		'')`, typeCol, idCol)
}

func scanRel(row pgx.Row, withLabels bool) (*domain.Relationship, error) {
	var r domain.Relationship
	dest := []any{&r.ID, &r.FromType, &r.FromID, &r.ToType, &r.ToID, &r.RelType, &r.IdeaID, &r.BranchID, &r.Origin, &r.Confidence,
		&r.Rationale, &r.SourceMessageID, &r.CreatedBy, &r.AgentRunID, &r.CreatedAt}
	if withLabels {
		dest = append(dest, &r.FromLabel, &r.ToLabel)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return &r, nil
}

// EntityOwned verifies that an entity of the given type exists and belongs to userID.
func (s *Store) EntityOwned(ctx context.Context, userID uuid.UUID, t domain.EntityType, id uuid.UUID) (bool, error) {
	var table string
	switch t {
	case domain.EntityIdea:
		table = "ideas"
	case domain.EntityBranch:
		table = "branches"
	case domain.EntityCheckpoint:
		table = "checkpoints"
	case domain.EntityConversation:
		table = "conversations"
	case domain.EntityMessage:
		table = "messages"
	case domain.EntitySource:
		table = "sources"
	case domain.EntityArtifact:
		table = "artifacts"
	case domain.EntityContextPack:
		table = "context_packs"
	default:
		if t.IsKnowledge() {
			var ok bool
			err := s.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM knowledge_items WHERE id = $1 AND user_id = $2 AND kind = $3)`, id, userID, string(t)).Scan(&ok)
			return ok, err
		}
		return false, domain.Invalid("entity_type", "unknown entity type")
	}
	var ok bool
	err := s.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1 AND user_id = $2)`, id, userID).Scan(&ok)
	return ok, err
}

// CreateRelationship inserts an edge; duplicates return the existing edge.
func (s *Store) CreateRelationship(ctx context.Context, userID uuid.UUID, r *domain.Relationship) error {
	err := s.q.QueryRow(ctx, `
		INSERT INTO relationships (user_id, from_type, from_id, to_type, to_id, rel_type, idea_id, branch_id, origin, confidence, rationale, source_message_id, created_by, agent_run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (from_type, from_id, to_type, to_id, rel_type) DO UPDATE SET rationale = CASE WHEN relationships.rationale = '' THEN EXCLUDED.rationale ELSE relationships.rationale END
		RETURNING id, created_at`,
		userID, r.FromType, r.FromID, r.ToType, r.ToID, r.RelType, r.IdeaID, r.BranchID, r.Origin, r.Confidence, r.Rationale, r.SourceMessageID, r.CreatedBy, r.AgentRunID).
		Scan(&r.ID, &r.CreatedAt)
	return mapErr(err, "relationship")
}

// RelationshipFilter selects edges.
type RelationshipFilter struct {
	UserID   uuid.UUID
	IdeaID   *uuid.UUID
	BranchID *uuid.UUID
	EntityID *uuid.UUID // edges touching this entity (either side)
	RelTypes []domain.RelType
	Limit    int
}

// ListRelationships lists edges with resolved labels.
func (s *Store) ListRelationships(ctx context.Context, f RelationshipFilter) ([]domain.Relationship, error) {
	b := &qb{}
	b.add("r.user_id = ?", f.UserID)
	if f.IdeaID != nil {
		b.add("r.idea_id = ?", *f.IdeaID)
	}
	if f.BranchID != nil {
		b.add("(r.branch_id = ? OR r.branch_id IS NULL)", *f.BranchID)
	}
	if f.EntityID != nil {
		p := b.arg(*f.EntityID)
		b.where = append(b.where, fmt.Sprintf("(r.from_id = %s OR r.to_id = %s)", p, p))
	}
	if len(f.RelTypes) > 0 {
		rt := make([]string, len(f.RelTypes))
		for i, t := range f.RelTypes {
			rt[i] = string(t)
		}
		b.add("r.rel_type = ANY(?)", rt)
	}
	sql := `SELECT ` + relCols + `, ` + labelExpr("r.from_type", "r.from_id") + `, ` + labelExpr("r.to_type", "r.to_id") +
		` FROM relationships r` + b.clause() + fmt.Sprintf(` ORDER BY r.created_at LIMIT %d`, clampLimit(f.Limit, 500, 5000))
	rows, err := s.q.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Relationship
	for rows.Next() {
		r, err := scanRel(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// DeleteRelationship removes an edge.
func (s *Store) DeleteRelationship(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.q.Exec(ctx, `DELETE FROM relationships WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("relationship")
	}
	return nil
}
