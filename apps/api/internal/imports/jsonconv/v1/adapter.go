// Package jsonconvv1 parses generic JSON conversations:
//
//	{"title": "...", "messages": [{"role": "user", "content": "..."}]}
//	[{"role": "user", "content": "..."}, ...]            (OpenAI chat format)
//	{"conversations": [{"title": ..., "messages": [...]}, ...]}
//	[{"title": ..., "messages": [...]}, ...]
//
// Content may be a string, an array of {"type": "text", "text": ...} parts,
// or an object with "text"; Gemini-API style "parts" are accepted too.
package jsonconvv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// Adapter identity.
const (
	Name     = "json/v1"
	Provider = "json"
)

// Adapter implements imports.Adapter for generic JSON conversations.
type Adapter struct{}

// New returns the generic JSON adapter.
func New() *Adapter { return &Adapter{} }

// Name implements imports.Adapter.
func (*Adapter) Name() string { return Name }

// Provider implements imports.Adapter.
func (*Adapter) Provider() string { return Provider }

// Detect implements imports.Adapter. Shapes owned by the ChatGPT, Claude and
// Gemini adapters score low so those adapters win.
func (*Adapter) Detect(in imports.Input) float64 {
	h := imports.Head(in.Data)
	if len(h) == 0 || (h[0] != '[' && h[0] != '{') {
		return 0
	}
	if imports.HasJSONKey(h, "mapping") || imports.HasJSONKey(h, "chat_messages") ||
		imports.HasJSONKey(h, "safeHtmlItem") ||
		(imports.HasJSONKey(h, "header") && (imports.HasJSONKey(h, "titleUrl") || imports.HasJSONKey(h, "products"))) {
		return 0.1
	}
	hasRole := imports.HasJSONKey(h, "role") || imports.HasJSONKey(h, "sender") || imports.HasJSONKey(h, "author")
	hasContent := imports.HasJSONKey(h, "content") || imports.HasJSONKey(h, "text") || imports.HasJSONKey(h, "parts")
	switch {
	case hasRole && hasContent:
		return 0.8
	case imports.HasJSONKey(h, "messages"):
		return 0.6
	case imports.HasExt(in.Filename, ".json"):
		return 0.3
	}
	return 0.2
}

// Parse implements imports.Adapter.
func (*Adapter) Parse(ctx context.Context, in imports.Input) (*imports.ParseResult, error) {
	data := bytes.TrimSpace(imports.NormalizeEncoding(in.Data))
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty JSON", imports.ErrEmptyInput)
	}
	var top any
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON: %v", imports.ErrUnsupportedFormat, err)
	}
	items, err := conversationItems(top)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", imports.ErrUnsupportedFormat, err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: JSON contains no conversations", imports.ErrEmptyInput)
	}
	res := &imports.ParseResult{Adapter: Name, Provider: Provider}
	for i, item := range items {
		if i%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		conv, err := convert(item)
		if err != nil {
			res.Failures = append(res.Failures, imports.ItemFailure{Index: i, ExternalID: conv.ExternalID, Title: conv.Title, Error: err.Error()})
			continue
		}
		conv.URL = in.URL
		res.Conversations = append(res.Conversations, conv)
	}
	imports.SanitizeResult(res)
	return res, nil
}

// conversationItems returns the conversation-shaped values in top. Each item
// is either an object with "messages" or a bare array of messages.
func conversationItems(top any) ([]any, error) {
	switch v := top.(type) {
	case []any:
		if len(v) == 0 {
			return nil, nil
		}
		if isConversationObject(v[0]) {
			return v, nil
		}
		return []any{v}, nil // a bare list of messages
	case map[string]any:
		if convs, ok := v["conversations"].([]any); ok {
			return convs, nil
		}
		if _, ok := v["messages"]; ok {
			return []any{v}, nil
		}
		return nil, errors.New(`JSON object has neither "messages" nor "conversations"`)
	}
	return nil, errors.New("JSON must be an object or an array")
}

func isConversationObject(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, ok = m["messages"]
	return ok
}

