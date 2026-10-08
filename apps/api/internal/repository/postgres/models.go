package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

const modelCols = `id, provider, model, display_name, capabilities, context_length, tool_calling, structured_output, vision, reasoning,
	relative_cost, speed, quality, input_cost_per_mtok::float8, output_cost_per_mtok::float8, enabled, is_builtin, created_at, updated_at`

func scanModel(row pgx.Row) (*domain.ModelConfig, error) {
	var m domain.ModelConfig
	err := row.Scan(&m.ID, &m.Provider, &m.Model, &m.DisplayName, &m.Capabilities, &m.ContextLength, &m.ToolCalling, &m.StructuredOutput,
		&m.Vision, &m.Reasoning, &m.RelativeCost, &m.Speed, &m.Quality, &m.InputCostPerMTok, &m.OutputCostPerMTok, &m.Enabled, &m.IsBuiltin,
		&m.CreatedAt, &m.UpdatedAt)
	m.Capabilities = nonNilStrings(m.Capabilities)
	return &m, err
}

// SeedModel inserts a built-in model if absent. Existing rows keep user edits
// (enabled flag, prices) but refresh descriptive metadata.
func (s *Store) SeedModel(ctx context.Context, m domain.ModelConfig) error {
	_, err := s.q.Exec(ctx, `
		INSERT INTO model_configs (provider, model, display_name, capabilities, context_length, tool_calling, structured_output, vision, reasoning,
			relative_cost, speed, quality, input_cost_per_mtok, output_cost_per_mtok, enabled, is_builtin)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,true)
		ON CONFLICT (provider, model) DO UPDATE SET display_name = EXCLUDED.display_name, capabilities = EXCLUDED.capabilities,
			context_length = EXCLUDED.context_length, tool_calling = EXCLUDED.tool_calling, structured_output = EXCLUDED.structured_output,
			vision = EXCLUDED.vision, reasoning = EXCLUDED.reasoning
		WHERE model_configs.is_builtin`,
		m.Provider, m.Model, m.DisplayName, nonNilStrings(m.Capabilities), m.ContextLength, m.ToolCalling, m.StructuredOutput, m.Vision, m.Reasoning,
		m.RelativeCost, m.Speed, m.Quality, m.InputCostPerMTok, m.OutputCostPerMTok, m.Enabled)
	return err
}

// UpsertModel creates or updates a (custom or built-in) model config.
func (s *Store) UpsertModel(ctx context.Context, m *domain.ModelConfig) error {
	got, err := scanModel(s.q.QueryRow(ctx, `
		INSERT INTO model_configs (provider, model, display_name, capabilities, context_length, tool_calling, structured_output, vision, reasoning,
			relative_cost, speed, quality, input_cost_per_mtok, output_cost_per_mtok, enabled, is_builtin)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,false)
		ON CONFLICT (provider, model) DO UPDATE SET display_name = EXCLUDED.display_name, capabilities = EXCLUDED.capabilities,
			context_length = EXCLUDED.context_length, tool_calling = EXCLUDED.tool_calling, structured_output = EXCLUDED.structured_output,
			vision = EXCLUDED.vision, reasoning = EXCLUDED.reasoning, relative_cost = EXCLUDED.relative_cost, speed = EXCLUDED.speed,
			quality = EXCLUDED.quality, input_cost_per_mtok = EXCLUDED.input_cost_per_mtok, output_cost_per_mtok = EXCLUDED.output_cost_per_mtok,
			enabled = EXCLUDED.enabled
		RETURNING `+modelCols,
		m.Provider, m.Model, m.DisplayName, nonNilStrings(m.Capabilities), m.ContextLength, m.ToolCalling, m.StructuredOutput, m.Vision, m.Reasoning,
		m.RelativeCost, m.Speed, m.Quality, m.InputCostPerMTok, m.OutputCostPerMTok, m.Enabled))
	if err != nil {
		return mapErr(err, "model")
	}
	*m = *got
	return nil
}

// SetModelEnabled toggles a model.
func (s *Store) SetModelEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	tag, err := s.q.Exec(ctx, `UPDATE model_configs SET enabled = $2 WHERE id = $1`, id, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("model")
	}
	return nil
}

// DeleteModel deletes a non-builtin model config.
func (s *Store) DeleteModel(ctx context.Context, id uuid.UUID) error {
	tag, err := s.q.Exec(ctx, `DELETE FROM model_configs WHERE id = $1 AND NOT is_builtin`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.Invalid("id", "model not found or built-in (disable it instead)")
	}
	return nil
}

