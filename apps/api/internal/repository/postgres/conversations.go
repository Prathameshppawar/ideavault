package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// CreateSource inserts a provenance source.
func (s *Store) CreateSource(ctx context.Context, src *domain.Source) error {
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO sources (user_id, kind, provider, adapter, title, uri, external_id, content_hash, trusted, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, created_at`,
		src.UserID, src.Kind, src.Provider, src.Adapter, src.Title, src.URI, src.ExternalID, src.ContentHash, src.Trusted, toJSON(src.Metadata)).
		Scan(&src.ID, &src.CreatedAt), "source")
}

// GetSource fetches a source.
func (s *Store) GetSource(ctx context.Context, userID, id uuid.UUID) (*domain.Source, error) {
	var src domain.Source
	var meta []byte
	err := s.q.QueryRow(ctx, `SELECT id, user_id, kind, provider, adapter, title, uri, external_id, content_hash, trusted, metadata, created_at
		FROM sources WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&src.ID, &src.UserID, &src.Kind, &src.Provider, &src.Adapter, &src.Title, &src.URI, &src.ExternalID, &src.ContentHash, &src.Trusted, &meta, &src.CreatedAt)
	if err != nil {
		return nil, mapErr(err, "source")
	}
	src.Metadata = jsonMap(meta)
	return &src, nil
}

const convCols = `c.id, c.user_id, c.idea_id, c.branch_id, c.source_id, c.title, c.origin, c.provider, c.external_id, c.summary,
	c.message_count, c.started_at, c.created_at, c.updated_at, coalesce(i.title, ''), coalesce(b.name, '')`

const convFrom = ` FROM conversations c LEFT JOIN ideas i ON i.id = c.idea_id LEFT JOIN branches b ON b.id = c.branch_id`

