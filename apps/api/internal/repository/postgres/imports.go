package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

const importCols = `id, source_kind, provider, adapter, status, stage, filename, uri, byte_size, content_hash, total_items, processed_items,
	failed_items, options, warnings, error, target_idea_id, created_at, updated_at, completed_at`

func scanImport(row pgx.Row) (*domain.Import, error) {
	var im domain.Import
	var opts, warns []byte
	if err := row.Scan(&im.ID, &im.SourceKind, &im.Provider, &im.Adapter, &im.Status, &im.Stage, &im.Filename, &im.URI, &im.ByteSize, &im.ContentHash,
		&im.TotalItems, &im.ProcessedItems, &im.FailedItems, &opts, &warns, &im.Error, &im.TargetIdeaID, &im.CreatedAt, &im.UpdatedAt, &im.CompletedAt); err != nil {
		return nil, err
	}
	im.Options = jsonMap(opts)
	_ = json.Unmarshal(warns, &im.Warnings)
	if im.Warnings == nil {
		im.Warnings = []string{}
	}
	return &im, nil
}

// CreateImport inserts a queued import with its raw payload.
func (s *Store) CreateImport(ctx context.Context, userID uuid.UUID, im *domain.Import, raw []byte) error {
	got, err := scanImport(s.q.QueryRow(ctx, `
		INSERT INTO imports (user_id, source_kind, provider, adapter, status, filename, uri, byte_size, content_hash, raw_payload, options, target_idea_id)
		VALUES ($1,$2,$3,$4,'QUEUED',$5,$6,$7,$8,$9,$10,$11) RETURNING `+importCols,
		userID, im.SourceKind, im.Provider, im.Adapter, im.Filename, im.URI, im.ByteSize, im.ContentHash, raw, toJSON(im.Options), im.TargetIdeaID))
	if err != nil {
		return mapErr(err, "import")
	}
	*im = *got
	return nil
}

// GetImport fetches an import.
func (s *Store) GetImport(ctx context.Context, userID, id uuid.UUID) (*domain.Import, error) {
	im, err := scanImport(s.q.QueryRow(ctx, `SELECT `+importCols+` FROM imports WHERE id = $1 AND user_id = $2`, id, userID))
	return im, mapErr(err, "import")
}

// ImportRaw returns the raw payload of an import (until it is cleared after parsing).
func (s *Store) ImportRaw(ctx context.Context, userID, id uuid.UUID) ([]byte, error) {
	var raw []byte
	err := s.q.QueryRow(ctx, `SELECT raw_payload FROM imports WHERE id = $1 AND user_id = $2`, id, userID).Scan(&raw)
	return raw, mapErr(err, "import")
}

// ListImports lists imports newest first.
func (s *Store) ListImports(ctx context.Context, userID uuid.UUID, limit int) ([]domain.Import, error) {
	rows, err := s.q.Query(ctx, `SELECT `+importCols+` FROM imports WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, userID, clampLimit(limit, 50, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Import
	for rows.Next() {
		im, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *im)
	}
	return out, rows.Err()
}

// ImportUpdate is a partial update of import progress.
type ImportUpdate struct {
	Status         *domain.ImportStatus
	Stage          *string
	Provider       *string
	Adapter        *string
	TotalItems     *int
	ProcessedItems *int
	FailedItems    *int
	Warnings       []string
	Error          *string
	ClearRaw       bool
	Completed      bool
}

// UpdateImport applies a partial update.
func (s *Store) UpdateImport(ctx context.Context, id uuid.UUID, u ImportUpdate) error {
	var warns []byte
	if u.Warnings != nil {
		warns, _ = json.Marshal(u.Warnings)
	}
	var status *string
	if u.Status != nil {
		st := string(*u.Status)
		status = &st
	}
	_, err := s.q.Exec(ctx, `UPDATE imports SET
		status = coalesce($2, status), stage = coalesce($3, stage), provider = coalesce($4, provider), adapter = coalesce($5, adapter),
		total_items = coalesce($6, total_items), processed_items = coalesce($7, processed_items), failed_items = coalesce($8, failed_items),
		warnings = coalesce($9::jsonb, warnings), error = coalesce($10, error),
		raw_payload = CASE WHEN $11 THEN NULL ELSE raw_payload END,
		completed_at = CASE WHEN $12 THEN now() ELSE completed_at END
		WHERE id = $1`,
		id, status, u.Stage, u.Provider, u.Adapter, u.TotalItems, u.ProcessedItems, u.FailedItems, warns, u.Error, u.ClearRaw, u.Completed)
	return err
}

// IncrementImportProgress atomically bumps processed/failed counters.
func (s *Store) IncrementImportProgress(ctx context.Context, id uuid.UUID, processed, failed int) error {
	_, err := s.q.Exec(ctx, `UPDATE imports SET processed_items = processed_items + $2, failed_items = failed_items + $3 WHERE id = $1`, id, processed, failed)
	return err
}

// InsertImportItem stores one parsed conversation.
func (s *Store) InsertImportItem(ctx context.Context, userID uuid.UUID, it *domain.ImportItem) error {
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO import_items (import_id, user_id, position, external_id, title, status, message_count, started_at, content_hash, payload, injection_flags, duplicate_of, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id, created_at, updated_at`,
		it.ImportID, userID, it.Position, it.ExternalID, sanitizeText(it.Title), it.Status, it.MessageCount, it.StartedAt, it.ContentHash,
		[]byte(it.Payload), nonNilStrings(it.InjectionFlags), it.DuplicateOf, it.Error).Scan(&it.ID, &it.CreatedAt, &it.UpdatedAt), "import item")
}