func convert(item any) (imports.NormalizedConversation, error) {
	conv := imports.NormalizedConversation{Kind: imports.KindConversation, Provider: Provider, Adapter: Name}
	var msgs []any
	switch v := item.(type) {
	case []any:
		msgs = v
	case map[string]any:
		conv.Title = firstString(v, "title", "name", "subject")
		conv.ExternalID = firstString(v, "id", "conversation_id", "uuid")
		conv.CreatedAt = firstTime(v, "created_at", "create_time", "createdAt", "timestamp")
		conv.UpdatedAt = firstTime(v, "updated_at", "update_time", "updatedAt")
		list, ok := v["messages"].([]any)
		if !ok {
			return conv, errors.New(`"messages" is not an array`)
		}
		msgs = list
	default:
		return conv, errors.New("conversation is not an object or array")
	}
	skipped := map[string]int{}
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			skipped["non-object"]++
			continue
		}
		roleName := messageRole(m)
		role := normalizeRole(roleName)
		if role == "" {
			skipped[roleName]++
			continue
		}
		text := messageContent(m)
		if strings.TrimSpace(text) == "" {
			continue
		}
		conv.Messages = append(conv.Messages, imports.NormalizedMessage{
			ExternalID: firstString(m, "id", "uuid"),
			Role:       role,
			Content:    text,
			CreatedAt:  firstTime(m, "created_at", "create_time", "createdAt", "timestamp", "time"),
			Model:      firstString(m, "model"),
		})
	}
	kinds := make([]string, 0, len(skipped))
	for k := range skipped {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		label := k
		if label == "" {
			label = "missing"
		}
		conv.Warnings = append(conv.Warnings, fmt.Sprintf("skipped %d message(s) with role %q", skipped[k], label))
	}
	if len(conv.Messages) == 0 {
		return conv, errors.New("conversation has no visible messages")
	}
	return conv, nil
}

func messageRole(m map[string]any) string {
	for _, k := range []string{"role", "sender", "author", "from", "speaker"} {
		switch v := m[k].(type) {
		case string:
			return strings.ToLower(strings.TrimSpace(v))
		case map[string]any: // {"author": {"role": "user"}}
			if r, ok := v["role"].(string); ok {
				return strings.ToLower(strings.TrimSpace(r))
			}
		}
	}
	return ""
}

func normalizeRole(r string) string {
	switch r {
	case "user", "human", "me", "customer":
		return imports.RoleUser
	case "assistant", "ai", "model", "bot", "chatbot", "gpt", "chatgpt", "claude", "gemini":
		return imports.RoleAssistant
	case "system", "developer":
		return imports.RoleSystem
	}
	return "" // tool, function and unknown roles are skipped
}

func messageContent(m map[string]any) string {
	for _, k := range []string{"content", "text", "message", "body", "parts"} {
		if v, ok := m[k]; ok && v != nil {
			if s := renderContent(v, 0); strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

// renderContent flattens string / parts-array / {"text"} content.
func renderContent(v any, depth int) string {
	if depth > 8 {
		return ""
	}
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var segs []string
		for _, p := range c {
			if s := strings.TrimSpace(renderContent(p, depth+1)); s != "" {
				segs = append(segs, s)
			}
		}
		return strings.Join(segs, "\n\n")
	case map[string]any:
		typ, _ := c["type"].(string)
		switch typ {
		case "image_url", "image", "input_image":
			return "[image]"
		case "", "text", "input_text", "output_text":
			if s, ok := c["text"].(string); ok {
				return s
			}
			if s, ok := c["content"]; ok {
				return renderContent(s, depth+1)
			}
		}
	}
	return ""
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return v
			}
		case float64:
			return fmt.Sprintf("%.0f", v)
		}
	}
	return ""
}

func firstTime(m map[string]any, keys ...string) *time.Time {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if t := imports.ParseTime(v); t != nil {
				return t
			}
		case float64:
			if t := imports.UnixSeconds(v); t != nil {
				return t
			}
		}
	}
	return nil
}
