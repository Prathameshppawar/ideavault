package chatgptv1

import (
	"encoding/json"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// Conversation is one conversation object of a ChatGPT data export. Share
// pages (chatgpt/v2) carry the same shape plus LinearConversation.
type Conversation struct {
	ID               string            `json:"id"`
	ConversationID   string            `json:"conversation_id"`
	Title            string            `json:"title"`
	CreateTime       imports.Timestamp `json:"create_time"`
	UpdateTime       imports.Timestamp `json:"update_time"`
	CurrentNode      string            `json:"current_node"`
	DefaultModelSlug string            `json:"default_model_slug"`
	// Mapping is node id → raw node. Nodes are decoded one by one so a single
	// malformed node cannot fail the whole conversation.
	Mapping map[string]json.RawMessage `json:"mapping"`
	// LinearConversation is the ordered active thread (share pages only).
	LinearConversation []json.RawMessage `json:"linear_conversation,omitempty"`
}

// Node is one entry of a conversation's message tree.
type Node struct {
	ID       string   `json:"id"`
	Message  *Message `json:"message"`
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
}

// Message is a ChatGPT message. Only fields IdeaVault uses are declared.
type Message struct {
	ID         string            `json:"id"`
	Author     Author            `json:"author"`
	CreateTime imports.Timestamp `json:"create_time"`
	Content    Content           `json:"content"`
	Recipient  string            `json:"recipient"`
	Metadata   Metadata          `json:"metadata"`
}

// Author identifies who produced a message.
type Author struct {
	Role string `json:"role"` // user | assistant | system | tool
	Name string `json:"name"`
}

// Content is a message body. Its shape depends on ContentType.
type Content struct {
	ContentType string            `json:"content_type"`
	Parts       []json.RawMessage `json:"parts"`
	// Text holds the body of "code" (and some other) content types. It is raw
	// so unexpected types do not fail decoding.
	Text json.RawMessage `json:"text"`
}

// Metadata carries the per-message flags used for visibility decisions.
type Metadata struct {
	ModelSlug           string `json:"model_slug"`
	IsVisuallyHidden    bool   `json:"is_visually_hidden_from_conversation"`
	IsThinkingPreamble  bool   `json:"is_thinking_preamble_message"`
	IsUserSystemMessage bool   `json:"is_user_system_message"`
	// ContentReferences describe citation/entity markers inside the text.
	// They are decoded leniently via References.
	ContentReferences []json.RawMessage `json:"content_references"`
}

// ContentReference maps a private-use marker in message text to its
// rendering (citations, entities, ...).
type ContentReference struct {
	MatchedText string    `json:"matched_text"`
	Type        string    `json:"type"`
	Alt         string    `json:"alt"`
	Items       []RefItem `json:"items"`
}

// RefItem is one cited source.
type RefItem struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// References decodes the message's content references, skipping malformed ones.
func (m Metadata) References() []ContentReference {
	refs := make([]ContentReference, 0, len(m.ContentReferences))
	for _, raw := range m.ContentReferences {
		var ref ContentReference
		if json.Unmarshal(raw, &ref) == nil && ref.MatchedText != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

// DecodeNode decodes one raw mapping/linear_conversation node. When the
// message is malformed it still returns the node's tree links (id, parent,
// children) with a nil Message, together with the decoding error.
func DecodeNode(raw json.RawMessage) (Node, error) {
	var n Node
	err := json.Unmarshal(raw, &n)
	if err == nil {
		return n, nil
	}
	var skeleton struct {
		ID       json.RawMessage   `json:"id"`
		Parent   json.RawMessage   `json:"parent"`
		Children []json.RawMessage `json:"children"`
	}
	if json.Unmarshal(raw, &skeleton) != nil {
		return Node{}, err
	}
	n = Node{ID: rawString(skeleton.ID), Parent: rawString(skeleton.Parent)}
	for _, c := range skeleton.Children {
		if s := rawString(c); s != "" {
			n.Children = append(n.Children, s)
		}
	}
	return n, err
}

// rawString returns raw as a Go string when it is a JSON string, else "".
func rawString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}
