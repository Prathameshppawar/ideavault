package chatgptv2

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// TestRealSharePage parses a real downloaded share page. Real pages contain
// personal content and are never committed; point IDEAVAULT_REAL_SHARE_HTML
// at a local copy to run it.
func TestRealSharePage(t *testing.T) {
	path := os.Getenv("IDEAVAULT_REAL_SHARE_HTML")
	if path == "" {
		t.Skip("IDEAVAULT_REAL_SHARE_HTML not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	a := New()
	if c := a.Detect(imports.Input{Data: data}); c < 0.9 {
		t.Fatalf("Detect = %v, want >= 0.9", c)
	}
	res, err := a.Parse(context.Background(), imports.Input{Data: data, URL: "https://chatgpt.com/share/example"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Conversations) != 1 {
		t.Fatalf("got %d conversations, want 1", len(res.Conversations))
	}
	conv := res.Conversations[0]
	if strings.TrimSpace(conv.Title) == "" {
		t.Error("the share page's title was not extracted")
	}
	if n := os.Getenv("IDEAVAULT_REAL_SHARE_MESSAGES"); n != "" && fmt.Sprint(len(conv.Messages)) != n {
		t.Fatalf("got %d messages, want %s", len(conv.Messages), n)
	}
	if len(conv.Messages) < 2 {
		t.Fatalf("got %d messages, want a user/assistant exchange", len(conv.Messages))
	}
	for i, m := range conv.Messages {
		want := imports.RoleUser
		if i%2 == 1 {
			want = imports.RoleAssistant
		}
		if m.Role != want {
			t.Errorf("message %d role = %q, want %q", i, m.Role, want)
		}
		if strings.TrimSpace(m.Content) == "" {
			t.Errorf("message %d is empty", i)
		}
		if strings.ContainsAny(m.Content, string([]rune{0xE200, 0xE201, 0xE202})) {
			t.Errorf("message %d still contains private-use citation markers", i)
		}
		if m.CreatedAt == nil {
			t.Errorf("message %d has no timestamp", i)
		}
	}
	if conv.Messages[1].Model == "" {
		t.Errorf("assistant message has no model slug")
	}
}
