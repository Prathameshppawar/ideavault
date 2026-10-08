package markdownv1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

func parse(t *testing.T, a *Adapter, in imports.Input) imports.NormalizedConversation {
	t.Helper()
	res, err := a.Parse(context.Background(), in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Conversations) != 1 {
		t.Fatalf("got %d conversations", len(res.Conversations))
	}
	return res.Conversations[0]
}

func TestParseChatGPTCopyFixture(t *testing.T) {
	c := parse(t, New(), imports.Input{Filename: "chatgpt_copy.md", Data: fixtures.Read(t, "markdown", "chatgpt_copy.md")})
	if c.Kind != imports.KindConversation || c.Provider != Provider || c.Adapter != Name {
		t.Errorf("labels = %s %s %s", c.Kind, c.Provider, c.Adapter)
	}
	if c.Title != "Biker trip platform pricing" {
		t.Errorf("title = %q", c.Title)
	}
	roles := []string{imports.RoleUser, imports.RoleAssistant, imports.RoleUser, imports.RoleAssistant}
	if len(c.Messages) != len(roles) {
		for _, m := range c.Messages {
			t.Logf("%s: %q", m.Role, m.Content)
		}
		t.Fatalf("got %d messages, want %d", len(c.Messages), len(roles))
	}
	for i, r := range roles {
		if c.Messages[i].Role != r {
			t.Errorf("message %d role = %s, want %s", i, c.Messages[i].Role, r)
		}
	}
	first := c.Messages[1].Content
	if !strings.HasPrefix(first, "Charge the organizer, not the riders.") || strings.Contains(first, "Thought for") {
		t.Errorf("assistant message = %q", first)
	}
	last := c.Messages[3].Content
	if !strings.Contains(last, "# User: this comment is not a role marker") || !strings.HasSuffix(last, "```") {
		t.Errorf("code block not preserved: %q", last)
	}
	if strings.Contains(last, "can make mistakes") {
		t.Errorf("UI noise kept: %q", last)
	}
}

func TestParseNotesFixtureIsDocument(t *testing.T) {
	data := fixtures.Read(t, "markdown", "notes.md")
	c := parse(t, New(), imports.Input{Filename: "notes.md", Data: data})
	if c.Kind != imports.KindDocument || len(c.Messages) != 1 || c.Messages[0].Role != imports.RoleUser {
		t.Fatalf("conversation = %+v", c)
	}
	if c.Title != "Clinic management platform: feature notes" {
		t.Errorf("title = %q", c.Title)
	}
	if c.Messages[0].Content != strings.TrimSpace(string(data)) {
		t.Errorf("document content altered")
	}
}

func TestParseTextTranscriptFixture(t *testing.T) {
	c := parse(t, NewText(), imports.Input{Filename: "transcript.txt", Data: fixtures.Read(t, "text", "transcript.txt")})
	if c.Provider != TextProvider || c.Adapter != TextName || c.Kind != imports.KindConversation {
		t.Errorf("labels = %s %s %s", c.Provider, c.Adapter, c.Kind)
	}
	if len(c.Messages) != 4 {
		t.Fatalf("got %d messages", len(c.Messages))
	}
	if c.Messages[1].Content != "Keep it to three quick questions:\n1. How was the route?\n2. How was the pace?\n3. Would you ride with this crew again?" {
		t.Errorf("multi-line message = %q", c.Messages[1].Content)
	}
	if c.Messages[3].Role != imports.RoleAssistant || !strings.HasPrefix(c.Messages[3].Content, "Show aggregate") {
		t.Errorf("AI: marker = %+v", c.Messages[3])
	}
	if c.Title != "I want riders to rate each trip after it ends. What should we ask?" {
		t.Errorf("title = %q", c.Title)
	}
}