func scanConversation(row pgx.Row) (*domain.Conversation, error) {
	var c domain.Conversation
	err := row.Scan(&c.ID, &c.UserID, &c.IdeaID, &c.BranchID, &c.SourceID, &c.Title, &c.Origin, &c.Provider, &c.ExternalID, &c.Summary,
		&c.MessageCount, &c.StartedAt, &c.CreatedAt, &c.UpdatedAt, &c.IdeaTitle, &c.BranchName)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateConversation inserts a conversation.
func (s *Store) CreateConversation(ctx context.Context, c *domain.Conversation) error {
	if c.Origin == "" {
		c.Origin = domain.OriginNative
	}
	if c.Provider == "" {
		c.Provider = "ideavault"
	}
	started := c.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO conversations (user_id, idea_id, branch_id, source_id, title, origin, provider, external_id, summary, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, started_at, created_at, updated_at`,
		c.UserID, c.IdeaID, c.BranchID, c.SourceID, c.Title, c.Origin, c.Provider, c.ExternalID, c.Summary, started).
		Scan(&c.ID, &c.StartedAt, &c.CreatedAt, &c.UpdatedAt), "conversation")
}

// GetConversation fetches a conversation owned by userID.
func (s *Store) GetConversation(ctx context.Context, userID, id uuid.UUID) (*domain.Conversation, error) {
	c, err := scanConversation(s.q.QueryRow(ctx, `SELECT `+convCols+convFrom+` WHERE c.id = $1 AND c.user_id = $2`, id, userID))
	return c, mapErr(err, "conversation")
}

// FindConversationByExternal finds an imported conversation by provider + external id.
func (s *Store) FindConversationByExternal(ctx context.Context, userID uuid.UUID, provider, externalID string) (*domain.Conversation, error) {
	c, err := scanConversation(s.q.QueryRow(ctx, `SELECT `+convCols+convFrom+` WHERE c.user_id = $1 AND c.provider = $2 AND c.external_id = $3`, userID, provider, externalID))
	return c, mapErr(err, "conversation")
}

// FindConversationBySourceHash finds a conversation whose source has the given content hash.
func (s *Store) FindConversationBySourceHash(ctx context.Context, userID uuid.UUID, hash string) (*domain.Conversation, error) {
	c, err := scanConversation(s.q.QueryRow(ctx, `SELECT `+convCols+convFrom+`
		JOIN sources s ON s.id = c.source_id WHERE c.user_id = $1 AND s.content_hash = $2 ORDER BY c.created_at LIMIT 1`, userID, hash))
	return c, mapErr(err, "conversation")
}

// ConversationFilter selects conversations.
type ConversationFilter struct {
	UserID   uuid.UUID
	IdeaID   *uuid.UUID
	BranchID *uuid.UUID
	// IncludeLinked also returns conversations linked to BranchID via branch_links (inherited).
	IncludeLinked bool
	Origin        string
	Unassigned    bool
	Query         string
	Limit         int
	Offset        int
}

// ListConversations lists conversations, newest activity first.
func (s *Store) ListConversations(ctx context.Context, f ConversationFilter) ([]domain.Conversation, error) {
	b := &qb{}
	b.add("c.user_id = ?", f.UserID)
	if f.IdeaID != nil {
		b.add("c.idea_id = ?", *f.IdeaID)
	}
	if f.BranchID != nil {
		if f.IncludeLinked {
			p := b.arg(*f.BranchID)
			b.where = append(b.where, fmt.Sprintf("(c.branch_id = %s OR c.id IN (SELECT entity_id FROM branch_links WHERE branch_id = %s AND entity_type = 'conversation'))", p, p))
		} else {
			b.add("c.branch_id = ?", *f.BranchID)
		}
	}
	if f.Origin != "" {
		b.add("c.origin = ?", f.Origin)
	}
	if f.Unassigned {
		b.add("c.idea_id IS NULL")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		b.add("(c.search_tsv @@ websearch_to_tsquery('english', ?) OR c.title ILIKE ?)", q, "%"+q+"%")
	}
	sql := `SELECT ` + convCols + convFrom + b.clause() + ` ORDER BY c.updated_at DESC` +
		fmt.Sprintf(` LIMIT %d OFFSET %d`, clampLimit(f.Limit, 50, 500), max(f.Offset, 0))
	rows, err := s.q.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// AttachConversation links a conversation to an idea/branch (when previously unattached or moving primary branch).
func (s *Store) AttachConversation(ctx context.Context, userID, convID uuid.UUID, ideaID, branchID *uuid.UUID) error {
	tag, err := s.q.Exec(ctx, `UPDATE conversations SET idea_id = $3, branch_id = $4 WHERE id = $1 AND user_id = $2`, convID, userID, ideaID, branchID)
	if err != nil {
		return mapErr(err, "conversation")
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("conversation")
	}
	return nil
}

// UpdateConversationMeta sets title/summary.
func (s *Store) UpdateConversationMeta(ctx context.Context, userID, convID uuid.UUID, title, summary *string) error {
	_, err := s.q.Exec(ctx, `UPDATE conversations SET title = coalesce($3, title), summary = coalesce($4, summary) WHERE id = $1 AND user_id = $2`,
		convID, userID, title, summary)
	return err
}

// AppendMessage appends a message at the next position (serialized per conversation).
func (s *Store) AppendMessage(ctx context.Context, m *domain.Message) error {
	return s.WithTx(ctx, func(tx *Store) error {
		if err := tx.lockKey(ctx, "conv", m.ConversationID.String()); err != nil {
			return err
		}
		created := m.CreatedAt
		if created.IsZero() {
			created = time.Now()
		}
		err := tx.q.QueryRow(ctx, `
			INSERT INTO messages (user_id, conversation_id, position, role, content, untrusted, external_id, model, agent_run_id, metadata, created_at)
			VALUES ($1, $2, (SELECT coalesce(max(position), 0) + 1 FROM messages WHERE conversation_id = $2), $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id, position, created_at`,
			m.UserID, m.ConversationID, m.Role, sanitizeText(m.Content), m.Untrusted, m.ExternalID, m.Model, m.AgentRunID, toJSON(m.Metadata), created).
			Scan(&m.ID, &m.Position, &m.CreatedAt)
		if err != nil {
			return mapErr(err, "message")
		}
		_, err = tx.q.Exec(ctx, `UPDATE conversations SET message_count = message_count + 1, updated_at = now() WHERE id = $1`, m.ConversationID)
		return err
	})
}

// UpdateMessageContent finalizes a streamed assistant message (content + metadata).
// Only assistant messages produced by IdeaVault may be updated.
func (s *Store) UpdateMessageContent(ctx context.Context, userID, id uuid.UUID, content string, meta map[string]any) error {
	_, err := s.q.Exec(ctx, `UPDATE messages SET content = $3, metadata = metadata || $4::jsonb
		WHERE id = $1 AND user_id = $2 AND role = 'assistant' AND NOT untrusted`, id, userID, sanitizeText(content), toJSON(meta))
	return err
}

const msgCols = `id, user_id, conversation_id, position, role, content, untrusted, external_id, model, agent_run_id, metadata, created_at`

func scanMessage(row pgx.Row) (*domain.Message, error) {
	var m domain.Message
	var meta []byte
	if err := row.Scan(&m.ID, &m.UserID, &m.ConversationID, &m.Position, &m.Role, &m.Content, &m.Untrusted, &m.ExternalID, &m.Model, &m.AgentRunID, &meta, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.Metadata = jsonMap(meta)
	return &m, nil
}

// ListMessages returns messages in order. If afterPosition > 0, only later messages.
func (s *Store) ListMessages(ctx context.Context, userID, convID uuid.UUID, afterPosition, limit int) ([]domain.Message, error) {
	rows, err := s.q.Query(ctx, `SELECT `+msgCols+` FROM messages WHERE conversation_id = $1 AND user_id = $2 AND position > $3 ORDER BY position LIMIT $4`,
		convID, userID, afterPosition, clampLimit(limit, 500, 5000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// RecentMessages returns the last n messages of a conversation in chronological order.
func (s *Store) RecentMessages(ctx context.Context, userID, convID uuid.UUID, n int) ([]domain.Message, error) {
	rows, err := s.q.Query(ctx, `SELECT `+msgCols+` FROM (SELECT * FROM messages WHERE conversation_id = $1 AND user_id = $2 ORDER BY position DESC LIMIT $3) t ORDER BY position`,
		convID, userID, clampLimit(n, 20, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetMessage fetches one message.
func (s *Store) GetMessage(ctx context.Context, userID, id uuid.UUID) (*domain.Message, error) {
	m, err := scanMessage(s.q.QueryRow(ctx, `SELECT `+msgCols+` FROM messages WHERE id = $1 AND user_id = $2`, id, userID))
	return m, mapErr(err, "message")
}

// MessageWindow returns messages around a position (inclusive radius).
func (s *Store) MessageWindow(ctx context.Context, userID, convID uuid.UUID, position, radius int) ([]domain.Message, error) {
	rows, err := s.q.Query(ctx, `SELECT `+msgCols+` FROM messages WHERE conversation_id = $1 AND user_id = $2 AND position BETWEEN $3 AND $4 ORDER BY position`,
		convID, userID, position-radius, position+radius)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// UserMessages returns the user's own (trusted, typed) messages for prompt/thinking analysis.
func (s *Store) UserMessages(ctx context.Context, userID uuid.UUID, ideaID *uuid.UUID, limit int) ([]domain.Message, error) {
	b := &qb{}
	b.add("m.user_id = ?", userID)
	b.add("m.role = 'user'")
	if ideaID != nil {
		b.add("c.idea_id = ?", *ideaID)
	}
	rows, err := s.q.Query(ctx, `SELECT m.id, m.user_id, m.conversation_id, m.position, m.role, m.content, m.untrusted, m.external_id, m.model, m.agent_run_id, m.metadata, m.created_at
		FROM messages m JOIN conversations c ON c.id = m.conversation_id`+b.clause()+fmt.Sprintf(` ORDER BY m.created_at DESC LIMIT %d`, clampLimit(limit, 200, 2000)), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// sanitizeText strips NUL bytes (rejected by PostgreSQL text) and invalid UTF-8.
func sanitizeText(s string) string {
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	return strings.ToValidUTF8(s, "�")
}
