package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// LabRun is a Model Lab comparison run.
type LabRun struct {
	ID          uuid.UUID       `json:"id"`
	Task        string          `json:"task"`
	Title       string          `json:"title"`
	System      string          `json:"system"`
	Prompt      string          `json:"prompt"`
	ExpectJSON  bool            `json:"expect_json"`
	JSONSchema  json.RawMessage `json:"json_schema,omitempty"`
	Reference   string          `json:"reference"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	Results     []LabResult     `json:"results,omitempty"`
}

// LabResult is one model's output in a lab run.
type LabResult struct {
	ID               uuid.UUID       `json:"id"`
	RunID            uuid.UUID       `json:"run_id"`
	Provider         string          `json:"provider"`
	Model            string          `json:"model"`
	Output           string          `json:"output"`
	ValidJSON        *bool           `json:"valid_json,omitempty"`
	SchemaErrors     json.RawMessage `json:"schema_errors"`
	Correctness      *float32        `json:"correctness,omitempty"`
	Error            string          `json:"error,omitempty"`
	LatencyMS        int             `json:"latency_ms"`
	InputTokens      int             `json:"input_tokens"`
	OutputTokens     int             `json:"output_tokens"`
	TotalTokens      int             `json:"total_tokens"`
	EstimatedCostUSD float64         `json:"estimated_cost_usd"`
	CreatedAt        time.Time       `json:"created_at"`
}

// CreateLabRun inserts a lab run.
func (s *Store) CreateLabRun(ctx context.Context, userID uuid.UUID, r *LabRun) error {
	var schema []byte
	if len(r.JSONSchema) > 0 {
		schema = r.JSONSchema
	}
	return s.q.QueryRow(ctx, `INSERT INTO model_lab_runs (user_id, task, title, system, prompt, expect_json, json_schema, reference)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, status, created_at`,
		userID, r.Task, r.Title, r.System, r.Prompt, r.ExpectJSON, schema, r.Reference).Scan(&r.ID, &r.Status, &r.CreatedAt)
}

// InsertLabResult stores a model result.
func (s *Store) InsertLabResult(ctx context.Context, res *LabResult) error {
	errs := res.SchemaErrors
	if len(errs) == 0 {
		errs = json.RawMessage(`[]`)
	}
	return s.q.QueryRow(ctx, `INSERT INTO model_lab_results (run_id, provider, model, output, valid_json, schema_errors, correctness, error, latency_ms,
		input_tokens, output_tokens, total_tokens, estimated_cost_usd) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id, created_at`,
		res.RunID, res.Provider, res.Model, sanitizeText(res.Output), res.ValidJSON, []byte(errs), res.Correctness, res.Error, res.LatencyMS,
		res.InputTokens, res.OutputTokens, res.TotalTokens, res.EstimatedCostUSD).Scan(&res.ID, &res.CreatedAt)
}

// CompleteLabRun marks a run finished.
func (s *Store) CompleteLabRun(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.q.Exec(ctx, `UPDATE model_lab_runs SET status = $2, completed_at = now() WHERE id = $1`, id, status)
	return err
}

// GetLabRun fetches a run with results.
func (s *Store) GetLabRun(ctx context.Context, userID, id uuid.UUID) (*LabRun, error) {
	var r LabRun
	var schema []byte
	err := s.q.QueryRow(ctx, `SELECT id, task, title, system, prompt, expect_json, json_schema, reference, status, created_at, completed_at
		FROM model_lab_runs WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&r.ID, &r.Task, &r.Title, &r.System, &r.Prompt, &r.ExpectJSON, &schema, &r.Reference, &r.Status, &r.CreatedAt, &r.CompletedAt)
	if err != nil {
		return nil, mapErr(err, "lab run")
	}
	r.JSONSchema = schema
	rows, err := s.q.Query(ctx, `SELECT id, run_id, provider, model, output, valid_json, schema_errors, correctness, error, latency_ms, input_tokens,
		output_tokens, total_tokens, estimated_cost_usd::float8, created_at FROM model_lab_results WHERE run_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var res LabResult
		var errs []byte
		if err := rows.Scan(&res.ID, &res.RunID, &res.Provider, &res.Model, &res.Output, &res.ValidJSON, &errs, &res.Correctness, &res.Error, &res.LatencyMS,
			&res.InputTokens, &res.OutputTokens, &res.TotalTokens, &res.EstimatedCostUSD, &res.CreatedAt); err != nil {
			return nil, err
		}
		res.SchemaErrors = errs
		r.Results = append(r.Results, res)
	}
	return &r, rows.Err()
}

// ListLabRuns lists runs newest first (without results).
func (s *Store) ListLabRuns(ctx context.Context, userID uuid.UUID, limit int) ([]LabRun, error) {
	rows, err := s.q.Query(ctx, `SELECT id, task, title, system, prompt, expect_json, reference, status, created_at, completed_at
		FROM model_lab_runs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, userID, clampLimit(limit, 30, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LabRun
	for rows.Next() {
		var r LabRun
		if err := rows.Scan(&r.ID, &r.Task, &r.Title, &r.System, &r.Prompt, &r.ExpectJSON, &r.Reference, &r.Status, &r.CreatedAt, &r.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
