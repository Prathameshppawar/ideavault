package imports

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeText(t *testing.T) {
	pua := string([]rune{0xE200}) + "cite" + string([]rune{0xE202}) + "x" + string([]rune{0xE201})
	tests := []struct {
		name, in, want string
	}{
		{"plain", "hello", "hello"},
		{"nul bytes", "a\x00b\x00", "ab"},
		{"invalid utf8", "ok\xff\xfeok", "okok"},
		{"crlf", "a\r\nb\rc", "a\nb\nc"},
		{"controls", "a\x07b\x1bc\td\n", "abc\td\n"},
		{"private use markers", "see" + pua, "seecitex"},
		{"bom", "\ufeffhi", "hi"},
		{"unicode kept", "café – 🚀", "café – 🚀"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeText(tt.in)
			if got != tt.want {
				t.Errorf("SanitizeText(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !utf8.ValidString(got) || strings.ContainsRune(got, 0) {
				t.Errorf("result not clean: %q", got)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 6, "hello…"},
		{"héllo wörld", 3, "hé…"},
		{"abc", 1, "…"},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		if got := Truncate(tt.in, tt.n); got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestTitleFromText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"# My **Title**\nbody", "My Title"},
		{"\n\n  - first bullet  \nsecond", "first bullet"},
		{"> quoted   text", "quoted text"},
		{"```\ncode\n```", "code"},
		{"", ""},
		{strings.Repeat("word ", 40), strings.TrimSpace(Truncate(strings.TrimSpace(strings.Repeat("word ", 40)), 20))},
	}
	for _, tt := range tests {
		if got := TitleFromText(tt.in, 20); got != tt.want {
			t.Errorf("TitleFromText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFinalizeConversation(t *testing.T) {
	c := NormalizedConversation{
		Title: "  ",
		Messages: []NormalizedMessage{
			{Role: "user", Content: "  \x00 "},
			{Role: "ROBOT", Content: "unknown role"},
			{Role: "User", Content: "First question\nwith detail\x00"},
			{Role: "assistant", Content: strings.Repeat("x", MaxMessageBytes+10)},
		},
	}
	FinalizeConversation(&c)
	if c.Kind != KindConversation {
		t.Errorf("Kind = %q", c.Kind)
	}
	if c.Title != "First question" {
		t.Errorf("Title = %q", c.Title)
	}
	if len(c.Messages) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(c.Messages), c.Messages)
	}
	if c.Messages[0].Role != RoleUser || c.Messages[0].Content != "First question\nwith detail" {
		t.Errorf("message 0 = %+v", c.Messages[0])
	}
	if !strings.HasSuffix(c.Messages[1].Content, "[truncated]") || len(c.Messages[1].Content) > MaxMessageBytes+20 {
		t.Errorf("oversized message not truncated (len %d)", len(c.Messages[1].Content))
	}
	if len(c.Warnings) != 2 {
		t.Errorf("warnings = %v", c.Warnings)
	}
	// Idempotent.
	before := c.Messages[0]
	FinalizeConversation(&c)
	if c.Messages[0] != before || len(c.Warnings) != 2 {
		t.Errorf("FinalizeConversation is not idempotent")
	}

	empty := NormalizedConversation{Kind: KindDocument}
	FinalizeConversation(&empty)
	if empty.Title != UntitledConversation || empty.Kind != KindDocument {
		t.Errorf("empty conversation: %+v", empty)
	}
}

func TestSanitizeResultMovesEmptyConversationsToFailures(t *testing.T) {
	res := &ParseResult{Conversations: []NormalizedConversation{
		{ExternalID: "a", Title: "kept", Messages: []NormalizedMessage{{Role: RoleUser, Content: "hi"}}},
		{ExternalID: "b\x00", Title: "empty", Messages: []NormalizedMessage{{Role: RoleUser, Content: "   "}}},
	}}
	SanitizeResult(res)
	if len(res.Conversations) != 1 || res.Conversations[0].ExternalID != "a" {
		t.Fatalf("conversations = %+v", res.Conversations)
	}
	if len(res.Failures) != 1 || res.Failures[0].ExternalID != "b" || res.Failures[0].Index != 1 {
		t.Fatalf("failures = %+v", res.Failures)
	}
}

func TestNormalizeEncoding(t *testing.T) {
	utf16le := []byte{0xFF, 0xFE, 'h', 0, 'i', 0, 0xAC, 0x20} // "hi€"
	utf16be := []byte{0xFE, 0xFF, 0, 'h', 0, 'i', 0x20, 0xAC}
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"utf8 bom", []byte("\xEF\xBB\xBFhello"), "hello"},
		{"utf16le", utf16le, "hi€"},
		{"utf16be", utf16be, "hi€"},
		{"plain", []byte("plain"), "plain"},
	}
	for _, tt := range tests {
		if got := string(NormalizeEncoding(tt.in)); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
