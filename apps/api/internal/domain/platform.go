package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// User is the (single) owner of a personal vault. Multi-user is possible later
// because every row is scoped by user_id.
type User struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// Session is an authenticated browser session or personal API token.
type Session struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"-"`
	Kind       string     `json:"kind"`
	Label      string     `json:"label"`
	UserAgent  string     `json:"user_agent"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// ToolCategory classifies agent tools for permission policy.
type ToolCategory string

const (
	ToolRead        ToolCategory = "READ"
	ToolAnalyze     ToolCategory = "ANALYZE"
	ToolWrite       ToolCategory = "WRITE"
	ToolDestructive ToolCategory = "DESTRUCTIVE"
	ToolExternal    ToolCategory = "EXTERNAL"
)

// AgentRunStatus is the lifecycle of one supervisor run.
type AgentRunStatus string

const (
	RunRunning              AgentRunStatus = "RUNNING"
	RunCompleted            AgentRunStatus = "COMPLETED"
	RunFailed               AgentRunStatus = "FAILED"
	RunCancelled            AgentRunStatus = "CANCELLED"
	RunAwaitingConfirmation AgentRunStatus = "AWAITING_CONFIRMATION"
)

// TraceEvent is a safe, user-visible execution event. It never contains chain-of-thought.
type TraceEvent struct {
	At         time.Time    `json:"at"`
	Kind       string       `json:"kind"` // step | tool | model | permission | error
	Label      string       `json:"label"`
	Tool       string       `json:"tool,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	Status     string       `json:"status,omitempty"`
	Refs       []EntityRef  `json:"refs,omitempty"`
	Category   ToolCategory `json:"category,omitempty"`
}

// AgentRun is one execution of the Agent Supervisor.
type AgentRun struct {
	ID                 uuid.UUID        `json:"id"`
	UserID             uuid.UUID        `json:"-"`
	ConversationID     *uuid.UUID       `json:"conversation_id,omitempty"`
	IdeaID             *uuid.UUID       `json:"idea_id,omitempty"`
	BranchID           *uuid.UUID       `json:"branch_id,omitempty"`
	UserMessageID      *uuid.UUID       `json:"user_message_id,omitempty"`
	AssistantMessageID *uuid.UUID       `json:"assistant_message_id,omitempty"`
	Status             AgentRunStatus   `json:"status"`
	Task               string           `json:"task"`
	Provider           string           `json:"provider"`
	Model              string           `json:"model"`
	Trace              []TraceEvent     `json:"trace"`
	State              json.RawMessage  `json:"-"`
	InputTokens        int              `json:"input_tokens"`
	OutputTokens       int              `json:"output_tokens"`
	Error              string           `json:"error,omitempty"`
	StartedAt          time.Time        `json:"started_at"`
	CompletedAt        *time.Time       `json:"completed_at,omitempty"`
	ToolCalls          []ToolCallRecord `json:"tool_calls,omitempty"`
}

// ToolCallRecord is a persisted tool invocation.
type ToolCallRecord struct {
	ID             uuid.UUID       `json:"id"`
	AgentRunID     uuid.UUID       `json:"agent_run_id"`
	ProviderCallID string          `json:"provider_call_id"`
	ToolName       string          `json:"tool_name"`
	Category       ToolCategory    `json:"category"`
	Arguments      json.RawMessage `json:"arguments"`
	Result         json.RawMessage `json:"result,omitempty"`
	Summary        string          `json:"summary"`
	Status         string          `json:"status"`
	Error          string          `json:"error,omitempty"`
	LatencyMS      int             `json:"latency_ms"`
	CreatedAt      time.Time       `json:"created_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}

// ModelConfig is a model registry entry.
type ModelConfig struct {
	ID                uuid.UUID `json:"id"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	DisplayName       string    `json:"display_name"`
	Capabilities      []string  `json:"capabilities"`
	ContextLength     int       `json:"context_length"`
	ToolCalling       bool      `json:"tool_calling"`
	StructuredOutput  bool      `json:"structured_output"`
	Vision            bool      `json:"vision"`
	Reasoning         bool      `json:"reasoning"`
	RelativeCost      int       `json:"relative_cost"`
	Speed             int       `json:"speed"`
	Quality           int       `json:"quality"`
	InputCostPerMTok  float64   `json:"input_cost_per_mtok"`
	OutputCostPerMTok float64   `json:"output_cost_per_mtok"`
	Enabled           bool      `json:"enabled"`
	IsBuiltin         bool      `json:"is_builtin"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	// Available is computed: provider has credentials and the model is enabled.
	Available bool `json:"available"`
}

// UsageEvent is one model call's accounting record.
type UsageEvent struct {
	ID               uuid.UUID  `json:"id"`
	ModelCallID      uuid.UUID  `json:"model_call_id"`
	Provider         string     `json:"provider"`
	Model            string     `json:"model"`
	Operation        string     `json:"operation"`
	Task             string     `json:"task"`
	InputTokens      int        `json:"input_tokens"`
	OutputTokens     int        `json:"output_tokens"`
	TotalTokens      int        `json:"total_tokens"`
	TokensEstimated  bool       `json:"tokens_estimated"`
	EstimatedCostUSD float64    `json:"estimated_cost_usd"`
	LatencyMS        int        `json:"latency_ms"`
	ToolCalls        int        `json:"tool_calls"`
	Success          bool       `json:"success"`
	Error            string     `json:"error,omitempty"`
	ConversationID   *uuid.UUID `json:"conversation_id,omitempty"`
	IdeaID           *uuid.UUID `json:"idea_id,omitempty"`
	AgentRunID       *uuid.UUID `json:"agent_run_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// ConnectorStatus is the honest connection state of a connector.
type ConnectorStatus string

const (
	ConnectorConnected    ConnectorStatus = "CONNECTED"
	ConnectorNotConnected ConnectorStatus = "NOT_CONNECTED"
	ConnectorError        ConnectorStatus = "ERROR"
)

// ConnectorEvent is one connector tool invocation record.
type ConnectorEvent struct {
	ID           uuid.UUID  `json:"id"`
	ConnectorKey string     `json:"connector_key"`
	Tool         string     `json:"tool"`
	Operation    string     `json:"operation"`
	Success      bool       `json:"success"`
	LatencyMS    int        `json:"latency_ms"`
	Error        string     `json:"error,omitempty"`
	AgentRunID   *uuid.UUID `json:"agent_run_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// ImportStatus is the lifecycle of an import.
type ImportStatus string

const (
	ImportQueued     ImportStatus = "QUEUED"
	ImportProcessing ImportStatus = "PROCESSING"
	ImportPreview    ImportStatus = "PREVIEW"
	ImportCompleted  ImportStatus = "COMPLETED"
	ImportFailed     ImportStatus = "FAILED"
	ImportPartial    ImportStatus = "PARTIAL"
	ImportCancelled  ImportStatus = "CANCELLED"
)

// Import is one import job (file, paste or URL).
type Import struct {
	ID             uuid.UUID      `json:"id"`
	SourceKind     string         `json:"source_kind"`
	Provider       string         `json:"provider"`
	Adapter        string         `json:"adapter"`
	Status         ImportStatus   `json:"status"`
	Stage          string         `json:"stage"`
	Filename       string         `json:"filename"`
	URI            string         `json:"uri"`
	ByteSize       int64          `json:"byte_size"`
	ContentHash    string         `json:"content_hash"`
	TotalItems     int            `json:"total_items"`
	ProcessedItems int            `json:"processed_items"`
	FailedItems    int            `json:"failed_items"`
	Options        map[string]any `json:"options"`
	Warnings       []string       `json:"warnings"`
	Error          string         `json:"error,omitempty"`
	TargetIdeaID   *uuid.UUID     `json:"target_idea_id,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
}

// ImportItem is one parsed conversation inside an import.
type ImportItem struct {
	ID             uuid.UUID        `json:"id"`
	ImportID       uuid.UUID        `json:"import_id"`
	Position       int              `json:"position"`
	ExternalID     string           `json:"external_id"`
	Title          string           `json:"title"`
	Status         string           `json:"status"`
	MessageCount   int              `json:"message_count"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	ContentHash    string           `json:"content_hash"`
	Payload        json.RawMessage  `json:"payload,omitempty"`
	InjectionFlags []string         `json:"injection_flags"`
	DuplicateOf    *uuid.UUID       `json:"duplicate_of,omitempty"`
	ConversationID *uuid.UUID       `json:"conversation_id,omitempty"`
	IdeaID         *uuid.UUID       `json:"idea_id,omitempty"`
	ExtractedCount int              `json:"extracted_count"`
	Error          string           `json:"error,omitempty"`
	Preview        []PreviewMessage `json:"preview,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// PreviewMessage is a short excerpt used in import previews.
type PreviewMessage struct {
	Role    string `json:"role"`
	Excerpt string `json:"excerpt"`
}
