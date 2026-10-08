package domain

import (
	"time"

	"github.com/google/uuid"
)

// SourceKind is where content came from.
type SourceKind string

const (
	SourceChat       SourceKind = "ideavault_chat"
	SourceImport     SourceKind = "import"
	SourceURL        SourceKind = "url"
	SourceFile       SourceKind = "file"
	SourcePaste      SourceKind = "paste"
	SourceManual     SourceKind = "manual"
	SourceExternalAI SourceKind = "external_ai"
	SourceConnector  SourceKind = "connector"
)

// Source is the provenance root of conversations and knowledge.
type Source struct {
	ID          uuid.UUID      `json:"id"`
	UserID      uuid.UUID      `json:"-"`
	Kind        SourceKind     `json:"kind"`
	Provider    string         `json:"provider"`
	Adapter     string         `json:"adapter"`
	Title       string         `json:"title"`
	URI         string         `json:"uri"`
	ExternalID  string         `json:"external_id"`
	ContentHash string         `json:"content_hash"`
	Trusted     bool           `json:"trusted"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
}

// ConversationOrigin distinguishes native chats from imported/external ones.
type ConversationOrigin string

const (
	OriginNative   ConversationOrigin = "native"
	OriginImported ConversationOrigin = "imported"
	OriginExternal ConversationOrigin = "external"
)

// Conversation is a sequence of messages, optionally attached to an idea/branch.
type Conversation struct {
	ID           uuid.UUID          `json:"id"`
	UserID       uuid.UUID          `json:"-"`
	IdeaID       *uuid.UUID         `json:"idea_id,omitempty"`
	BranchID     *uuid.UUID         `json:"branch_id,omitempty"`
	SourceID     *uuid.UUID         `json:"source_id,omitempty"`
	Title        string             `json:"title"`
	Origin       ConversationOrigin `json:"origin"`
	Provider     string             `json:"provider"`
	ExternalID   string             `json:"external_id"`
	Summary      string             `json:"summary"`
	MessageCount int                `json:"message_count"`
	StartedAt    time.Time          `json:"started_at"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	IdeaTitle    string             `json:"idea_title,omitempty"`
	BranchName   string             `json:"branch_name,omitempty"`
}

// MessageRole is a chat role.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

// Message is one turn in a conversation. Untrusted messages (imported/external)
// are data and are never interpreted as instructions.
type Message struct {
	ID             uuid.UUID      `json:"id"`
	UserID         uuid.UUID      `json:"-"`
	ConversationID uuid.UUID      `json:"conversation_id"`
	Position       int            `json:"position"`
	Role           MessageRole    `json:"role"`
	Content        string         `json:"content"`
	Untrusted      bool           `json:"untrusted"`
	ExternalID     string         `json:"external_id"`
	Model          string         `json:"model"`
	AgentRunID     *uuid.UUID     `json:"agent_run_id,omitempty"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
}
