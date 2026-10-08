// Package models is IdeaVault's provider-agnostic AI layer: chat providers,
// embedders, the model registry, task routing and usage accounting.
package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Role of a chat message in a provider-neutral conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a provider-neutral chat message.
//
//   - Assistant messages may carry ToolCalls.
//   - Tool messages carry the result of exactly one tool call in Content and
//     reference it with ToolCallID (and Name = tool name).
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	// Replay is opaque, provider-specific content for an assistant message that
	// must be sent back verbatim on the next request to the same provider/model
	// (e.g. Anthropic thinking blocks, which must be replayed unchanged inside a
	// tool loop). It is never shown to users or persisted outside run state.
	Replay *ReplayState `json:"replay,omitempty"`
}

// ReplayState ties opaque replay content to the provider and model that produced it.
// Providers ignore replay state from other providers/models.
type ReplayState struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Content  json.RawMessage `json:"content"`
}

// ToolCall is a model's request to invoke a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDef describes a tool the model may call. Parameters is a JSON Schema object.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Request is a single model invocation.
type Request struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// JSONSchema requests structured JSON output conforming to the schema.
	// Providers use native structured output where supported and otherwise
	// fall back to prompting for JSON. SchemaName is a short identifier.
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	SchemaName string          `json:"schema_name,omitempty"`
}

// Usage reports token consumption. Estimated is true when the provider did not
// report usage and counts were approximated locally.
type Usage struct {
	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	Estimated    bool `json:"estimated"`
}

// Total returns input + output tokens.
func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }

// Response is the final result of a model invocation. Providers must never put
// hidden reasoning / chain-of-thought into Content; such blocks are discarded.
type Response struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        Usage      `json:"usage"`
	FinishReason string     `json:"finish_reason"` // stop | tool_calls | length | refusal | error
	Model        string     `json:"model"`
	// Replay, when non-nil, must be attached to the assistant Message built from
	// this response so the provider can replay it verbatim.
	Replay *ReplayState `json:"replay,omitempty"`
}

// StreamHandler receives incremental visible text as it is generated.
type StreamHandler func(delta string)

// Provider is a chat-completion backend (OpenAI, Groq, Anthropic, Gemini, local, mock...).
type Provider interface {
	// ID is the registry provider key, e.g. "openai", "groq", "anthropic", "gemini", "ollama", "mock".
	ID() string
	// Complete performs a non-streaming request.
	Complete(ctx context.Context, req Request) (*Response, error)
	// Stream performs a streaming request, invoking onDelta for visible text and
	// returning the fully assembled response (including tool calls) at the end.
	Stream(ctx context.Context, req Request, onDelta StreamHandler) (*Response, error)
}

// Embedder produces fixed-dimension embeddings (EmbeddingDims) for texts.
type Embedder interface {
	ProviderID() string
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, Usage, error)
}

// EmbeddingDims is the fixed vector dimension stored in PostgreSQL (pgvector).
const EmbeddingDims = 768

// ProviderError is a classified provider failure.
type ProviderError struct {
	Provider   string
	StatusCode int
	// Retryable is true for rate limits, timeouts and 5xx responses.
	Retryable bool
	Message   string
}

func (e *ProviderError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Provider, e.Message)
}

// ErrNotConfigured is returned when a provider has no credentials.
var ErrNotConfigured = errors.New("provider not configured")

// IsRetryable reports whether err is a retryable provider failure.
func IsRetryable(err error) bool {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Retryable
	}
	return errors.Is(err, context.DeadlineExceeded)
}
