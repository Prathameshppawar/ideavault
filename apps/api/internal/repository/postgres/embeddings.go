package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// VectorLiteral formats a float32 slice as a pgvector text literal.
func VectorLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 10)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', 7, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// EmbeddingRow is one stored embedding chunk.
type EmbeddingRow struct {
	EntityType  domain.EntityType
	EntityID    uuid.UUID
	IdeaID      *uuid.UUID
	ChunkIndex  int
	Content     string
	ContentHash string
	Provider    string
	Model       string
	Vector      []float32
}

// UpsertEmbedding writes (or replaces) one embedding chunk.
func (s *Store) UpsertEmbedding(ctx context.Context, userID uuid.UUID, e EmbeddingRow) error {
	_, err := s.q.Exec(ctx, `
		INSERT INTO embeddings (user_id, entity_type, entity_id, idea_id, chunk_index, content, content_hash, provider, model, embedding)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::vector)
		ON CONFLICT (entity_type, entity_id, chunk_index, model) DO UPDATE SET content = EXCLUDED.content, content_hash = EXCLUDED.content_hash,
			embedding = EXCLUDED.embedding, idea_id = EXCLUDED.idea_id, provider = EXCLUDED.provider, created_at = now()`,
		userID, e.EntityType, e.EntityID, e.IdeaID, e.ChunkIndex, truncate(sanitizeText(e.Content), 2000), e.ContentHash, e.Provider, e.Model, VectorLiteral(e.Vector))
	return err
}

// EmbeddingHashes returns chunk_index → content_hash for an entity under a model (to skip unchanged content).
func (s *Store) EmbeddingHashes(ctx context.Context, entityType domain.EntityType, entityID uuid.UUID, model string) (map[int]string, error) {
	rows, err := s.q.Query(ctx, `SELECT chunk_index, content_hash FROM embeddings WHERE entity_type = $1 AND entity_id = $2 AND model = $3`, entityType, entityID, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var i int
		var h string
		if err := rows.Scan(&i, &h); err != nil {
			return nil, err
		}
		out[i] = h
	}
	return out, rows.Err()
}

// DeleteEmbeddingsFrom removes chunks with index >= from (after content shrank).
func (s *Store) DeleteEmbeddingsFrom(ctx context.Context, entityType domain.EntityType, entityID uuid.UUID, model string, from int) error {
	_, err := s.q.Exec(ctx, `DELETE FROM embeddings WHERE entity_type = $1 AND entity_id = $2 AND model = $3 AND chunk_index >= $4`, entityType, entityID, model, from)
	return err
}

// SemanticHit is a vector search result.
type SemanticHit struct {
	EntityType domain.EntityType `json:"entity_type"`
	EntityID   uuid.UUID         `json:"entity_id"`
	IdeaID     *uuid.UUID        `json:"idea_id,omitempty"`
	ChunkIndex int               `json:"chunk_index"`
	Content    string            `json:"content"`
	Score      float64           `json:"score"` // cosine similarity in [−1, 1]
}

// SemanticSearch finds nearest embeddings (cosine) for the user under one model.
func (s *Store) SemanticSearch(ctx context.Context, userID uuid.UUID, model string, vec []float32, types []domain.EntityType, ideaID *uuid.UUID, limit int) ([]SemanticHit, error) {
	b := &qb{}
	vp := b.arg(VectorLiteral(vec))
	b.add("e.user_id = ?", userID)
	b.add("e.model = ?", model)
	if len(types) > 0 {
		ts := make([]string, len(types))
		for i, t := range types {
			ts[i] = string(t)
		}
		b.add("e.entity_type = ANY(?)", ts)
	}
	if ideaID != nil {
		b.add("e.idea_id = ?", *ideaID)
	}
	sql := fmt.Sprintf(`SELECT e.entity_type, e.entity_id, e.idea_id, e.chunk_index, e.content, 1 - (e.embedding <=> %s::vector) AS score
		FROM embeddings e%s ORDER BY e.embedding <=> %s::vector LIMIT %d`, vp, b.clause(), vp, clampLimit(limit, 20, 200))
	rows, err := s.q.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SemanticHit
	for rows.Next() {
		var h SemanticHit
		var et string
		if err := rows.Scan(&et, &h.EntityID, &h.IdeaID, &h.ChunkIndex, &h.Content, &h.Score); err != nil {
			return nil, err
		}
		h.EntityType = domain.EntityType(et)
		out = append(out, h)
	}
	return out, rows.Err()
}

// EmbeddingCoverage reports how many entities are embedded per type for a model.
func (s *Store) EmbeddingCoverage(ctx context.Context, userID uuid.UUID, model string) (map[string]int, error) {
	rows, err := s.q.Query(ctx, `SELECT entity_type, count(DISTINCT entity_id) FROM embeddings WHERE user_id = $1 AND model = $2 GROUP BY entity_type`, userID, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var t string
		var n int
		if err := rows.Scan(&t, &n); err != nil {
			return nil, err
		}
		out[t] = n
	}
	return out, rows.Err()
}

// UnembeddedEntity is an entity lacking an embedding for a model.
type UnembeddedEntity struct {
	EntityType domain.EntityType
	EntityID   uuid.UUID
}

// UnembeddedEntities lists entities without embeddings for the given model (for backfill).
func (s *Store) UnembeddedEntities(ctx context.Context, userID uuid.UUID, model string, limit int) ([]UnembeddedEntity, error) {
	rows, err := s.q.Query(ctx, `
		SELECT t, id FROM (
			SELECT 'idea' t, id, created_at FROM ideas WHERE user_id = $1
			UNION ALL SELECT kind, id, created_at FROM knowledge_items WHERE user_id = $1 AND review_state <> 'REJECTED'
			UNION ALL SELECT 'message', id, created_at FROM messages WHERE user_id = $1 AND role IN ('user','assistant') AND length(content) > 20
			UNION ALL SELECT 'artifact', id, created_at FROM artifacts WHERE user_id = $1
		) x WHERE NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.entity_id = x.id AND e.model = $2)
		ORDER BY created_at DESC LIMIT $3`, userID, model, clampLimit(limit, 200, 5000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnembeddedEntity
	for rows.Next() {
		var u UnembeddedEntity
		var t string
		if err := rows.Scan(&t, &u.EntityID); err != nil {
			return nil, err
		}
		u.EntityType = domain.EntityType(t)
		out = append(out, u)
	}
	return out, rows.Err()
}
