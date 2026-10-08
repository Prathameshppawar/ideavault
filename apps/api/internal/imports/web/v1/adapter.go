// Package webv1 turns a generic public HTML page into a readable text
// document (Kind "document"): page chrome, scripts and styles are dropped and
// <article>/<main> content is preferred when present.
package webv1

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/htmltext"
)

// Adapter identity.
const (
	Name     = "web/v1"
	Provider = "web"
)

// Adapter implements imports.Adapter for generic web pages.
type Adapter struct{}

// New returns the web page adapter.
func New() *Adapter { return &Adapter{} }

// Name implements imports.Adapter.
func (*Adapter) Name() string { return Name }

// Provider implements imports.Adapter.
func (*Adapter) Provider() string { return Provider }

// Detect implements imports.Adapter. Any HTML qualifies, with a confidence
// low enough that specific adapters (e.g. ChatGPT share pages) win.
func (*Adapter) Detect(in imports.Input) float64 {
	if imports.LooksLikeHTML(in.Data) {
		return 0.55
	}
	if (imports.HasExt(in.Filename, ".html", ".htm", ".xhtml") || imports.MediaTypeIs(in.ContentType, "text/html", "application/xhtml+xml")) &&
		bytes.Contains(bytes.ToLower(imports.Head(in.Data)), []byte("<")) {
		return 0.55
	}
	return 0
}

// Parse implements imports.Adapter.
func (*Adapter) Parse(ctx context.Context, in imports.Input) (*imports.ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc, err := html.Parse(bytes.NewReader(imports.NormalizeEncoding(in.Data)))
	if err != nil {
		return nil, fmt.Errorf("%w: html: %v", imports.ErrUnsupportedFormat, err)
	}
	text := htmltext.Render(contentRoot(doc), htmltext.Options{SkipChrome: true})
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: web page has no readable text", imports.ErrEmptyInput)
	}
	title := pageTitle(doc)
	if title == "" {
		title = imports.TitleFromText(text, imports.DefaultTitleRunes)
	}
	if title == "" {
		title = urlTitle(in.URL)
	}
	conv := imports.NormalizedConversation{
		ExternalID: in.URL,
		Title:      title,
		Kind:       imports.KindDocument,
		Provider:   Provider,
		Adapter:    Name,
		URL:        in.URL,
		Messages:   []imports.NormalizedMessage{{Role: imports.RoleUser, Content: text}},
	}
	res := &imports.ParseResult{Adapter: Name, Provider: Provider, Conversations: []imports.NormalizedConversation{conv}}
	imports.SanitizeResult(res)
	return res, nil
}

// pageTitle prefers og:title, then <title>, then the first <h1>.
func pageTitle(doc *html.Node) string {
	var ogTitle, titleTag, h1 string
	walk(doc, func(n *html.Node) bool {
		switch n.DataAtom {
		case atom.Meta:
			key := strings.ToLower(htmltext.Attr(n, "property") + htmltext.Attr(n, "name"))
			if ogTitle == "" && key == "og:title" {
				ogTitle = clean(htmltext.Attr(n, "content"))
			}
		case atom.Title:
			if titleTag == "" {
				titleTag = clean(htmltext.TextContent(n))
			}
		case atom.H1:
			if h1 == "" {
				h1 = clean(htmltext.TextContent(n))
			}
		case atom.Script, atom.Style, atom.Svg:
			return false
		}
		return true
	})
	for _, t := range []string{ogTitle, titleTag, h1} {
		if t != "" {
			return t
		}
	}
	return ""
}

// contentRoot picks the largest <article>, else <main>, else role=main,
// else <body>, else the document.
func contentRoot(doc *html.Node) *html.Node {
	var articles []*html.Node
	var mainEl, roleMain, body *html.Node
	walk(doc, func(n *html.Node) bool {
		switch {
		case n.DataAtom == atom.Article:
			articles = append(articles, n)
		case n.DataAtom == atom.Main && mainEl == nil:
			mainEl = n
		case roleMain == nil && strings.EqualFold(htmltext.Attr(n, "role"), "main"):
			roleMain = n
		case n.DataAtom == atom.Body && body == nil:
			body = n
		}
		return n.DataAtom != atom.Script && n.DataAtom != atom.Style
	})
	if len(articles) > 0 {
		best, bestLen := articles[0], -1
		for _, a := range articles {
			if l := len(htmltext.TextContent(a)); l > bestLen {
				best, bestLen = a, l
			}
		}
		if bestLen > 0 {
			return best
		}
	}
	for _, n := range []*html.Node{mainEl, roleMain, body} {
		if n != nil {
			return n
		}
	}
	return doc
}

// walk visits element nodes depth-first (iteratively, so hostile nesting
// cannot overflow the stack); visit returns false to skip a subtree.
func walk(root *html.Node, visit func(*html.Node) bool) {
	stack := []*html.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Type == html.ElementNode && !visit(n) {
			continue
		}
		for c := n.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, c)
		}
	}
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func urlTitle(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimSuffix(u.Host+u.Path, "/")
}
