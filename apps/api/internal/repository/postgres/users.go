package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// CountUsers returns the number of users (0 means first-run setup is allowed).
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts a user with a pre-hashed password.
func (s *Store) CreateUser(ctx context.Context, email, displayName, passwordHash string) (*domain.User, error) {
	u := &domain.User{}
	err := s.q.QueryRow(ctx, `
		INSERT INTO users (email, display_name, password_hash) VALUES ($1, $2, $3)
		RETURNING id, email, display_name, created_at`, email, displayName, passwordHash).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	return u, mapErr(err, "user")
}

// UserByEmail returns a user and password hash.
func (s *Store) UserByEmail(ctx context.Context, email string) (*domain.User, string, error) {
	u := &domain.User{}
	var hash string
	err := s.q.QueryRow(ctx, `SELECT id, email, display_name, created_at, password_hash FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt, &hash)
	return u, hash, mapErr(err, "user")
}

// UserByID returns a user.
func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	u := &domain.User{}
	err := s.q.QueryRow(ctx, `SELECT id, email, display_name, created_at FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	return u, mapErr(err, "user")
}

// UpdatePassword replaces a user's password hash.
func (s *Store) UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.q.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, id, hash)
	return err
}

// CreateSession stores a hashed session token.
func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, kind, label, ua, ip string, expires time.Time) (*domain.Session, error) {
	ss := &domain.Session{}
	err := s.q.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, kind, label, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, kind, label, user_agent, created_at, last_seen_at, expires_at`,
		userID, tokenHash, kind, label, ua, ip, expires).
		Scan(&ss.ID, &ss.UserID, &ss.Kind, &ss.Label, &ss.UserAgent, &ss.CreatedAt, &ss.LastSeenAt, &ss.ExpiresAt)
	return ss, mapErr(err, "session")
}

// SessionByTokenHash resolves an active, unexpired session and touches last_seen_at.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash []byte) (*domain.Session, error) {
	ss := &domain.Session{}
	err := s.q.QueryRow(ctx, `
		UPDATE sessions SET last_seen_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		RETURNING id, user_id, kind, label, user_agent, created_at, last_seen_at, expires_at`, tokenHash).
		Scan(&ss.ID, &ss.UserID, &ss.Kind, &ss.Label, &ss.UserAgent, &ss.CreatedAt, &ss.LastSeenAt, &ss.ExpiresAt)
	return ss, mapErr(err, "session")
}

// RevokeSession revokes a session by token hash.
func (s *Store) RevokeSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.q.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	return err
}

// RevokeSessionByID revokes one of the user's sessions/tokens.
func (s *Store) RevokeSessionByID(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.q.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("session")
	}
	return nil
}

// ListSessions lists the user's active sessions and API tokens.
func (s *Store) ListSessions(ctx context.Context, userID uuid.UUID) ([]domain.Session, error) {
	rows, err := s.q.Query(ctx, `
		SELECT id, user_id, kind, label, user_agent, created_at, last_seen_at, expires_at, revoked_at
		FROM sessions WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY last_seen_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Session
	for rows.Next() {
		var ss domain.Session
		if err := rows.Scan(&ss.ID, &ss.UserID, &ss.Kind, &ss.Label, &ss.UserAgent, &ss.CreatedAt, &ss.LastSeenAt, &ss.ExpiresAt, &ss.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, ss)
	}
	return out, rows.Err()
}

// PurgeExpiredSessions deletes expired/revoked sessions older than a grace period.
func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.q.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now() - interval '7 days' OR revoked_at < now() - interval '7 days'`)
	return tag.RowsAffected(), err
}

// GetSettings returns the user's settings JSON (empty object if none).
func (s *Store) GetSettings(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	var raw []byte
	err := s.q.QueryRow(ctx, `SELECT settings FROM user_settings WHERE user_id = $1`, userID).Scan(&raw)
	if err != nil {
		if domain.IsNotFound(mapErr(err, "settings")) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	return jsonMap(raw), nil
}

// MergeSettings shallow-merges patch into the user's settings and returns the result.
func (s *Store) MergeSettings(ctx context.Context, userID uuid.UUID, patch map[string]any) (map[string]any, error) {
	b, _ := json.Marshal(patch)
	var raw []byte
	err := s.q.QueryRow(ctx, `
		INSERT INTO user_settings (user_id, settings) VALUES ($1, $2::jsonb)
		ON CONFLICT (user_id) DO UPDATE SET settings = user_settings.settings || EXCLUDED.settings
		RETURNING settings`, userID, b).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return jsonMap(raw), nil
}

// FirstUserID returns the oldest user's id (the vault owner in personal mode).
func (s *Store) FirstUserID(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.q.QueryRow(ctx, `SELECT id FROM users ORDER BY created_at LIMIT 1`).Scan(&id)
	return id, mapErr(err, "user")
}
