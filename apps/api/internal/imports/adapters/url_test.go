package adapters

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

type fakePage struct {
	body  []byte
	ct    string
	final string
	err   error
}

// fakeFetcher serves canned pages by URL and records every fetch.
type fakeFetcher struct {
	pages map[string]fakePage
	calls []string
}

var errFakeNotFound = errors.New("fake: not found")

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string) ([]byte, string, string, error) {
	f.calls = append(f.calls, rawURL)
	p, ok := f.pages[rawURL]
	if !ok {
		return nil, "", "", errFakeNotFound
	}
	if p.err != nil {
		return nil, "", "", p.err
	}
	final := p.final
	if final == "" {
		final = rawURL
	}
	return p.body, p.ct, final, nil
}

const (
	shareURL   = "https://chatgpt.com/share/6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03"
	clientPage = `<!doctype html><html><head><title>Claude</title></head><body><div id="root"></div><script src="/app.js"></script></body></html>`
)

func TestParseURLRouting(t *testing.T) {
	share := fixtures.Read(t, "chatgpt", "share_page.html")
	export := fixtures.Read(t, "chatgpt", "conversations.json")
	claudeEmbedded := `<html><body><script type="application/json" id="__NEXT_DATA__">{"props":{"pageProps":{"conversation":` +
		`{"uuid":"shared-claude-01","name":"Shared clinic chat","chat_messages":[` +
		`{"sender":"human","text":"Which module first?"},{"sender":"assistant","text":"Appointments."}]}}}}</script></body></html>`
	f := &fakeFetcher{pages: map[string]fakePage{
		shareURL: {body: share, ct: "text/html"},
		"https://chat.openai.com/share/6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03": {body: share, ct: "text/html"},
		"https://claude.ai/share/abc-embedded":                               {body: []byte(claudeEmbedded), ct: "text/html"},
		"https://blog.example.com/group-rides": {body: []byte(`<html><head><title>Group rides</title></head><body><article><p>Ride in staggered formation.</p></article></body></html>`),
			ct: "text/html; charset=utf-8"},
		"https://files.example.com/export/conversations.json": {body: export, ct: "application/json"},
		"https://raw.example.com/notes.md":                    {body: fixtures.Read(t, "markdown", "notes.md"), ct: "text/plain; charset=utf-8"},
		"https://short.example/share":                         {body: share, ct: "text/html", final: shareURL},
		"https://files.example.com/export.zip":                {body: zipOf(t, map[string][]byte{"conversations.json": export}), ct: "application/zip"},
	}}
	tests := []struct {
		name    string
		url     string
		adapter string
		title   string
		kind    string
		wantURL string
	}{
		{"chatgpt share", shareURL, "chatgpt/v2", "Weekend ride route voting", imports.KindConversation, shareURL},
		{"chat.openai.com share", "https://chat.openai.com/share/6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03", "chatgpt/v2", "Weekend ride route voting", imports.KindConversation, "https://chat.openai.com/share/6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03"},
		{"share without scheme", "chatgpt.com/share/6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03", "chatgpt/v2", "Weekend ride route voting", imports.KindConversation, shareURL},
		{"shortener to share", "https://short.example/share", "chatgpt/v2", "Weekend ride route voting", imports.KindConversation, shareURL},
		{"claude share with embedded data", "https://claude.ai/share/abc-embedded", "claude/v1", "Shared clinic chat", imports.KindConversation, "https://claude.ai/share/abc-embedded"},
		{"web page", "https://blog.example.com/group-rides", "web/v1", "Group rides", imports.KindDocument, "https://blog.example.com/group-rides"},
		{"export json by url", "https://files.example.com/export/conversations.json", "chatgpt/v1", "Biker community trip platform", imports.KindConversation, "https://chatgpt.com/c/6a1f0c2e-1b2c-4d3e-9f10-aa11bb22cc01"},
		{"markdown by url", "https://raw.example.com/notes.md", "markdown/v1", "Clinic management platform: feature notes", imports.KindDocument, "https://raw.example.com/notes.md"},
		{"zip by url", "https://files.example.com/export.zip", "chatgpt/v1", "Biker community trip platform", imports.KindConversation, "https://chatgpt.com/c/6a1f0c2e-1b2c-4d3e-9f10-aa11bb22cc01"},
	}
	r := NewRegistry()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := r.ParseURL(context.Background(), f, tt.url)
			if err != nil {
				t.Fatalf("ParseURL: %v", err)
			}
			c := res.Conversations[0]
			if res.Adapter != tt.adapter || c.Title != tt.title || c.Kind != tt.kind || c.URL != tt.wantURL {
				t.Errorf("got adapter=%s title=%q kind=%s url=%q", res.Adapter, c.Title, c.Kind, c.URL)
			}
		})
	}
}

