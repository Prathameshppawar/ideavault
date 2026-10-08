package markdownv1

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// roleWords are the speaker names recognized in role markers.
const roleWords = `user|human|me|you|q|question|assistant|ai|a|answer|claude|gemini|bard|chatgpt|gpt(?:-?[0-9][a-z0-9.\-]*)?|copilot|bot|system`

// markerPatterns recognize a role marker at the start of a line. Group 1 is
// the speaker; group 2 (when present) is text following the marker.
var markerPatterns = []struct {
	re     *regexp.Regexp
	quoted bool
}{
	// ChatGPT copy-paste: "You said:" / "ChatGPT said:".
	{re: regexp.MustCompile(`(?i)^\s*(you|user|chatgpt|claude|gemini|copilot|assistant)\s+said\s*:\s*(.*)$`)},
	// Headings: "## User", "### Assistant:", "## **User**".
	{re: regexp.MustCompile(`(?i)^\s*#{1,6}\s*(?:\*\*|__)?\s*(` + roleWords + `)\s*:?\s*(?:\*\*|__)?\s*:?\s*()$`)},
	// Bold: "**User:** text", "**Assistant**: text".
	{re: regexp.MustCompile(`(?i)^\s*(?:\*\*|__)\s*(` + roleWords + `)\s*:?\s*(?:\*\*|__)\s*:?\s*(.*)$`)},
	// Quoted bold: "> **User**".
	{re: regexp.MustCompile(`(?i)^\s*>\s*(?:\*\*|__)\s*(` + roleWords + `)\s*:?\s*(?:\*\*|__)\s*:?\s*(.*)$`), quoted: true},
	// Plain: "User: text", "Q: text", "AI: text".
	{re: regexp.MustCompile(`(?i)^\s*(` + roleWords + `)\s*:\s*(.*)$`)},
}

// noiseLines are UI chrome that copy-pasting from AI apps drags along.
var noiseLines = regexp.MustCompile(`(?i)^(?:chatgpt can make mistakes\..*|claude can make mistakes\..*|gemini can make mistakes.*|thought for \d+(?:\.\d+)?\s*(?:s|sec|seconds|m|min|minutes)|is this conversation helpful so far\?)$`)

var h1 = regexp.MustCompile(`^#\s+(.+?)\s*#*\s*$`)

func speakerRole(word string) string {
	w := strings.ToLower(word)
	switch {
	case w == "user" || w == "human" || w == "me" || w == "you" || w == "q" || w == "question":
		return imports.RoleUser
	case w == "system":
		return imports.RoleSystem
	default:
		return imports.RoleAssistant
	}
}

// matchMarker reports whether line is a role marker.
func matchMarker(line string) (role, rest string, quoted, ok bool) {
	if len(line) > 200 {
		return "", "", false, false // markers are short; skip long prose lines
	}
	for _, p := range markerPatterns {
		if m := p.re.FindStringSubmatch(line); m != nil {
			rest := ""
			if len(m) > 2 {
				rest = m[2]
			}
			return speakerRole(m[1]), strings.TrimSpace(rest), p.quoted, true
		}
	}
	return "", "", false, false
}

type segment struct {
	role   string
	quoted bool
	lines  []string
}

// parseTranscript splits text into role segments. Text is a conversation
// when it has at least two markers including both a user and an assistant
// marker; otherwise it is a document (one user message with all the text).
func parseTranscript(text string) imports.NormalizedConversation {
	lines := strings.Split(text, "\n")
	var segs []segment
	var preamble []string
	inFence := false
	title := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		} else if !inFence {
			if title == "" {
				if m := h1.FindStringSubmatch(trimmed); m != nil {
					title = m[1]
				}
			}
			if role, rest, quoted, ok := matchMarker(line); ok {
				seg := segment{role: role, quoted: quoted}
				if rest != "" {
					seg.lines = append(seg.lines, rest)
				}
				segs = append(segs, seg)
				continue
			}
		}
		if len(segs) == 0 {
			preamble = append(preamble, line)
		} else {
			segs[len(segs)-1].lines = append(segs[len(segs)-1].lines, line)
		}
	}

	if !isConversation(segs) {
		return imports.NormalizedConversation{
			Title:    firstNonEmpty(title, imports.TitleFromText(text, imports.DefaultTitleRunes)),
			Kind:     imports.KindDocument,
			Messages: []imports.NormalizedMessage{{Role: imports.RoleUser, Content: strings.TrimSpace(text)}},
		}
	}

	conv := imports.NormalizedConversation{Title: title, Kind: imports.KindConversation}
	for _, s := range segs {
		content := segmentContent(s)
		if content == "" {
			continue
		}
		conv.Messages = append(conv.Messages, imports.NormalizedMessage{Role: s.role, Content: content})
	}
	if conv.Title == "" {
		for _, m := range conv.Messages {
			if m.Role == imports.RoleUser {
				conv.Title = imports.TitleFromText(m.Content, imports.DefaultTitleRunes)
				break
			}
		}
	}
	if dropped := countContentLines(preamble, title); dropped > 0 {
		conv.Warnings = append(conv.Warnings, fmt.Sprintf("ignored %d line(s) before the first role marker", dropped))
	}
	return conv
}

func isConversation(segs []segment) bool {
	if len(segs) < 2 {
		return false
	}
	user, assistant := false, false
	for _, s := range segs {
		user = user || s.role == imports.RoleUser
		assistant = assistant || s.role == imports.RoleAssistant
	}
	return user && assistant
}

// segmentContent joins a segment's lines, dropping UI noise and, for quoted
// segments ("> **User**"), the blockquote prefix.
func segmentContent(s segment) string {
	lines := make([]string, 0, len(s.lines))
	for _, l := range s.lines {
		if noiseLines.MatchString(strings.TrimSpace(l)) {
			continue
		}
		lines = append(lines, l)
	}
	if s.quoted && allQuoted(lines) {
		for i, l := range lines {
			l = strings.TrimLeft(l, " \t")
			l = strings.TrimPrefix(l, ">")
			lines[i] = strings.TrimPrefix(l, " ")
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func allQuoted(lines []string) bool {
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, ">") {
			return false
		}
	}
	return true
}

// countContentLines counts non-empty preamble lines other than the title heading.
func countContentLines(lines []string, title string) int {
	n := 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || noiseLines.MatchString(t) {
			continue
		}
		if m := h1.FindStringSubmatch(t); m != nil && m[1] == title {
			continue
		}
		n++
	}
	return n
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
