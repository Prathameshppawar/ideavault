package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	chatgptv2 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/chatgpt/v2"
	claudev1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/claude/v1"
)

// Platforms named in URL errors.
const (
	PlatformChatGPT = "ChatGPT"
	PlatformClaude  = "Claude"
	PlatformGemini  = "Gemini"
)

// URLKind classifies an import URL.
type URLKind int

// URL kinds recognized by ClassifyURL.
const (
	URLGeneric      URLKind = iota // any other public page or file
	URLChatGPTShare                // chatgpt.com/share/<id>: server-rendered, parsed by chatgpt/v2
	URLClaudeShare                 // claude.ai/share/<id>: client-rendered, best effort
	URLGeminiShare                 // gemini.google.com/share/<id>, g.co/gemini/share/<id>: client-rendered, best effort
	URLPrivate                     // a logged-in-only conversation (chatgpt.com/c/<id>, claude.ai/chat/<id>, ...)
)

// PrivateURLError explains why a private conversation URL cannot be
// imported. It wraps imports.ErrPrivateURL.
type PrivateURLError struct {
	Platform string
	URL      string
}

// Error implements error.
func (e *PrivateURLError) Error() string {
	return imports.ErrPrivateURL.Error() + ": " + e.Help()
}

// Unwrap makes errors.Is(err, imports.ErrPrivateURL) true.
func (e *PrivateURLError) Unwrap() error { return imports.ErrPrivateURL }

// Help returns user-facing guidance for importing the conversation.
func (e *PrivateURLError) Help() string {
	msg := fmt.Sprintf("this %s link opens a private conversation that only your logged-in account can see. "+
		"IdeaVault cannot access private conversations and will never ask for your password or session cookies.", e.Platform)
	switch e.Platform {
	case PlatformChatGPT:
		return msg + " To import it, open the conversation in ChatGPT, choose Share → Copy link and paste the public chatgpt.com/share/... link, " +
			"or upload your official data export (Settings → Data controls → Export data)."
	case PlatformClaude:
		return msg + " To import it, upload your official Claude data export (Settings → Privacy → Export data). " +
			"Claude share links are rendered in the browser, so the export is the reliable option."
	case PlatformGemini:
		return msg + " To import it, export your Gemini Apps activity with Google Takeout (My Activity → Gemini Apps) " +
			"and upload the Takeout zip or MyActivity.json."
	}
	return msg + " Use the platform's public share link or its official data export instead."
}

// ClassifyURL reports what kind of import URL u is and, for AI platforms,
// which platform it belongs to.
func ClassifyURL(u *url.URL) (URLKind, string) {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	switch host {
	case "chatgpt.com", "chat.openai.com":
		if hasPair(segs, "share") {
			return URLChatGPTShare, PlatformChatGPT
		}
		if hasPair(segs, "c") {
			return URLPrivate, PlatformChatGPT
		}
	case "claude.ai":
		if hasPair(segs, "share") {
			return URLClaudeShare, PlatformClaude
		}
		if hasPair(segs, "chat") || hasPair(segs, "project") {
			return URLPrivate, PlatformClaude
		}
	case "gemini.google.com", "bard.google.com":
		if hasPair(segs, "share") {
			return URLGeminiShare, PlatformGemini
		}
		if hasSeg(segs, "app") || hasSeg(segs, "gem") || hasSeg(segs, "chat") {
			return URLPrivate, PlatformGemini
		}
	case "g.co":
		if len(segs) >= 3 && strings.EqualFold(segs[0], "gemini") && strings.EqualFold(segs[1], "share") {
			return URLGeminiShare, PlatformGemini
		}
	}
	return URLGeneric, ""
}

// hasPair reports whether a path has segment key followed by a non-empty segment.
func hasPair(segs []string, key string) bool {
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == key {
			return true
		}
	}
	return false
}

func hasSeg(segs []string, key string) bool {
	for _, s := range segs {
		if s == key {
			return true
		}
	}
	return false
}

// ParseImportURL validates a user-supplied URL: http(s) only, a host, no
// embedded credentials. A missing scheme defaults to https.
func ParseImportURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: empty URL", imports.ErrInvalidURL)
	}
	if !strings.Contains(raw, "://") && !strings.Contains(raw, ":") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", imports.ErrInvalidURL, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: only http and https URLs can be imported", imports.ErrInvalidURL)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("%w: URL has no host", imports.ErrInvalidURL)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: URLs with embedded credentials are not accepted", imports.ErrInvalidURL)
	}
	return u, nil
}

