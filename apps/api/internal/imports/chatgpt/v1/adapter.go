// Package chatgptv1 parses the official ChatGPT data export (conversations.json).
//
// Each conversation is a tree ("mapping") of message nodes; edits and
// regenerations create branches. The adapter imports only the active branch
// (current_node up to the root) and only what ChatGPT shows the user.
package chatgptv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// Adapter identity.
const (
	Name     = "chatgpt/v1"
	Provider = "chatgpt"
)

// ErrNoVisibleMessages is returned by Normalize for conversations without any
// visible user/assistant content.
var ErrNoVisibleMessages = errors.New("conversation has no visible messages")

// Adapter implements imports.Adapter for ChatGPT data exports.
type Adapter struct{}

// New returns the ChatGPT export adapter.
func New() *Adapter { return &Adapter{} }

// Name implements imports.Adapter.
func (*Adapter) Name() string { return Name }

// Provider implements imports.Adapter.
func (*Adapter) Provider() string { return Provider }

// Detect implements imports.Adapter: JSON with a "mapping" of message nodes.
func (*Adapter) Detect(in imports.Input) float64 {
	h := imports.Head(in.Data)
	if len(h) == 0 || (h[0] != '[' && h[0] != '{') {
		return 0
	}
	if !imports.HasJSONKey(h, "mapping") {
		return 0
	}
	if imports.HasJSONKey(h, "current_node") || imports.HasJSONKey(h, "author") {
		return 0.95
	}
	return 0.7
}

// Parse implements imports.Adapter. A malformed conversation becomes an
// ItemFailure; it never fails the whole export.
func (*Adapter) Parse(ctx context.Context, in imports.Input) (*imports.ParseResult, error) {
	raws, err := SplitConversations(imports.NormalizeEncoding(in.Data))
	if err != nil {
		return nil, fmt.Errorf("%w: chatgpt export: %v", imports.ErrUnsupportedFormat, err)
	}
	if len(raws) == 0 {
		return nil, fmt.Errorf("%w: chatgpt export contains no conversations", imports.ErrEmptyInput)
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
			id, title := peekIdentity(raw)
			res.Failures = append(res.Failures, imports.ItemFailure{Index: i, ExternalID: id, Title: title, Error: "malformed conversation: " + err.Error()})
			continue
		}
		conv, err := Normalize(c, Name, Provider)
		if err != nil {
			res.Failures = append(res.Failures, imports.ItemFailure{Index: i, ExternalID: conv.ExternalID, Title: conv.Title, Error: err.Error()})
			continue
		}
		if safeID.MatchString(conv.ExternalID) {
			conv.URL = "https://chatgpt.com/c/" + conv.ExternalID
		}
		res.Conversations = append(res.Conversations, conv)
	}
	imports.SanitizeResult(res)
	return res, nil
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// Normalize converts one ChatGPT conversation into a NormalizedConversation
// labelled with adapterName/provider. It uses c.LinearConversation when it
// contains messages (share pages) and otherwise reconstructs the active
// thread from c.Mapping.
func Normalize(c Conversation, adapterName, provider string) (imports.NormalizedConversation, error) {
	conv := imports.NormalizedConversation{
		ExternalID: c.ConversationID,
		Title:      c.Title,
		Kind:       imports.KindConversation,
		Provider:   provider,
		Adapter:    adapterName,
		CreatedAt:  c.CreateTime.Ptr(),
		UpdatedAt:  c.UpdateTime.Ptr(),
	}
	if conv.ExternalID == "" {
		conv.ExternalID = c.ID
	}
	var nodes []Node
	bad := 0
	for _, raw := range c.LinearConversation {
		n, err := DecodeNode(raw)
		if err != nil {
			bad++
		}
		nodes = append(nodes, n)
	}
	if !hasMessages(nodes) {
		if len(c.Mapping) == 0 {
			return conv, errors.New("conversation has no message mapping")
		}
		decoded, badMapping := decodeMapping(c.Mapping)
		bad = badMapping
		nodes = ActiveThread(decoded, c.CurrentNode)
	}
	if bad > 0 {
		conv.Warnings = append(conv.Warnings, fmt.Sprintf("skipped %d malformed message node(s)", bad))
	}
	msgs, warnings := MessagesFromNodes(nodes, c.DefaultModelSlug)
	conv.Warnings = append(conv.Warnings, warnings...)
	conv.Messages = msgs
	if len(msgs) == 0 {
		return conv, ErrNoVisibleMessages
	}
	return conv, nil
}

func hasMessages(nodes []Node) bool {
	for _, n := range nodes {
		if n.Message != nil {
			return true
		}
	}
	return false
}

// SplitConversations splits an export into raw conversation objects. It
// accepts a JSON array of conversations, an object with a "conversations"
// array, or a single conversation object.
func SplitConversations(data []byte) ([]json.RawMessage, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var raws []json.RawMessage
	switch data[0] {
	case '[':
		if err := json.Unmarshal(data, &raws); err != nil {
			return nil, err
		}
		return raws, nil
	case '{':
		var wrapper struct {
			Conversations []json.RawMessage `json:"conversations"`
			Mapping       json.RawMessage   `json:"mapping"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			return nil, err
		}
		if wrapper.Conversations != nil && wrapper.Mapping == nil {
			return wrapper.Conversations, nil
		}
		return []json.RawMessage{json.RawMessage(data)}, nil
	}
	return nil, errors.New("not a JSON array or object")
}

// peekIdentity best-effort extracts the id and title of a malformed conversation.
func peekIdentity(raw json.RawMessage) (id, title string) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return "", ""
	}
	id = rawString(fields["conversation_id"])
	if id == "" {
		id = rawString(fields["id"])
	}
	return id, rawString(fields["title"])
}
