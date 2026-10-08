package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// CreateAgentRun inserts a RUNNING agent run.
func (s *Store) CreateAgentRun(ctx context.Context, r *domain.AgentRun) error {
	if r.Status == "" {
		r.Status = domain.RunRunning
	}
	trace, _ := json.Marshal(nonNilTrace(r.Trace))
	state := r.State
	if len(state) == 0 {
		state = json.RawMessage(`{}`)
	}
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO agent_runs (user_id, conversation_id, idea_id, branch_id, user_message_id, status, task, provider, model, trace, state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id, started_at`,
		r.UserID, r.ConversationID, r.IdeaID, r.BranchID, r.UserMessageID, r.Status, r.Task, r.Provider, r.Model, trace, []byte(state)).
		Scan(&r.ID, &r.StartedAt), "agent run")
}

func nonNilTrace(t []domain.TraceEvent) []domain.TraceEvent {
	if t == nil {
		return []domain.TraceEvent{}
	}
	return t
}

// UpdateAgentRun persists run progress/final state.
func (s *Store) UpdateAgentRun(ctx context.Context, r *domain.AgentRun) error {
	trace, _ := json.Marshal(nonNilTrace(r.Trace))
	state := r.State
	if len(state) == 0 {
		state = json.RawMessage(`{}`)
	}
	_, err := s.q.Exec(ctx, `UPDATE agent_runs SET status = $3, idea_id = $4, branch_id = $5, assistant_message_id = $6, provider = $7, model = $8,
		trace = $9, state = $10, input_tokens = $11, output_tokens = $12, error = $13, completed_at = $14
		WHERE id = $1 AND user_id = $2`,
		r.ID, r.UserID, r.Status, r.IdeaID, r.BranchID, r.AssistantMessageID, r.Provider, r.Model, trace, []byte(state),
		r.InputTokens, r.OutputTokens, r.Error, r.CompletedAt)
	return err
}

const runCols = `id, user_id, conversation_id, idea_id, branch_id, user_message_id, assistant_message_id, status, task, provider, model,
	trace, state, input_tokens, output_tokens, error, started_at, completed_at`

func scanRun(row pgx.Row) (*domain.AgentRun, error) {
	var r domain.AgentRun
	var trace, state []byte
	if err := row.Scan(&r.ID, &r.UserID, &r.ConversationID, &r.IdeaID, &r.BranchID, &r.UserMessageID, &r.AssistantMessageID, &r.Status, &r.Task,
		&r.Provider, &r.Model, &trace, &state, &r.InputTokens, &r.OutputTokens, &r.Error, &r.StartedAt, &r.CompletedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(trace, &r.Trace)
	r.State = state
	return &r, nil
}

// GetAgentRun fetches a run with its tool calls.
func (s *Store) GetAgentRun(ctx context.Context, userID, id uuid.UUID) (*domain.AgentRun, error) {
	r, err := scanRun(s.q.QueryRow(ctx, `SELECT `+runCols+` FROM agent_runs WHERE id = $1 AND user_id = $2`, id, userID))
	if err != nil {
		return nil, mapErr(err, "agent run")
	}
	r.ToolCalls, err = s.ListToolCalls(ctx, userID, r.ID)
	return r, err
}

// ListAgentRuns lists recent runs (optionally per conversation).
func (s *Store) ListAgentRuns(ctx context.Context, userID uuid.UUID, convID *uuid.UUID, limit int) ([]domain.AgentRun, error) {
	b := &qb{}
	b.add("user_id = ?", userID)
	if convID != nil {
		b.add("conversation_id = ?", *convID)
	}
	rows, err := s.q.Query(ctx, `SELECT `+runCols+` FROM agent_runs`+b.clause()+fmt.Sprintf(` ORDER BY started_at DESC LIMIT %d`, clampLimit(limit, 50, 500)), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AgentRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		r.State = nil
		out = append(out, *r)
	}
	return out, rows.Err()
}

// MarkStaleRuns fails RUNNING runs older than the cutoff (e.g. after a crash).
func (s *Store) MarkStaleRuns(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.q.Exec(ctx, `UPDATE agent_runs SET status = 'FAILED', error = 'interrupted', completed_at = now() WHERE status = 'RUNNING' AND started_at < $1`, cutoff)
	return tag.RowsAffected(), err
}

// CreateToolCall inserts a tool call record.
func (s *Store) CreateToolCall(ctx context.Context, userID uuid.UUID, t *domain.ToolCallRecord) error {
	args := t.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return mapErr(s.q.QueryRow(ctx, `
		INSERT INTO tool_calls (agent_run_id, user_id, provider_call_id, tool_name, category, arguments, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`,
		t.AgentRunID, userID, t.ProviderCallID, t.ToolName, t.Category, []byte(args), t.Status).Scan(&t.ID, &t.CreatedAt), "tool call")
}

// UpdateToolCall stores the outcome of a tool call.
func (s *Store) UpdateToolCall(ctx context.Context, t *domain.ToolCallRecord) error {
	var res []byte
	if len(t.Result) > 0 {
		res = t.Result
	}
	_, err := s.q.Exec(ctx, `UPDATE tool_calls SET result = $2, summary = $3, status = $4, error = $5, latency_ms = $6, completed_at = $7 WHERE id = $1`,
		t.ID, res, t.Summary, t.Status, t.Error, t.LatencyMS, t.CompletedAt)
	return err
}

// GetToolCall fetches one tool call owned by the user.
func (s *Store) GetToolCall(ctx context.Context, userID, id uuid.UUID) (*domain.ToolCallRecord, error) {
	var t domain.ToolCallRecord
	var args, res []byte
	err := s.q.QueryRow(ctx, `SELECT id, agent_run_id, provider_call_id, tool_name, category, arguments, result, summary, status, error, latency_ms, created_at, completed_at
		FROM tool_calls WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&t.ID, &t.AgentRunID, &t.ProviderCallID, &t.ToolName, &t.Category, &args, &res, &t.Summary, &t.Status, &t.Error, &t.LatencyMS, &t.CreatedAt, &t.CompletedAt)
	if err != nil {
		return nil, mapErr(err, "tool call")
	}
	t.Arguments = args
	t.Result = res
	return &t, nil
}

// ListToolCalls lists a run's tool calls in order.
func (s *Store) ListToolCalls(ctx context.Context, userID, runID uuid.UUID) ([]domain.ToolCallRecord, error) {
	rows, err := s.q.Query(ctx, `SELECT id, agent_run_id, provider_call_id, tool_name, category, arguments, result, summary, status, error, latency_ms, created_at, completed_at
		FROM tool_calls WHERE agent_run_id = $1 AND user_id = $2 ORDER BY created_at`, runID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ToolCallRecord
	for rows.Next() {
		var t domain.ToolCallRecord
		var args, res []byte
		if err := rows.Scan(&t.ID, &t.AgentRunID, &t.ProviderCallID, &t.ToolName, &t.Category, &args, &res, &t.Summary, &t.Status, &t.Error, &t.LatencyMS, &t.CreatedAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		t.Arguments = args
		t.Result = res
		out = append(out, t)
	}
	return out, rows.Err()
}

// PendingConfirmations lists tool calls awaiting user confirmation.
func (s *Store) PendingConfirmations(ctx context.Context, userID uuid.UUID) ([]domain.ToolCallRecord, error) {
	return s.pendingConfirmations(ctx, userID, nil)
}

// PendingConfirmationsInConversation lists the calls awaiting confirmation in one conversation,
// so a reopened chat can show its approval prompt again.
func (s *Store) PendingConfirmationsInConversation(ctx context.Context, userID, convID uuid.UUID) ([]domain.ToolCallRecord, error) {
	return s.pendingConfirmations(ctx, userID, &convID)
}

func (s *Store) pendingConfirmations(ctx context.Context, userID uuid.UUID, convID *uuid.UUID) ([]domain.ToolCallRecord, error) {
	rows, err := s.q.Query(ctx, `SELECT t.id, t.agent_run_id, t.provider_call_id, t.tool_name, t.category, t.arguments, t.summary, t.status, t.created_at
		FROM tool_calls t JOIN agent_runs r ON r.id = t.agent_run_id AND r.user_id = t.user_id
		WHERE t.user_id = $1 AND t.status = 'AWAITING_CONFIRMATION' AND ($2::uuid IS NULL OR r.conversation_id = $2)
		ORDER BY t.created_at`, userID, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ToolCallRecord
	for rows.Next() {
		var t domain.ToolCallRecord
		var args []byte
		if err := rows.Scan(&t.ID, &t.AgentRunID, &t.ProviderCallID, &t.ToolName, &t.Category, &args, &t.Summary, &t.Status, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Arguments = args
		out = append(out, t)
	}
	return out, rows.Err()
}
