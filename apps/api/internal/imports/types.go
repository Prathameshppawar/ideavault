// Package imports turns external conversation exports (ChatGPT, Claude, Gemini,
// Markdown, JSON, plain text, public share links) into normalized conversations.
//
// Everything produced here is UNTRUSTED DATA. Nothing in an imported conversation
// is ever treated as an instruction to IdeaVault's agent.
package imports

import (
	"context"
	"errors"
	"time"
)

// Message roles in normalized conversations.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// Conversation kinds. Documents (web pages, role-less notes) are stored as a
// single-message conversation whose only message has role "user".
const (
	KindConversation = "conversation"
	KindDocument     = "document"
)

// NormalizedMessage is one visible turn of an imported conversation.
type NormalizedMessage struct {
	ExternalID string     `json:"external_id,omitempty"`
	Role       string     `json:"role"` // user | assistant | system
	Content    string     `json:"content"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	Model      string     `json:"model,omitempty"`
}

// NormalizedConversation is the adapter-independent representation of one conversation.
type NormalizedConversation struct {
	ExternalID string              `json:"external_id,omitempty"`
	Title      string              `json:"title"`
	Kind       string              `json:"kind"`     // conversation | document
	Provider   string              `json:"provider"` // chatgpt | claude | gemini | markdown | json | text | web
	Adapter    string              `json:"adapter"`  // e.g. "chatgpt/v1"
	URL        string              `json:"url,omitempty"`
	CreatedAt  *time.Time          `json:"created_at,omitempty"`
	UpdatedAt  *time.Time          `json:"updated_at,omitempty"`
	Messages   []NormalizedMessage `json:"messages"`
	// Warnings are non-fatal parse notes (skipped parts, unknown content types...).
	Warnings []string `json:"warnings,omitempty"`
}

// ParseResult is what an adapter returns for one input.
type ParseResult struct {
	Adapter       string                   `json:"adapter"`
	Provider      string                   `json:"provider"`
	Conversations []NormalizedConversation `json:"conversations"`
	// Failures records conversations that could not be parsed (partial success).
	Failures []ItemFailure `json:"failures,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
}

// ItemFailure describes a single conversation that failed to parse.
type ItemFailure struct {
	Index      int    `json:"index"`
	ExternalID string `json:"external_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Error      string `json:"error"`
}

// Input is raw content handed to adapters.
type Input struct {
	Filename    string // may be empty for pastes
	ContentType string // may be empty
	Data        []byte
	URL         string // set for URL imports (the final, fetched URL)
}

// Adapter parses one input format. Adapters are versioned ("chatgpt/v1").
type Adapter interface {
	// Name is the versioned adapter id, e.g. "chatgpt/v1".
	Name() string
	// Provider is the source family, e.g. "chatgpt".
	Provider() string
	// Detect returns a confidence in [0,1] that this adapter can parse the input.
	Detect(in Input) float64
	// Parse converts the input into normalized conversations.
	Parse(ctx context.Context, in Input) (*ParseResult, error)
}

// Fetcher retrieves public URLs. Implementations MUST enforce SSRF protections
// (public addresses only, size limits, timeouts). See security.SafeHTTPClient.
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) (body []byte, contentType string, finalURL string, err error)
}

// Sentinel errors.
var (
	ErrUnsupportedFormat = errors.New("unsupported import format")
	ErrEmptyInput        = errors.New("import input is empty")
	// ErrPrivateURL is returned for private AI-conversation URLs that IdeaVault
	// cannot (and must not pretend to) access, e.g. chatgpt.com/c/<id>.
	ErrPrivateURL = errors.New("private conversation URL")
	ErrTooLarge   = errors.New("import input too large")
	// ErrInvalidURL is returned for malformed URLs and non-http(s) schemes.
	ErrInvalidURL = errors.New("invalid import URL")
)
