// Package claudev1 parses the Claude.ai data export (conversations.json).
//
// Each conversation has a flat list of chat_messages. Message bodies are
// taken from content blocks of type "text"; thinking, tool_use and
// tool_result blocks are never imported.
package claudev1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// Adapter identity.
const (
	Name     = "claude/v1"
	Provider = "claude"
)

// Adapter implements imports.Adapter for Claude.ai exports.
type Adapter struct{}

// New returns the Claude export adapter.
func New() *Adapter { return &Adapter{} }

// Name implements imports.Adapter.
func (*Adapter) Name() string { return Name }

// Provider implements imports.Adapter.
func (*Adapter) Provider() string { return Provider }

// Detect implements imports.Adapter: JSON with "chat_messages".
func (*Adapter) Detect(in imports.Input) float64 {
	h := imports.Head(in.Data)
	if len(h) == 0 || (h[0] != '[' && h[0] != '{') {
		return 0
	}
	if !imports.HasJSONKey(h, "chat_messages") {
		return 0
	}
	if imports.HasJSONKey(h, "sender") || imports.HasJSONKey(h, "uuid") {
		return 0.95
	}
	return 0.8
}

// Conversation is one conversation of a Claude export.
type Conversation struct {
	UUID         string            `json:"uuid"`
	Name         string            `json:"name"`
	CreatedAt    imports.Timestamp `json:"created_at"`
	UpdatedAt    imports.Timestamp `json:"updated_at"`
	ChatMessages []json.RawMessage `json:"chat_messages"`
}

// ChatMessage is one message of a Claude conversation.
type ChatMessage struct {
	UUID        string            `json:"uuid"`
	Sender      string            `json:"sender"` // human | assistant
	Text        string            `json:"text"`
	CreatedAt   imports.Timestamp `json:"created_at"`
	Content     []ContentBlock    `json:"content"`
	Attachments []Attachment      `json:"attachments"`
	Files       []Attachment      `json:"files"`
}

// ContentBlock is one block of a message's content.
type ContentBlock struct {
	Type string          `json:"type"` // text | thinking | tool_use | tool_result | ...
	Text json.RawMessage `json:"text"`
}

// Attachment is a file attached to a message. Only its name is imported.
type Attachment struct {
	FileName string `json:"file_name"`
}

// Parse implements imports.Adapter. A malformed conversation becomes an
// ItemFailure; it never fails the whole export.
func (*Adapter) Parse(ctx context.Context, in imports.Input) (*imports.ParseResult, error) {
	raws, err := splitConversations(imports.NormalizeEncoding(in.Data))
	if err != nil {
		return nil, fmt.Errorf("%w: claude export: %v", imports.ErrUnsupportedFormat, err)
	}
	if len(raws) == 0 {
		return nil, fmt.Errorf("%w: claude export contains no conversations", imports.ErrEmptyInput)
	}
	res := &imports.ParseResult{Adapter: Name, Provider: Provider}
	for i, raw := range raws {
		if i%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		var c Conversation
		if err := json.Unmarshal(raw, &c); err != nil {
			res.Failures = append(res.Failures, imports.ItemFailure{Index: i, Error: "malformed conversation: " + err.Error()})
			continue
		}
		conv := convert(c)
		if len(conv.Messages) == 0 {
			res.Failures = append(res.Failures, imports.ItemFailure{Index: i, ExternalID: conv.ExternalID, Title: conv.Title, Error: "conversation has no visible messages"})
			continue
		}
		res.Conversations = append(res.Conversations, conv)
	}
	imports.SanitizeResult(res)
	return res, nil
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

func convert(c Conversation) imports.NormalizedConversation {
	conv := imports.NormalizedConversation{
		ExternalID: c.UUID,
		Title:      c.Name,
		Kind:       imports.KindConversation,
		Provider:   Provider,
		Adapter:    Name,
		CreatedAt:  c.CreatedAt.Ptr(),
		UpdatedAt:  c.UpdatedAt.Ptr(),
	}
	if safeID.MatchString(c.UUID) {
		conv.URL = "https://claude.ai/chat/" + c.UUID
	}
	bad, unknownSender := 0, 0
	for _, raw := range c.ChatMessages {
		var m ChatMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			bad++
			continue
		}
		var role string
		switch strings.ToLower(m.Sender) {
		case "human", "user":
			role = imports.RoleUser
		case "assistant":
			role = imports.RoleAssistant
		default:
			unknownSender++
			continue
		}
		text := messageText(m)
		if text == "" {
			continue
		}
		conv.Messages = append(conv.Messages, imports.NormalizedMessage{
			ExternalID: m.UUID,
			Role:       role,
			Content:    text,
			CreatedAt:  m.CreatedAt.Ptr(),
		})
	}
	if bad > 0 {
		conv.Warnings = append(conv.Warnings, fmt.Sprintf("skipped %d malformed message(s)", bad))
	}
	if unknownSender > 0 {
		conv.Warnings = append(conv.Warnings, fmt.Sprintf("skipped %d message(s) with unknown sender", unknownSender))
	}
	return conv
}

// unsupportedBlock is the placeholder Claude's export puts in "text" for
// blocks it cannot render (tool use, artifacts...).
var unsupportedBlock = regexp.MustCompile("(?s)\\s*```\\s*This block is not supported on your current device yet\\.?\\s*```\\s*")

// messageText prefers the concatenated "text" content blocks and falls back
// to the legacy top-level text field when there are no content blocks.
// Attachment and file names are appended as placeholders.
func messageText(m ChatMessage) string {
	var parts []string
	for _, b := range m.Content {
		if b.Type != "text" {
			continue // thinking, tool_use, tool_result, ... are never imported
		}
		var s string
		if json.Unmarshal(b.Text, &s) == nil && strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	text := strings.Join(parts, "\n\n")
	if len(m.Content) == 0 {
		text = strings.TrimSpace(unsupportedBlock.ReplaceAllString(m.Text, "\n\n"))
	}
	var refs []string
	seen := map[string]bool{}
	for _, a := range append(m.Attachments, m.Files...) {
		if name := strings.TrimSpace(a.FileName); name != "" && !seen[name] {
			seen[name] = true
			refs = append(refs, "[attachment: "+name+"]")
		}
	}
	if len(refs) > 0 {
		if text != "" {
			text += "\n\n"
		}
		text += strings.Join(refs, "\n")
	}
	return strings.TrimSpace(text)
}

// splitConversations accepts a JSON array of conversations or a single
// conversation object.
func splitConversations(data []byte) ([]json.RawMessage, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	switch data[0] {
	case '[':
		var raws []json.RawMessage
		if err := json.Unmarshal(data, &raws); err != nil {
			return nil, err
		}
		return raws, nil
	case '{':
		if !json.Valid(data) {
			return nil, errors.New("invalid JSON object")
		}
		return []json.RawMessage{json.RawMessage(data)}, nil
	}
	return nil, errors.New("not a JSON array or object")
}
