package chatgptv2

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

func TestParseSharePageFixture(t *testing.T) {
	data := fixtures.Read(t, "chatgpt", "share_page.html")
	res, err := New().Parse(context.Background(), imports.Input{Data: data})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Adapter != Name || res.Provider != Provider || len(res.Conversations) != 1 {
		t.Fatalf("result = %+v", res)
	}
	c := res.Conversations[0]
	if c.Title != "Weekend ride route voting" {
		t.Errorf("title = %q", c.Title)
	}
	if c.ExternalID != "6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03" || c.URL != "https://chatgpt.com/share/6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03" {
		t.Errorf("id/url = %q / %q", c.ExternalID, c.URL)
	}
	if c.CreatedAt == nil || c.UpdatedAt == nil {
		t.Errorf("missing conversation timestamps")
	}
	want := []struct{ role, prefix string }{
		{imports.RoleUser, "We ride every weekend in a group of 15."},
		{imports.RoleAssistant, "Let riders vote on two or three candidate routes before the ride. ([Moto Club Handbook](https://example.net/route-voting))"},
		{imports.RoleUser, "Nice. What about riders with different skill levels?"},
		{imports.RoleAssistant, "Tag every route with a difficulty level"},
	}
	if len(c.Messages) != len(want) {
		for _, m := range c.Messages {
			t.Logf("%s: %q", m.Role, m.Content)
		}
		t.Fatalf("got %d messages, want %d", len(c.Messages), len(want))
	}
	for i, w := range want {
		m := c.Messages[i]
		if m.Role != w.role || !strings.HasPrefix(m.Content, w.prefix) {
			t.Errorf("message %d = %s %q, want %s %q...", i, m.Role, m.Content, w.role, w.prefix)
		}
		for _, banned := range []string{"PRIVATE REASONING", "TOOL OUTPUT", "MEMORY", "Looking at a few", "search_query", "Thought for"} {
			if strings.Contains(m.Content, banned) {
				t.Errorf("message %d leaks %q", i, banned)
			}
		}
		if strings.ContainsAny(m.Content, string([]rune{0xE200, 0xE201, 0xE202})) {
			t.Errorf("message %d keeps private-use markers", i)
		}
	}
	if c.Messages[1].Model != "gpt-5" || c.Messages[0].CreatedAt == nil {
		t.Errorf("model/timestamp not imported: %+v", c.Messages[1])
	}
}

func TestParseShareKeepsShareURL(t *testing.T) {
	data := fixtures.Read(t, "chatgpt", "share_page.html")
	in := imports.Input{Data: data, URL: "https://chatgpt.com/share/6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03?utm=x"}
	res, err := New().Parse(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Conversations[0].URL; got != in.URL {
		t.Errorf("URL = %q", got)
	}
}

func TestParseNextDataSharePage(t *testing.T) {
	page := `<html><head><title>x</title></head><body><script id="__NEXT_DATA__" type="application/json">` +
		`{"props":{"pageProps":{"sharedConversationId":"next-share-0001","serverResponse":{"data":{` +
		`"title":"Clinic waiting room","create_time":1767225600,"conversation_id":"next-share-0001",` +
		`"linear_conversation":[{"id":"r","children":["u"]},` +
		`{"id":"u","parent":"r","children":["a"],"message":{"id":"u","author":{"role":"user"},"content":{"content_type":"text","parts":["What should the display show?"]},"recipient":"all"}},` +
		`{"id":"a","parent":"u","children":[],"message":{"id":"a","author":{"role":"assistant"},"content":{"content_type":"text","parts":["Queue positions."]},"recipient":"all"}}]` +
		`}}}}}</script></body></html>`
	in := imports.Input{Data: []byte(page)}
	if c := New().Detect(in); c < 0.85 {
		t.Fatalf("Detect = %v", c)
	}
	res, err := New().Parse(context.Background(), in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := res.Conversations[0]
	if c.Title != "Clinic waiting room" || len(c.Messages) != 2 || c.URL != "https://chatgpt.com/share/next-share-0001" {
		t.Fatalf("conversation = %+v", c)
	}
}

func TestParseSharePageErrors(t *testing.T) {
	tests := []struct {
		name string
		page string
	}{
		{"no payload", `<html><body><p>Not found</p></body></html>`},
		{"garbage payload", `<html><script>x.streamController.enqueue("not json");</script></html>`},
		{"payload without conversation", `<html><script>x.streamController.enqueue("[{\"_1\":2},\"loaderData\",{}]\n");</script></html>`},
		{"empty linear conversation", `<html><script>x.streamController.enqueue("[{\"_1\":2},\"linear_conversation\",[]]\n");</script></html>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Parse(context.Background(), imports.Input{Data: []byte(tt.page)})
			if !errors.Is(err, imports.ErrUnsupportedFormat) {
				t.Errorf("err = %v, want ErrUnsupportedFormat", err)
			}
		})
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		in   imports.Input
		min  float64
		max  float64
	}{
		{"fixture", imports.Input{Data: fixtures.Read(t, "chatgpt", "share_page.html")}, 0.9, 1},
		{"plain page", imports.Input{Data: []byte("<html><body>hello</body></html>")}, 0, 0},
		{"plain page at share url", imports.Input{Data: []byte("<html><body>hello</body></html>"), URL: "https://chatgpt.com/share/abc"}, 0.5, 0.7},
		{"json export", imports.Input{Data: fixtures.Read(t, "chatgpt", "conversations.json")}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New().Detect(tt.in); got < tt.min || got > tt.max {
				t.Errorf("Detect = %v, want [%v, %v]", got, tt.min, tt.max)
			}
		})
	}
}

func TestIsShareURL(t *testing.T) {
	tests := map[string]bool{
		"https://chatgpt.com/share/6a1f0c2e":         true,
		"https://chat.openai.com/share/abc-123":      true,
		"https://www.chatgpt.com/share/e/abc":        true,
		"https://chatgpt.com/c/abc":                  false,
		"https://chatgpt.com/share/":                 false,
		"https://evil.example/chatgpt.com/share/abc": false,
		"ftp://chatgpt.com/share/abc":                false,
		"not a url":                                  false,
	}
	for raw, want := range tests {
		if got := IsShareURL(raw); got != want {
			t.Errorf("IsShareURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
