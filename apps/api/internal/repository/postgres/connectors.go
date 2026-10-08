package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// ConnectorState is the persisted per-user connector state.
type ConnectorState struct {
	ConnectorKey       string                 `json:"connector_key"`
	Status             domain.ConnectorStatus `json:"status"`
	Config             map[string]any         `json:"config"`
	GrantedPermissions []string               `json:"granted_permissions"`
	LastError          string                 `json:"last_error"`
	LastCheckedAt      *time.Time             `json:"last_checked_at,omitempty"`
	ConnectedAt        *time.Time             `json:"connected_at,omitempty"`
}

// ListConnectorStates returns persisted connector states keyed by connector key.
func (s *Store) ListConnectorStates(ctx context.Context, userID uuid.UUID) (map[string]ConnectorState, error) {
	rows, err := s.q.Query(ctx, `SELECT connector_key, status, config, granted_permissions, last_error, last_checked_at, connected_at FROM connectors WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ConnectorState{}
	for rows.Next() {
		var c ConnectorState
		var cfg []byte
		if err := rows.Scan(&c.ConnectorKey, &c.Status, &cfg, &c.GrantedPermissions, &c.LastError, &c.LastCheckedAt, &c.ConnectedAt); err != nil {
			return nil, err
		}
		c.Config = jsonMap(cfg)
		c.GrantedPermissions = nonNilStrings(c.GrantedPermissions)
		out[c.ConnectorKey] = c
	}
	return out, rows.Err()
}

// UpsertConnectorState writes connector status after a connect/health check.
func (s *Store) UpsertConnectorState(ctx context.Context, userID uuid.UUID, c ConnectorState) error {
	_, err := s.q.Exec(ctx, `
		INSERT INTO connectors (user_id, connector_key, status, config, granted_permissions, last_error, last_checked_at, connected_at)
		VALUES ($1,$2,$3,$4,$5,$6,now(), CASE WHEN $3 = 'CONNECTED' THEN now() END)
		ON CONFLICT (user_id, connector_key) DO UPDATE SET status = EXCLUDED.status, config = EXCLUDED.config,
			granted_permissions = EXCLUDED.granted_permissions, last_error = EXCLUDED.last_error, last_checked_at = now(),
			connected_at = CASE WHEN EXCLUDED.status = 'CONNECTED' THEN coalesce(connectors.connected_at, now()) ELSE NULL END`,
		userID, c.ConnectorKey, c.Status, toJSON(c.Config), nonNilStrings(c.GrantedPermissions), truncate(c.LastError, 500))
	return err
}

// EncryptedCredential is a stored, encrypted secret.
type EncryptedCredential struct {
	ID         uuid.UUID  `json:"id"`
	OwnerType  string     `json:"owner_type"`
	OwnerKey   string     `json:"owner_key"`
	Name       string     `json:"name"`
	Ciphertext []byte     `json:"-"`
	Nonce      []byte     `json:"-"`
	KeyVersion int        `json:"key_version"`
	Hint       string     `json:"hint"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// PutCredential upserts an encrypted credential.
func (s *Store) PutCredential(ctx context.Context, userID uuid.UUID, c *EncryptedCredential) error {
	return s.q.QueryRow(ctx, `
		INSERT INTO credentials (user_id, owner_type, owner_key, name, ciphertext, nonce, key_version, hint)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (user_id, owner_type, owner_key, name) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, nonce = EXCLUDED.nonce,
			key_version = EXCLUDED.key_version, hint = EXCLUDED.hint
		RETURNING id, created_at, updated_at`,
		userID, c.OwnerType, c.OwnerKey, c.Name, c.Ciphertext, c.Nonce, c.KeyVersion, c.Hint).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

// GetCredential fetches an encrypted credential.
func (s *Store) GetCredential(ctx context.Context, userID uuid.UUID, ownerType, ownerKey, name string) (*EncryptedCredential, error) {
	var c EncryptedCredential
	err := s.q.QueryRow(ctx, `SELECT id, owner_type, owner_key, name, ciphertext, nonce, key_version, hint, created_at, updated_at, last_used_at
		FROM credentials WHERE user_id = $1 AND owner_type = $2 AND owner_key = $3 AND name = $4`, userID, ownerType, ownerKey, name).
		Scan(&c.ID, &c.OwnerType, &c.OwnerKey, &c.Name, &c.Ciphertext, &c.Nonce, &c.KeyVersion, &c.Hint, &c.CreatedAt, &c.UpdatedAt, &c.LastUsedAt)
	if err != nil {
		return nil, mapErr(err, "credential")
	}
	return &c, nil
}

// ListCredentials lists credential metadata (never secrets).
func (s *Store) ListCredentials(ctx context.Context, userID uuid.UUID) ([]EncryptedCredential, error) {
	rows, err := s.q.Query(ctx, `SELECT id, owner_type, owner_key, name, key_version, hint, created_at, updated_at, last_used_at
		FROM credentials WHERE user_id = $1 ORDER BY owner_type, owner_key, name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EncryptedCredential
	for rows.Next() {
		var c EncryptedCredential
		if err := rows.Scan(&c.ID, &c.OwnerType, &c.OwnerKey, &c.Name, &c.KeyVersion, &c.Hint, &c.CreatedAt, &c.UpdatedAt, &c.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AllModelProviderCredentials returns every user's encrypted model-provider credentials (for registry wiring).
func (s *Store) AllModelProviderCredentials(ctx context.Context) (map[uuid.UUID][]EncryptedCredential, error) {
	rows, err := s.q.Query(ctx, `SELECT user_id, id, owner_type, owner_key, name, ciphertext, nonce, key_version, hint FROM credentials WHERE owner_type = 'model_provider'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]EncryptedCredential{}
	for rows.Next() {
		var uid uuid.UUID
		var c EncryptedCredential
		if err := rows.Scan(&uid, &c.ID, &c.OwnerType, &c.OwnerKey, &c.Name, &c.Ciphertext, &c.Nonce, &c.KeyVersion, &c.Hint); err != nil {
			return nil, err
		}
		out[uid] = append(out[uid], c)
	}
	return out, rows.Err()
}

// TouchCredential stamps last_used_at.
func (s *Store) TouchCredential(ctx context.Context, id uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE credentials SET last_used_at = now() WHERE id = $1`, id)
	return err
}

// DeleteCredential removes a credential.
func (s *Store) DeleteCredential(ctx context.Context, userID uuid.UUID, ownerType, ownerKey, name string) error {
	tag, err := s.q.Exec(ctx, `DELETE FROM credentials WHERE user_id = $1 AND owner_type = $2 AND owner_key = $3 AND name = $4`, userID, ownerType, ownerKey, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("credential")
	}
	return nil
}

// InsertConnectorEvent records a connector invocation.
func (s *Store) InsertConnectorEvent(ctx context.Context, userID uuid.UUID, e *domain.ConnectorEvent) error {
	return s.q.QueryRow(ctx, `INSERT INTO connector_events (user_id, connector_key, tool, operation, success, latency_ms, error, agent_run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at`,
		userID, e.ConnectorKey, e.Tool, e.Operation, e.Success, e.LatencyMS, truncate(e.Error, 500), e.AgentRunID).Scan(&e.ID, &e.CreatedAt)
}

// ListConnectorEvents lists recent connector events.
func (s *Store) ListConnectorEvents(ctx context.Context, userID uuid.UUID, connectorKey string, since time.Time, limit int) ([]domain.ConnectorEvent, error) {
	b := &qb{}
	b.add("user_id = ?", userID)
	b.add("created_at >= ?", since)
	if connectorKey != "" {
		b.add("connector_key = ?", connectorKey)
	}
	rows, err := s.q.Query(ctx, `SELECT id, connector_key, tool, operation, success, latency_ms, error, agent_run_id, created_at FROM connector_events`+
		b.clause()+fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, clampLimit(limit, 100, 2000)), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ConnectorEvent
	for rows.Next() {
		var e domain.ConnectorEvent
		if err := rows.Scan(&e.ID, &e.ConnectorKey, &e.Tool, &e.Operation, &e.Success, &e.LatencyMS, &e.Error, &e.AgentRunID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ConnectorUsageRow aggregates connector usage.
type ConnectorUsageRow struct {
	ConnectorKey string    `json:"connector_key"`
	Tool         string    `json:"tool"`
	Calls        int       `json:"calls"`
	Failures     int       `json:"failures"`
	AvgLatencyMS float64   `json:"avg_latency_ms"`
	LastAt       time.Time `json:"last_at"`
}

// ConnectorUsage aggregates connector events by connector and tool.
func (s *Store) ConnectorUsage(ctx context.Context, userID uuid.UUID, since time.Time) ([]ConnectorUsageRow, error) {
	rows, err := s.q.Query(ctx, `SELECT connector_key, tool, count(*), count(*) FILTER (WHERE NOT success), coalesce(avg(latency_ms),0)::float8, max(created_at)
		FROM connector_events WHERE user_id = $1 AND created_at >= $2 GROUP BY connector_key, tool ORDER BY connector_key, tool`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConnectorUsageRow
	for rows.Next() {
		var r ConnectorUsageRow
		if err := rows.Scan(&r.ConnectorKey, &r.Tool, &r.Calls, &r.Failures, &r.AvgLatencyMS, &r.LastAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