func TestParseURLPrivateConversations(t *testing.T) {
	tests := []struct {
		url      string
		platform string
		hint     string
	}{
		{"https://chatgpt.com/c/6a1f0c2e-1b2c-4d3e-9f10-aa11bb22cc01", PlatformChatGPT, "Share"},
		{"https://chat.openai.com/c/abc123", PlatformChatGPT, "Export data"},
		{"https://chatgpt.com/g/g-abc123-trip-helper/c/6a1f0c2e", PlatformChatGPT, "chatgpt.com/share"},
		{"http://www.chatgpt.com/c/abc", PlatformChatGPT, "Share"},
		{"https://claude.ai/chat/3f2a1b0c-0d1e-4f20-8a31-b2c3d4e5f601", PlatformClaude, "Export data"},
		{"https://claude.ai/project/abc", PlatformClaude, "Export data"},
		{"https://gemini.google.com/app/1a2b3c4d5e", PlatformGemini, "Takeout"},
		{"https://gemini.google.com/u/1/app/1a2b3c4d5e", PlatformGemini, "Takeout"},
		{"gemini.google.com/app/abc", PlatformGemini, "Takeout"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			f := &fakeFetcher{}
			_, err := NewRegistry().ParseURL(context.Background(), f, tt.url)
			if !errors.Is(err, imports.ErrPrivateURL) {
				t.Fatalf("err = %v, want ErrPrivateURL", err)
			}
			var pe *PrivateURLError
			if !errors.As(err, &pe) || pe.Platform != tt.platform {
				t.Fatalf("err = %#v, want *PrivateURLError for %s", err, tt.platform)
			}
			msg := err.Error()
			for _, want := range []string{"cannot access private conversations", "never ask for your password", tt.hint} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not mention %q", msg, want)
				}
			}
			if len(f.calls) != 0 {
				t.Errorf("private URL was fetched: %v", f.calls)
			}
		})
	}
}

func TestParseURLRedirectsToPrivate(t *testing.T) {
	f := &fakeFetcher{pages: map[string]fakePage{
		"https://short.example/x": {body: []byte("<html>login</html>"), final: "https://chatgpt.com/c/abc"},
	}}
	_, err := NewRegistry().ParseURL(context.Background(), f, "https://short.example/x")
	if !errors.Is(err, imports.ErrPrivateURL) {
		t.Fatalf("err = %v, want ErrPrivateURL", err)
	}
}

func TestParseURLClientRenderedShares(t *testing.T) {
	tests := []struct {
		url, hint string
	}{
		{"https://claude.ai/share/7f6e5d4c-share", "Settings → Privacy → Export data"},
		{"https://gemini.google.com/share/abc123", "Google Takeout"},
		{"https://g.co/gemini/share/abc123", "Google Takeout"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			f := &fakeFetcher{pages: map[string]fakePage{tt.url: {body: []byte(clientPage), ct: "text/html"}}}
			res, err := NewRegistry().ParseURL(context.Background(), f, tt.url)
			if !errors.Is(err, imports.ErrUnsupportedFormat) || res != nil {
				t.Fatalf("res=%v err=%v, want ErrUnsupportedFormat", res, err)
			}
			if !strings.Contains(err.Error(), "rendered in the browser") || !strings.Contains(err.Error(), tt.hint) {
				t.Errorf("message %q lacks guidance %q", err, tt.hint)
			}
			if len(f.calls) != 1 {
				t.Errorf("expected exactly one fetch attempt, got %v", f.calls)
			}
		})
	}
}

func TestParseURLErrors(t *testing.T) {
	fetchErr := errors.New("dial tcp: blocked private address")
	f := &fakeFetcher{pages: map[string]fakePage{
		"https://down.example/":                      {err: fetchErr},
		"https://huge.example/":                      {body: make([]byte, MaxInputBytes+1), ct: "text/plain"},
		"https://binary.example/":                    {body: []byte{0x89, 'P', 'N', 'G', 0, 1, 2}, ct: "image/png"},
		shareURL:                                     {body: []byte("<html>login</html>"), final: "https://chatgpt.com/auth/login"},
		"https://chatgpt.com/share/deleted-share-01": {body: []byte("<html><body>This shared link has been disabled.</body></html>"), ct: "text/html"},
	}}
	tests := []struct {
		name string
		url  string
		want error
	}{
		{"ftp", "ftp://example.com/file", imports.ErrInvalidURL},
		{"javascript", "javascript:alert(1)", imports.ErrInvalidURL},
		{"file", "file:///etc/passwd", imports.ErrInvalidURL},
		{"credentials", "https://user:pass@example.com/", imports.ErrInvalidURL},
		{"empty", "  ", imports.ErrInvalidURL},
		{"no host", "https:///path", imports.ErrInvalidURL},
		{"fetch error", "https://down.example/", fetchErr},
		{"too large", "https://huge.example/", imports.ErrTooLarge},
		{"binary", "https://binary.example/", imports.ErrUnsupportedFormat},
		{"share redirected to login", shareURL, imports.ErrUnsupportedFormat},
		{"share deleted", "https://chatgpt.com/share/deleted-share-01", imports.ErrUnsupportedFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRegistry().ParseURL(context.Background(), f, tt.url)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
	if _, err := NewRegistry().ParseURL(context.Background(), nil, "https://example.com/"); err == nil {
		t.Error("nil fetcher accepted")
	}
}

func TestClassifyURL(t *testing.T) {
	tests := map[string]URLKind{
		"https://chatgpt.com/share/abc":       URLChatGPTShare,
		"https://chatgpt.com/share/e/abc":     URLChatGPTShare,
		"https://chatgpt.com/c/abc":           URLPrivate,
		"https://chatgpt.com/g/g-x/c/abc":     URLPrivate,
		"https://chatgpt.com/":                URLGeneric,
		"https://claude.ai/share/abc":         URLClaudeShare,
		"https://claude.ai/chat/abc":          URLPrivate,
		"https://gemini.google.com/share/abc": URLGeminiShare,
		"https://g.co/gemini/share/abc":       URLGeminiShare,
		"https://gemini.google.com/app/abc":   URLPrivate,
		"https://g.co/other":                  URLGeneric,
		"https://example.com/share/abc":       URLGeneric,
		"https://example.com/c/abc":           URLGeneric,
	}
	for raw, want := range tests {
		u, err := ParseImportURL(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := ClassifyURL(u); got != want {
			t.Errorf("ClassifyURL(%s) = %v, want %v", raw, got, want)
		}
	}
}
