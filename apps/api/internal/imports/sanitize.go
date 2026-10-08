package imports

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Content limits applied to every normalized conversation.
const (
	// MaxMessageBytes caps a single message's content. Longer content is
	// truncated (with a conversation warning) so one pasted blob cannot
	// overwhelm storage, embedding or analysis downstream.
	MaxMessageBytes = 2 << 20
	// MaxTitleRunes caps conversation titles.
	MaxTitleRunes = 300
	// DefaultTitleRunes is the length used when a title is derived from content.
	DefaultTitleRunes = 80
	// UntitledConversation is the fallback title when nothing better exists.
	UntitledConversation = "Untitled conversation"
)

// SanitizeText makes text safe to store: it drops invalid UTF-8 and NUL bytes
// (PostgreSQL rejects NUL in text columns), normalizes CRLF/CR line endings to
// LF, removes other C0 control characters except tab/newline, and strips the
// U+E200–U+E202 private-use markers ChatGPT uses for citations (adapters
// should already have resolved them; this is a safety net).
func SanitizeText(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToValidUTF8(s, "")
	if strings.ContainsRune(s, '\r') {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	clean := true
	for _, r := range s {
		if isStrippedRune(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isStrippedRune(r) {
			return -1
		}
		return r
	}, s)
}

func isStrippedRune(r rune) bool {
	switch {
	case r == '\t' || r == '\n':
		return false
	case r < 0x20 || r == 0x7f:
		return true
	case r >= 0xE200 && r <= 0xE202:
		return true
	case r == 0xFEFF: // stray byte-order marks
		return true
	}
	return false
}

// Truncate shortens s to at most maxRunes runes, appending "…" when cut.
// It never splits a multi-byte character.
func Truncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	if maxRunes == 1 {
		return "…"
	}
	n := 0
	for i := range s {
		if n == maxRunes-1 {
			return strings.TrimRightFunc(s[:i], unicode.IsSpace) + "…"
		}
		n++
	}
	return s
}

// truncateBytes cuts s to at most maxBytes bytes on a rune boundary.
func truncateBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// TitleFromText derives a short title from the first non-empty line of text,
// stripping common Markdown decoration (#, >, list bullets, emphasis).
func TitleFromText(text string, maxRunes int) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#>*-+ \t")
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "__", "")
		line = strings.Trim(line, "`_ \t")
		line = strings.Join(strings.Fields(line), " ")
		if line != "" && line != "```" {
			return Truncate(line, maxRunes)
		}
	}
	return ""
}

// NormalizeRole maps a role to one of the normalized roles, or "" if unknown.
func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleUser:
		return RoleUser
	case RoleAssistant:
		return RoleAssistant
	case RoleSystem:
		return RoleSystem
	}
	return ""
}

// FinalizeConversation sanitizes every string field, drops messages that are
// empty or have an unknown role, truncates oversized content, and fills in
// defaults (Kind, Title). It is idempotent.
func FinalizeConversation(c *NormalizedConversation) {
	c.ExternalID = strings.TrimSpace(SanitizeText(c.ExternalID))
	c.URL = strings.TrimSpace(SanitizeText(c.URL))
	if c.Kind != KindDocument {
		c.Kind = KindConversation
	}
	msgs := c.Messages[:0]
	dropped := 0
	for _, m := range c.Messages {
		m.Role = NormalizeRole(m.Role)
		m.ExternalID = strings.TrimSpace(SanitizeText(m.ExternalID))
		m.Model = strings.TrimSpace(SanitizeText(m.Model))
		m.Content = strings.TrimSpace(SanitizeText(m.Content))
		if m.Role == "" {
			dropped++
			continue
		}
		if m.Content == "" {
			continue
		}
		if len(m.Content) > MaxMessageBytes {
			m.Content = truncateBytes(m.Content, MaxMessageBytes) + "\n\n[truncated]"
			c.Warnings = appendUnique(c.Warnings, fmt.Sprintf("message content truncated to %d bytes", MaxMessageBytes))
		}
		msgs = append(msgs, m)
	}
	c.Messages = msgs
	if dropped > 0 {
		c.Warnings = appendUnique(c.Warnings, fmt.Sprintf("skipped %d message(s) with unknown roles", dropped))
	}
	title := strings.Join(strings.Fields(SanitizeText(c.Title)), " ")
	if title == "" {
		for _, m := range c.Messages {
			if m.Role == RoleUser {
				title = TitleFromText(m.Content, DefaultTitleRunes)
				break
			}
		}
	}
	if title == "" {
		title = UntitledConversation
	}
	c.Title = Truncate(title, MaxTitleRunes)
	for i, w := range c.Warnings {
		c.Warnings[i] = SanitizeText(w)
	}
}

// SanitizeResult finalizes every conversation of a parse result. Conversations
// left without any visible message are moved to Failures so callers never
// persist empty conversations. It is idempotent.
func SanitizeResult(res *ParseResult) {
	if res == nil {
		return
	}
	kept := res.Conversations[:0]
	for i := range res.Conversations {
		c := res.Conversations[i]
		FinalizeConversation(&c)
		if len(c.Messages) == 0 {
			res.Failures = append(res.Failures, ItemFailure{
				Index:      i,
				ExternalID: c.ExternalID,
				Title:      c.Title,
				Error:      "conversation has no visible messages",
			})
			continue
		}
		kept = append(kept, c)
	}
	res.Conversations = kept
	for i := range res.Failures {
		f := &res.Failures[i]
		f.ExternalID = SanitizeText(f.ExternalID)
		f.Title = Truncate(SanitizeText(f.Title), MaxTitleRunes)
		f.Error = SanitizeText(f.Error)
	}
	for i, w := range res.Warnings {
		res.Warnings[i] = SanitizeText(w)
	}
}

// NormalizeEncoding strips a UTF-8 byte-order mark and transcodes UTF-16
// (detected by its BOM) to UTF-8. Other input is returned unchanged.
func NormalizeEncoding(data []byte) []byte {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return utf16ToUTF8(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return utf16ToUTF8(data[2:], true)
	}
	return data
}

func utf16ToUTF8(b []byte, bigEndian bool) []byte {
	units := make([]uint16, len(b)/2)
	for i := range units {
		if bigEndian {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			units[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return []byte(string(utf16.Decode(units)))
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}