// ParseURL imports a URL. ChatGPT share links are fetched and parsed by
// chatgpt/v2; private conversation links fail with a *PrivateURLError
// (wrapping imports.ErrPrivateURL) without any network access; Claude and
// Gemini share pages are fetched but, being client-rendered, usually yield a
// clear error recommending the official export. Any other http(s) URL is
// fetched and parsed by the detected adapter (web/v1 for ordinary pages).
// The fetcher is responsible for SSRF protection.
func (r *Registry) ParseURL(ctx context.Context, fetcher imports.Fetcher, rawURL string) (*imports.ParseResult, error) {
	u, err := ParseImportURL(rawURL)
	if err != nil {
		return nil, err
	}
	kind, platform := ClassifyURL(u)
	if kind == URLPrivate {
		return nil, &PrivateURLError{Platform: platform, URL: u.String()}
	}
	if fetcher == nil {
		return nil, errors.New("imports: no fetcher configured for URL imports")
	}
	body, contentType, finalURL, err := fetcher.Fetch(ctx, u.String())
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", u.Host, err)
	}
	if len(body) > MaxInputBytes {
		return nil, fmt.Errorf("%w: fetched page exceeds the %d MiB limit", imports.ErrTooLarge, MaxInputBytes>>20)
	}
	final := u
	if finalURL != "" {
		if fu, err := url.Parse(finalURL); err == nil && fu.Host != "" {
			final = fu
		}
	}
	// Redirects can land somewhere else entirely (shorteners, login walls).
	finalKind, finalPlatform := ClassifyURL(final)
	switch {
	case finalKind == URLPrivate:
		return nil, &PrivateURLError{Platform: finalPlatform, URL: final.String()}
	case kind == URLChatGPTShare && finalKind != URLChatGPTShare:
		return nil, fmt.Errorf("%w: the ChatGPT share link redirected away from the shared conversation; it may have been deleted or made private", imports.ErrUnsupportedFormat)
	case kind == URLGeneric:
		kind, platform = finalKind, finalPlatform
	}

	in := imports.Input{URL: final.String(), ContentType: contentType, Data: body, Filename: fileName(final)}
	var res *imports.ParseResult
	switch kind {
	case URLChatGPTShare:
		res, err = r.Parse(ctx, in, chatgptv2.Name)
		if err != nil {
			return nil, fmt.Errorf("%w: could not read a conversation from this ChatGPT share page (%v); check that the link is still shared publicly, or upload your ChatGPT data export instead",
				imports.ErrUnsupportedFormat, err)
		}
	case URLClaudeShare, URLGeminiShare:
		res, err = r.parseClientRenderedShare(ctx, in, platform)
		if err != nil {
			return nil, err
		}
	default:
		res, err = r.Parse(ctx, in, "")
		if err != nil {
			return nil, err
		}
	}
	for i := range res.Conversations {
		if res.Conversations[i].URL == "" {
			res.Conversations[i].URL = in.URL
		}
	}
	return res, nil
}

// parseClientRenderedShare looks for conversation data embedded in a Claude
// or Gemini share page. These pages are rendered in the browser, so usually
// nothing is found and a clear error is returned; content is never guessed
// from page chrome.
func (r *Registry) parseClientRenderedShare(ctx context.Context, in imports.Input, platform string) (*imports.ParseResult, error) {
	if raw := findEmbeddedClaudeConversation(in.Data); raw != nil {
		res, err := r.Parse(ctx, imports.Input{URL: in.URL, Data: raw, ContentType: "application/json"}, claudev1.Name)
		if err == nil && len(res.Conversations) > 0 {
			for i := range res.Conversations {
				res.Conversations[i].URL = in.URL // the public share link, not the private chat URL
			}
			return res, nil
		}
	}
	help := "Export your data from Claude (Settings → Privacy → Export data) and upload the zip or conversations.json instead."
	if platform == PlatformGemini {
		help = "Export your Gemini Apps activity with Google Takeout (My Activity → Gemini Apps) and upload the zip or MyActivity.json instead."
	}
	return nil, fmt.Errorf("%w: could not extract a conversation from this %s share page because its content is rendered in the browser. %s",
		imports.ErrUnsupportedFormat, platform, help)
}

// findEmbeddedClaudeConversation searches JSON <script> blocks for an object
// shaped like a Claude conversation (with "chat_messages") and returns it
// re-encoded, or nil.
func findEmbeddedClaudeConversation(page []byte) []byte {
	if !bytes.Contains(page, []byte("chat_messages")) {
		return nil
	}
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil
	}
	var found []byte
	var visit func(*html.Node, int)
	visit = func(n *html.Node, depth int) {
		if found != nil || depth > 512 {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Script && n.FirstChild != nil {
			text := n.FirstChild.Data
			if strings.Contains(text, "chat_messages") {
				var v any
				if json.Unmarshal([]byte(strings.TrimSpace(text)), &v) == nil {
					if obj := findChatMessages(v, 0); obj != nil {
						found, _ = json.Marshal(obj)
					}
				}
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c, depth+1)
		}
	}
	visit(doc, 0)
	return found
}

func findChatMessages(v any, depth int) map[string]any {
	if depth > 64 {
		return nil
	}
	switch x := v.(type) {
	case map[string]any:
		if msgs, ok := x["chat_messages"].([]any); ok && len(msgs) > 0 {
			return x
		}
		for _, c := range x {
			if m := findChatMessages(c, depth+1); m != nil {
				return m
			}
		}
	case []any:
		for _, c := range x {
			if m := findChatMessages(c, depth+1); m != nil {
				return m
			}
		}
	}
	return nil
}

// fileName returns the last path segment of u when it has an extension, so
// URL imports of .md/.json/.txt files are detected like uploads.
func fileName(u *url.URL) string {
	base := path.Base(u.Path)
	if base == "." || base == "/" || path.Ext(base) == "" {
		return ""
	}
	return base
}