// ListImportItems lists items of an import (payload omitted unless withPayload).
func (s *Store) ListImportItems(ctx context.Context, userID, importID uuid.UUID, withPayload bool, statuses []string) ([]domain.ImportItem, error) {
	b := &qb{}
	b.add("import_id = ?", importID)
	b.add("user_id = ?", userID)
	if len(statuses) > 0 {
		b.add("status = ANY(?)", statuses)
	}
	payload := "NULL::jsonb"
	if withPayload {
		payload = "payload"
	}
	rows, err := s.q.Query(ctx, `SELECT id, import_id, position, external_id, title, status, message_count, started_at, content_hash, `+payload+`,
		injection_flags, duplicate_of, conversation_id, idea_id, extracted_count, error, created_at, updated_at,
		(SELECT coalesce(jsonb_agg(jsonb_build_object('role', m->>'role', 'excerpt', left(m->>'content', 220))), '[]'::jsonb)
			FROM (SELECT m FROM jsonb_array_elements(payload->'messages') m LIMIT 4) t)
		FROM import_items`+b.clause()+` ORDER BY position`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ImportItem
	for rows.Next() {
		var it domain.ImportItem
		var pl, preview []byte
		if err := rows.Scan(&it.ID, &it.ImportID, &it.Position, &it.ExternalID, &it.Title, &it.Status, &it.MessageCount, &it.StartedAt, &it.ContentHash, &pl,
			&it.InjectionFlags, &it.DuplicateOf, &it.ConversationID, &it.IdeaID, &it.ExtractedCount, &it.Error, &it.CreatedAt, &it.UpdatedAt, &preview); err != nil {
			return nil, err
		}
		if len(pl) > 0 {
			it.Payload = pl
		}
		_ = json.Unmarshal(preview, &it.Preview)
		it.InjectionFlags = nonNilStrings(it.InjectionFlags)
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetImportItem fetches one item with payload.
func (s *Store) GetImportItem(ctx context.Context, userID, id uuid.UUID) (*domain.ImportItem, error) {
	var it domain.ImportItem
	var pl []byte
	err := s.q.QueryRow(ctx, `SELECT id, import_id, position, external_id, title, status, message_count, started_at, content_hash, payload,
		injection_flags, duplicate_of, conversation_id, idea_id, extracted_count, error, created_at, updated_at
		FROM import_items WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&it.ID, &it.ImportID, &it.Position, &it.ExternalID, &it.Title, &it.Status, &it.MessageCount, &it.StartedAt, &it.ContentHash, &pl,
			&it.InjectionFlags, &it.DuplicateOf, &it.ConversationID, &it.IdeaID, &it.ExtractedCount, &it.Error, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, mapErr(err, "import item")
	}
	it.Payload = pl
	return &it, nil
}

// ImportItemUpdate is a partial update of an import item.
type ImportItemUpdate struct {
	Status         *string
	ConversationID *uuid.UUID
	IdeaID         *uuid.UUID
	ExtractedCount *int
	Error          *string
}

// UpdateImportItem applies a partial update.
func (s *Store) UpdateImportItem(ctx context.Context, id uuid.UUID, u ImportItemUpdate) error {
	_, err := s.q.Exec(ctx, `UPDATE import_items SET status = coalesce($2, status), conversation_id = coalesce($3, conversation_id),
		idea_id = coalesce($4, idea_id), extracted_count = coalesce($5, extracted_count), error = coalesce($6, error) WHERE id = $1`,
		id, u.Status, u.ConversationID, u.IdeaID, u.ExtractedCount, u.Error)
	return err
}

// FailStuckImports marks imports stuck in PROCESSING (e.g. after a crash) so they can be retried.
func (s *Store) FailStuckImports(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.q.Exec(ctx, `UPDATE imports SET status = 'FAILED', error = 'interrupted; retry the import' WHERE status = 'PROCESSING' AND updated_at < $1
		AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.status IN ('QUEUED','RUNNING') AND j.payload->>'import_id' = imports.id::text)`, cutoff)
	return tag.RowsAffected(), err
}

// UpdateImportOptions replaces an import's options JSON.
func (s *Store) UpdateImportOptions(ctx context.Context, id uuid.UUID, opts map[string]any) error {
	_, err := s.q.Exec(ctx, `UPDATE imports SET options = $2 WHERE id = $1`, id, toJSON(opts))
	return err
}
