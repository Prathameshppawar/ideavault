// Package chatgptv2 parses public ChatGPT share pages
// (https://chatgpt.com/share/<id>).
//
// Share pages embed the conversation in a React Router turbo-stream payload
// (window.__reactRouterContext.streamController.enqueue("...")); older pages
// used Next.js __NEXT_DATA__. The decoded data has the export shape of
// chatgpt/v1 plus "linear_conversation", the ordered active thread, so the
// visibility rules and markup cleanup of chatgpt/v1 are reused.
package chatgptv2

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	chatgptv1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/chatgpt/v1"
)

// Adapter identity.
const (
	Name     = "chatgpt/v2"
	Provider = "chatgpt"
)

// Adapter implements imports.Adapter for ChatGPT share pages.
type Adapter struct{}

// New returns the ChatGPT share-page adapter.
func New() *Adapter { return &Adapter{} }

// Name implements imports.Adapter.
func (*Adapter) Name() string { return Name }

// Provider implements imports.Adapter.
func (*Adapter) Provider() string { return Provider }

// Detect implements imports.Adapter: HTML embedding a share payload.
func (*Adapter) Detect(in imports.Input) float64 {
	data := in.Data
	if !imports.LooksLikeHTML(data) && !bytes.Contains(imports.Head(data), []byte("<script")) {
		return 0
	}
	hasConv := bytes.Contains(data, []byte("linear_conversation")) || bytes.Contains(data, []byte("routes/share"))
	switch {
	case bytes.Contains(data, []byte(enqueueCall)) && hasConv:
		return 0.95
	case bytes.Contains(data, []byte(nextDataID)) && bytes.Contains(data, []byte("serverResponse")) &&
		(hasConv || bytes.Contains(data, []byte(`"mapping"`))):
		return 0.9
	case IsShareURL(in.URL):
		return 0.6
	}
	return 0
}

// Parse implements imports.Adapter.
func (*Adapter) Parse(ctx context.Context, in imports.Input) (*imports.ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := extractShare(string(imports.NormalizeEncoding(in.Data)))
	if err != nil {
		return nil, fmt.Errorf("%w: chatgpt share page: %v", imports.ErrUnsupportedFormat, err)
	}
	conv, err := chatgptv1.Normalize(payload.conv, Name, Provider)
	if err != nil {
		return nil, fmt.Errorf("%w: chatgpt share page: %v", imports.ErrUnsupportedFormat, err)
	}
	switch {
	case IsShareURL(in.URL):
		conv.URL = in.URL
	case shareIDPattern.MatchString(payload.shareID):
		conv.URL = "https://chatgpt.com/share/" + payload.shareID
	}
	if payload.shareID != "" {
		conv.ExternalID = payload.shareID
	}
	res := &imports.ParseResult{Adapter: Name, Provider: Provider, Conversations: []imports.NormalizedConversation{conv}}
	imports.SanitizeResult(res)
	return res, nil
}

var shareIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// IsShareURL reports whether raw is a public ChatGPT share link
// (chatgpt.com/share/<id> or chat.openai.com/share/<id>).
func IsShareURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host != "chatgpt.com" && host != "chat.openai.com" {
		return false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "share" && segs[i+1] != "" {
			return true
		}
	}
	return false
}