func TestRoleMarkerVariants(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		roles []string
		first string
	}{
		{"headings", "## User\nHow many riders?\n\n### Assistant\nFifteen.", []string{"user", "assistant"}, "How many riders?"},
		{"bold inline", "**User:** Price?\n**Assistant**: Six dollars.", []string{"user", "assistant"}, "Price?"},
		{"quoted bold", "> **User**\n> Which city?\n\n> **Assistant**\n> Pune first.", []string{"user", "assistant"}, "Which city?"},
		{"human claude", "Human: hi\n\nClaude: hello", []string{"user", "assistant"}, "hi"},
		{"q and a", "Q: Is it free?\nA: For organizers with three trips.", []string{"user", "assistant"}, "Is it free?"},
		{"me and gemini", "Me: idea?\nGemini: yes", []string{"user", "assistant"}, "idea?"},
		{"gpt model name", "You: hi\nGPT-4o: hello", []string{"user", "assistant"}, "hi"},
		{"you said", "You said:\nhi\nChatGPT said:\nhello", []string{"user", "assistant"}, "hi"},
		{"system kept", "System: be brief\nUser: hi\nAssistant: hello", []string{"system", "user", "assistant"}, "be brief"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := parse(t, New(), imports.Input{Data: []byte(tt.text)})
			if c.Kind != imports.KindConversation || len(c.Messages) != len(tt.roles) {
				t.Fatalf("conversation = %+v", c)
			}
			for i, r := range tt.roles {
				if c.Messages[i].Role != r {
					t.Errorf("message %d role = %s, want %s", i, c.Messages[i].Role, r)
				}
			}
			if c.Messages[0].Content != tt.first {
				t.Errorf("first content = %q, want %q", c.Messages[0].Content, tt.first)
			}
		})
	}
}

func TestDocumentsWithoutEnoughMarkers(t *testing.T) {
	for _, text := range []string{
		"User: only one marker here\nand some notes",
		"Assistant: one\nAI: two but no user",
		"Plain prose without any markers.\nSecond line.",
		"```\nUser: in code\nAssistant: in code\n```",
	} {
		c := parse(t, New(), imports.Input{Data: []byte(text)})
		if c.Kind != imports.KindDocument {
			t.Errorf("%q parsed as %s", text, c.Kind)
		}
	}
}

func TestPreambleWarning(t *testing.T) {
	c := parse(t, New(), imports.Input{Data: []byte("# Title\nSkip to content\n\nUser: hi\nAssistant: hello")})
	if c.Title != "Title" || len(c.Warnings) != 1 {
		t.Errorf("title=%q warnings=%v", c.Title, c.Warnings)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := New().Parse(context.Background(), imports.Input{Data: []byte(" \n\x00 ")}); !errors.Is(err, imports.ErrEmptyInput) {
		t.Errorf("err = %v", err)
	}
}

func TestDetect(t *testing.T) {
	md, txt := New(), NewText()
	tests := []struct {
		name            string
		in              imports.Input
		wantMD, wantTXT float64
	}{
		{"md file", imports.Input{Filename: "notes.md", Data: []byte("hello")}, 0.8, 0.5},
		{"txt file", imports.Input{Filename: "notes.txt", Data: []byte("hello")}, 0.5, 0.8},
		{"markdown media type", imports.Input{ContentType: "text/markdown", Data: []byte("x")}, 0.8, 0.5},
		{"paste with markdown", imports.Input{Data: []byte("# Title\n\n- item")}, 0.55, 0.5},
		{"plain paste", imports.Input{Data: []byte("just words")}, 0.5, 0.5},
		{"json paste", imports.Input{Data: []byte(`{"a": 1}`)}, 0, 0},
		{"html paste", imports.Input{Data: []byte("<!doctype html><html></html>")}, 0, 0},
		{"json file", imports.Input{Filename: "x.json", Data: []byte("not really json")}, 0, 0},
		{"binary", imports.Input{Data: []byte("PK\x03\x04\x00\x01\x02")}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := md.Detect(tt.in); got != tt.wantMD {
				t.Errorf("markdown Detect = %v, want %v", got, tt.wantMD)
			}
			if got := txt.Detect(tt.in); got != tt.wantTXT {
				t.Errorf("text Detect = %v, want %v", got, tt.wantTXT)
			}
		})
	}
}
