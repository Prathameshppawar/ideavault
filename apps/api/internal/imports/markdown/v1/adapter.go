// Package markdownv1 parses transcripts copied from AI chat UIs and saved as
// Markdown or plain text ("You said:" / "ChatGPT said:", "User:" /
// "Assistant:", "## User", "**User:**", "> **User**", "Q:" / "A:" ...).
// Text without role markers is imported as a document.
//
// The same parser backs two adapters: markdown/v1 (provider "markdown") and
// text/v1 (provider "text").
package markdownv1

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// Adapter identities.
const (
	Name         = "markdown/v1"
	Provider     = "markdown"
	TextName     = "text/v1"
	TextProvider = "text"
)

// Adapter implements imports.Adapter for Markdown or plain-text transcripts.
type Adapter struct {
	name, provider string
	plain          bool
}

// New returns the Markdown adapter (markdown/v1).
func New() *Adapter { return &Adapter{name: Name, provider: Provider} }

// NewText returns the plain-text adapter (text/v1).
func NewText() *Adapter { return &Adapter{name: TextName, provider: TextProvider, plain: true} }

// Name implements imports.Adapter.
func (a *Adapter) Name() string { return a.name }

// Provider implements imports.Adapter.
func (a *Adapter) Provider() string { return a.provider }

var markdownSyntax = regexp.MustCompile("(?m)^(?:#{1,6} |\\s*[-*+] |\\s*\\d+\\. |```|> )|\\*\\*[^*\\n]+\\*\\*|\\[[^\\]\\n]+\\]\\([^)\\n]+\\)")

// Detect implements imports.Adapter. File extensions and media types decide
// first; unnamed pastes of readable text score just above the acceptance
// threshold so structured formats always win.
func (a *Adapter) Detect(in imports.Input) float64 {
	if a.plain {
		if imports.HasExt(in.Filename, ".txt", ".text", ".log") {
			return 0.8
		}
	} else {
		if imports.HasExt(in.Filename, ".md", ".markdown", ".mdown", ".mkd", ".mdx") {
			return 0.8
		}
		if imports.MediaTypeIs(in.ContentType, "text/markdown", "text/x-markdown") {
			return 0.8
		}
	}
	if !imports.LooksLikeText(in.Data) || imports.LooksLikeHTML(in.Data) {
		return 0
	}
	if imports.LooksLikeJSON(in.Data) && json.Valid(in.Data) {
		return 0
	}
	if in.Filename != "" && !imports.HasExt(in.Filename, "") &&
		!imports.HasExt(in.Filename, ".txt", ".text", ".log", ".md", ".markdown", ".mdown", ".mkd", ".mdx") {
		return 0 // some other file type (.json, .html, .csv ...)
	}
	head := imports.Head(in.Data)
	if len(head) > 64<<10 {
		head = head[:64<<10]
	}
	score := 0.5
	if !a.plain && markdownSyntax.Match(head) {
		score = 0.55
	}
	if a.plain && imports.MediaTypeIs(in.ContentType, "text/plain") {
		score = 0.55
	}
	return score
}

// Parse implements imports.Adapter.
func (a *Adapter) Parse(ctx context.Context, in imports.Input) (*imports.ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text := imports.SanitizeText(string(imports.NormalizeEncoding(in.Data)))
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: %s input has no text", imports.ErrEmptyInput, a.provider)
	}
	conv := parseTranscript(text)
	conv.Provider = a.provider
	conv.Adapter = a.name
	conv.URL = in.URL
	res := &imports.ParseResult{Adapter: a.name, Provider: a.provider, Conversations: []imports.NormalizedConversation{conv}}
	imports.SanitizeResult(res)
	return res, nil
}
