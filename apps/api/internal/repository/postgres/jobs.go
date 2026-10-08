package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/db"
)

// Job is a unit of background work stored in PostgreSQL.
type Job struct {
	ID          uuid.UUID       `json:"id"`
	UserID      *uuid.UUID      `json:"user_id,omitempty"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	Status      string          `json:"status"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	LastError   string          `json:"last_error"`
	CreatedAt   time.Time       `json:"created_at"`
}

// EnqueueJob adds a job. A non-empty dedupeKey makes enqueueing idempotent while a matching job is pending.
func (s *Store) EnqueueJob(ctx context.Context, userID *uuid.UUID, kind string, payload any, dedupeKey string, runAfter time.Time) (*Job, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var dk *string
	if dedupeKey != "" {
		dk = &dedupeKey
	}
	if runAfter.IsZero() {
		runAfter = time.Now()
	}
	j := &Job{UserID: userID, Kind: kind, Payload: b, Status: "QUEUED", MaxAttempts: 3}
	err = s.q.QueryRow(ctx, `INSERT INTO jobs (user_id, kind, payload, dedupe_key, run_after) VALUES ($1,$2,$3,$4,$5) RETURNING id, created_at`,
		userID, kind, b, dk, runAfter).Scan(&j.ID, &j.CreatedAt)
	if err != nil && db.IsUniqueViolation(err) {
		return nil, nil // an equivalent job is already pending
	}
	return j, err
}

// ClaimJob atomically claims the next runnable job (SKIP LOCKED) or returns nil.
func (s *Store) ClaimJob(ctx context.Context, workerID string) (*Job, error) {
	var j Job
	err := s.q.QueryRow(ctx, `
		UPDATE jobs SET status = 'RUNNING', attempts = attempts + 1, locked_at = now(), locked_by = $1
		WHERE id = (SELECT id FROM jobs WHERE status = 'QUEUED' AND run_after <= now() ORDER BY run_after, created_at FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id, user_id, kind, payload, status, attempts, max_attempts, last_error, created_at`, workerID).
		Scan(&j.ID, &j.UserID, &j.Kind, &j.Payload, &j.Status, &j.Attempts, &j.MaxAttempts, &j.LastError, &j.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &j, err
}

// CompleteJob marks a job done.
func (s *Store) CompleteJob(ctx context.Context, id uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE jobs SET status = 'COMPLETED', completed_at = now(), locked_at = NULL WHERE id = $1`, id)
	return err
}

// FailJob records a failure; retries with exponential backoff until max attempts.
func (s *Store) FailJob(ctx context.Context, j *Job, cause error) error {
	msg := truncate(cause.Error(), 1000)
	if j.Attempts < j.MaxAttempts {
		backoff := time.Duration(1<<j.Attempts) * 2 * time.Second
		_, err := s.q.Exec(ctx, `UPDATE jobs SET status = 'QUEUED', last_error = $2, locked_at = NULL, run_after = now() + $3::interval WHERE id = $1`,
			j.ID, msg, backoff.String())
		return err
	}
	_, err := s.q.Exec(ctx, `UPDATE jobs SET status = 'FAILED', last_error = $2, locked_at = NULL, completed_at = now() WHERE id = $1`, j.ID, msg)
	return err
}

// RequeueStaleJobs returns RUNNING jobs whose lock is older than cutoff to the queue.
func (s *Store) RequeueStaleJobs(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.q.Exec(ctx, `UPDATE jobs SET status = 'QUEUED', locked_at = NULL WHERE status = 'RUNNING' AND locked_at < $1`, cutoff)
	return tag.RowsAffected(), err
}

// JobStats summarises the queue.
func (s *Store) JobStats(ctx context.Context) (map[string]int, error) {
	rows, err := s.q.Query(ctx, `SELECT status, count(*) FROM jobs WHERE created_at > now() - interval '7 days' GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}