// ListModels lists all model configs.
func (s *Store) ListModels(ctx context.Context) ([]domain.ModelConfig, error) {
	rows, err := s.q.Query(ctx, `SELECT `+modelCols+` FROM model_configs ORDER BY provider, quality DESC, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ModelConfig
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// InsertUsage records a model call.
func (s *Store) InsertUsage(ctx context.Context, userID *uuid.UUID, u *domain.UsageEvent) error {
	if u.ModelCallID == uuid.Nil {
		u.ModelCallID = uuid.New()
	}
	return s.q.QueryRow(ctx, `
		INSERT INTO usage_events (user_id, model_call_id, provider, model, operation, task, input_tokens, output_tokens, total_tokens, tokens_estimated,
			estimated_cost_usd, latency_ms, tool_calls, success, error, conversation_id, idea_id, agent_run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING id, created_at`,
		userID, u.ModelCallID, u.Provider, u.Model, u.Operation, u.Task, u.InputTokens, u.OutputTokens, u.TotalTokens, u.TokensEstimated,
		u.EstimatedCostUSD, u.LatencyMS, u.ToolCalls, u.Success, truncate(u.Error, 1000), u.ConversationID, u.IdeaID, u.AgentRunID).
		Scan(&u.ID, &u.CreatedAt)
}

// UsageFilter selects usage events.
type UsageFilter struct {
	UserID   uuid.UUID
	Since    time.Time
	Until    *time.Time
	Provider string
	Model    string
	Task     string
	IdeaID   *uuid.UUID
	Limit    int
}

func (f UsageFilter) build() *qb {
	b := &qb{}
	b.add("u.user_id = ?", f.UserID)
	b.add("u.created_at >= ?", f.Since)
	if f.Until != nil {
		b.add("u.created_at <= ?", *f.Until)
	}
	if f.Provider != "" {
		b.add("u.provider = ?", f.Provider)
	}
	if f.Model != "" {
		b.add("u.model = ?", f.Model)
	}
	if f.Task != "" {
		b.add("u.task = ?", f.Task)
	}
	if f.IdeaID != nil {
		b.add("u.idea_id = ?", *f.IdeaID)
	}
	return b
}

// ListUsage returns recent usage events.
func (s *Store) ListUsage(ctx context.Context, f UsageFilter) ([]domain.UsageEvent, error) {
	b := f.build()
	rows, err := s.q.Query(ctx, `SELECT u.id, u.model_call_id, u.provider, u.model, u.operation, u.task, u.input_tokens, u.output_tokens, u.total_tokens,
		u.tokens_estimated, u.estimated_cost_usd::float8, u.latency_ms, u.tool_calls, u.success, u.error, u.conversation_id, u.idea_id, u.agent_run_id, u.created_at
		FROM usage_events u`+b.clause()+fmt.Sprintf(` ORDER BY u.created_at DESC LIMIT %d`, clampLimit(f.Limit, 100, 2000)), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.UsageEvent
	for rows.Next() {
		var u domain.UsageEvent
		if err := rows.Scan(&u.ID, &u.ModelCallID, &u.Provider, &u.Model, &u.Operation, &u.Task, &u.InputTokens, &u.OutputTokens, &u.TotalTokens,
			&u.TokensEstimated, &u.EstimatedCostUSD, &u.LatencyMS, &u.ToolCalls, &u.Success, &u.Error, &u.ConversationID, &u.IdeaID, &u.AgentRunID, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UsageGroup is an aggregate row.
type UsageGroup struct {
	Key          string  `json:"key"`
	Calls        int     `json:"calls"`
	Failures     int     `json:"failures"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	P95LatencyMS float64 `json:"p95_latency_ms"`
	ToolCalls    int64   `json:"tool_calls"`
	Estimated    int     `json:"estimated"`
}

// UsageBreakdown aggregates usage by a dimension: provider | model | task | operation | day | idea.
func (s *Store) UsageBreakdown(ctx context.Context, f UsageFilter, dim string) ([]UsageGroup, error) {
	var key string
	switch dim {
	case "provider":
		key = "u.provider"
	case "model":
		key = "u.provider || '/' || u.model"
	case "task":
		key = "u.task"
	case "operation":
		key = "u.operation"
	case "day":
		key = "to_char(date_trunc('day', u.created_at), 'YYYY-MM-DD')"
	case "idea":
		key = "coalesce((SELECT title FROM ideas WHERE id = u.idea_id), '(no idea)')"
	default:
		return nil, domain.Invalid("group_by", "must be provider, model, task, operation, day or idea")
	}
	b := f.build()
	rows, err := s.q.Query(ctx, `SELECT `+key+` AS k, count(*), count(*) FILTER (WHERE NOT u.success),
		coalesce(sum(u.input_tokens), 0), coalesce(sum(u.output_tokens), 0), coalesce(sum(u.total_tokens), 0),
		coalesce(sum(u.estimated_cost_usd), 0)::float8, coalesce(avg(u.latency_ms), 0)::float8,
		coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY u.latency_ms), 0)::float8,
		coalesce(sum(u.tool_calls), 0), count(*) FILTER (WHERE u.tokens_estimated)
		FROM usage_events u`+b.clause()+` GROUP BY k ORDER BY k`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageGroup
	for rows.Next() {
		var g UsageGroup
		if err := rows.Scan(&g.Key, &g.Calls, &g.Failures, &g.InputTokens, &g.OutputTokens, &g.TotalTokens, &g.CostUSD, &g.AvgLatencyMS, &g.P95LatencyMS, &g.ToolCalls, &g.Estimated); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
