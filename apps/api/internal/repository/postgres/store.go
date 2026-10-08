// Package postgres implements IdeaVault persistence on PostgreSQL (pgx + pgvector).
// Every query is scoped by user_id; no method returns another user's data.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/db"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// Store is the repository facade. A Store created by WithTx runs every call in that transaction.
type Store struct {
	pool *pgxpool.Pool
	q    db.DBTX
	inTx bool
}

// New returns a Store backed by pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: pool} }

// Pool exposes the underlying pool (for health checks and job workers).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// WithTx runs fn in a transaction. Nested calls reuse the outer transaction.
func (s *Store) WithTx(ctx context.Context, fn func(tx *Store) error) error {
	if s.inTx {
		return fn(s)
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{pool: s.pool, q: tx, inTx: true})
	})
}

// AllowHistoryDelete enables deletion of immutable history rows for the current
// transaction only. Used exclusively by explicit, user-confirmed idea deletion.
func (s *Store) AllowHistoryDelete(ctx context.Context) error {
	if !s.inTx {
		return errors.New("AllowHistoryDelete requires a transaction")
	}
	_, err := s.q.Exec(ctx, `SET LOCAL ideavault.allow_history_delete = 'on'`)
	return err
}

// lockKey takes a transaction-scoped advisory lock derived from the given parts.
func (s *Store) lockKey(ctx context.Context, parts ...string) error {
	_, err := s.q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, strings.Join(parts, ":"))
	return err
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// mapErr converts storage errors into domain errors where meaningful.
func mapErr(err error, entity string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NotFound(entity)
	}
	if db.IsUniqueViolation(err) {
		return domain.Conflict(fmt.Sprintf("%s already exists (%s)", entity, db.ConstraintName(err)))
	}
	if db.IsCheckViolation(err) && strings.Contains(err.Error(), "immutable") {
		return domain.Immutable(err.Error())
	}
	if db.IsForeignKeyViolation(err) {
		return domain.Invalid(entity, "references a missing or foreign entity")
	}
	return err
}

// qb is a tiny positional-argument query builder.
type qb struct {
	where []string
	args  []any
}

func (b *qb) add(cond string, vals ...any) {
	for _, v := range vals {
		b.args = append(b.args, v)
		cond = strings.Replace(cond, "?", fmt.Sprintf("$%d", len(b.args)), 1)
	}
	b.where = append(b.where, cond)
}

func (b *qb) arg(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *qb) clause() string {
	if len(b.where) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(b.where, " AND ")
}

func toJSON(v any) []byte {
	if v == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func jsonMap(b []byte) map[string]any {
	m := map[string]any{}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}
