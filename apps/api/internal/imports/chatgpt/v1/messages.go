package chatgptv1

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// hiddenContentTypes are never shown to the user in ChatGPT and never imported:
// reasoning, memory/context blobs, browsing internals, tool output, errors.
var hiddenContentTypes = map[string]bool{
	"thoughts":                true,
	"reasoning_recap":         true,
	"model_editable_context":  true,
	"user_editable_context":   true,
	"tether_browsing_display": true,
	"tether_quote":            true,
	"execution_output":        true,
	"system_error":            true,
	"computer_output":         true,
	"super_widget":            true,
}

// MessagesFromNodes converts nodes (already in conversation order) into the
// visible normalized messages, applying ChatGPT's visibility rules:
//
//   - only user and assistant turns (plus visible, non-empty system turns) are
//     kept; tool messages and tool invocations (recipient other than "all")
//     are dropped, as are hidden and "thinking preamble" messages;
//   - only text, multimodal_text and code content is rendered; reasoning and
//     context content types are skipped;
//   - citation/entity markup is resolved (see CleanMarkup);
//   - consecutive assistant messages of one turn are merged.
//
// defaultModel is used for assistant messages that carry no model slug. The
// returned warnings describe skipped, unsupported content.
func MessagesFromNodes(nodes []Node, defaultModel string) ([]imports.NormalizedMessage, []string) {
	var out []imports.NormalizedMessage
	unsupported := map[string]int{}
	for _, n := range nodes {
		m := n.Message
		if m == nil {
			continue
		}
		role, ok := visibleRole(m)
		if !ok {
			continue
		}
		text, known := renderContent(m.Content)
		if !known {
			unsupported[m.Content.ContentType]++
			continue
		}
		text = strings.TrimSpace(CleanMarkup(text, m.Metadata.References()))
		if text == "" {
			continue
		}
		model := ""
		if role == imports.RoleAssistant {
			model = m.Metadata.ModelSlug
			if model == "" {
				model = defaultModel
			}
		}
		id := m.ID
		if id == "" {
			id = n.ID
		}
		if role == imports.RoleAssistant && len(out) > 0 && out[len(out)-1].Role == imports.RoleAssistant {
			last := &out[len(out)-1]
			last.Content += "\n\n" + text
			if last.Model == "" {
				last.Model = model
			}
			continue
		}
		out = append(out, imports.NormalizedMessage{
			ExternalID: id,
			Role:       role,
			Content:    text,
			CreatedAt:  m.CreateTime.Ptr(),
			Model:      model,
		})
	}
	var warnings []string
	types := make([]string, 0, len(unsupported))
	for t := range unsupported {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		warnings = append(warnings, fmt.Sprintf("skipped %d message(s) with unsupported content type %q", unsupported[t], t))
	}
	return out, warnings
}

// visibleRole reports the normalized role of m and whether m is visible.
func visibleRole(m *Message) (string, bool) {
	if m.Metadata.IsVisuallyHidden || m.Metadata.IsThinkingPreamble {
		return "", false
	}
	if r := m.Recipient; r != "" && r != "all" {
		return "", false // tool invocation (web, web.run, python, browser, ...)
	}
	if hiddenContentTypes[m.Content.ContentType] {
		return "", false
	}
	switch m.Author.Role {
	case "user":
		return imports.RoleUser, true
	case "assistant":
		return imports.RoleAssistant, true
	case "system":
		// Custom-instruction system messages are context, not conversation.
		return imports.RoleSystem, !m.Metadata.IsUserSystemMessage
	}
	return "", false // tool and unknown roles
}

// renderContent returns the visible text of c and whether its content type
// is supported.
func renderContent(c Content) (string, bool) {
	switch c.ContentType {
	case "text", "multimodal_text", "":
		return renderParts(c.Parts), true
	case "code":
		return rawString(c.Text), true
	}
	return "", false
}

// renderParts joins string parts and replaces non-text parts (images,
// audio, files) with short placeholders.
func renderParts(parts []json.RawMessage) string {
	segs := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		if p[0] == '"' {
			if s := rawString(p); strings.TrimSpace(s) != "" {
				segs = append(segs, s)
			}
			continue
		}
		var obj struct {
			ContentType string          `json:"content_type"`
			Text        json.RawMessage `json:"text"`
		}
		if p[0] != '{' || json.Unmarshal(p, &obj) != nil {
			continue
		}
		switch obj.ContentType {
		case "image_asset_pointer":
			segs = append(segs, "[image]")
		case "audio_transcription":
			if s := rawString(obj.Text); strings.TrimSpace(s) != "" {
				segs = append(segs, s)
			}
		case "audio_asset_pointer", "real_time_user_audio_video_asset_pointer":
			// The transcription part carries the words; the audio itself is not imported.
		case "video_container_asset_pointer":
			segs = append(segs, "[video]")
		default:
			if s := rawString(obj.Text); strings.TrimSpace(s) != "" {
				segs = append(segs, s)
			} else {
				segs = append(segs, "[attachment]")
			}
		}
	}
	return strings.Join(segs, "\n\n")
}
