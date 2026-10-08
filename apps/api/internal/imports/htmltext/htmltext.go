// Package htmltext converts HTML into readable, Markdown-flavoured plain text
// (paragraphs, headings, list items, code blocks, links, tables). It is used by
// adapters whose sources are HTML (Gemini Takeout responses, public web pages).
package htmltext

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Options tune the conversion.
type Options struct {
	// SkipChrome drops page chrome: nav, header, footer, aside, form, dialog, menu.
	SkipChrome bool
}

// Output and recursion guards for hostile input.
const (
	maxOutputBytes = 16 << 20
	maxDepth       = 512
)

// FromHTML parses src as an HTML body fragment and returns readable text.
func FromHTML(src string, opts Options) string {
	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(src), ctx)
	if err != nil {
		return ""
	}
	r := newRenderer(opts)
	for _, n := range nodes {
		r.walk(n, 0)
	}
	return r.result()
}

// Render converts the subtree rooted at n (including n) into readable text.
func Render(n *html.Node, opts Options) string {
	if n == nil {
		return ""
	}
	r := newRenderer(opts)
	r.walk(n, 0)
	return r.result()
}

// TextContent returns the concatenated text of n's subtree with whitespace
// collapsed, skipping scripts and styles.
func TextContent(n *html.Node) string {
	var sb strings.Builder
	collectText(n, &sb, 0)
	return strings.Join(strings.Fields(sb.String()), " ")
}

func collectText(n *html.Node, sb *strings.Builder, depth int) {
	if n == nil || depth > maxDepth || sb.Len() > maxOutputBytes {
		return
	}
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
		return
	}
	if n.Type == html.ElementNode && (n.DataAtom == atom.Script || n.DataAtom == atom.Style || n.DataAtom == atom.Template) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, sb, depth+1)
	}
}

type renderer struct {
	opts       Options
	sb         strings.Builder
	nl         int  // number of newlines currently ending sb
	space      bool // collapsed whitespace pending before the next word
	inListItem bool
}

func newRenderer(opts Options) *renderer { return &renderer{opts: opts} }

// sub returns a fresh renderer sharing options, for rendering nested blocks.
func (r *renderer) sub() *renderer { return &renderer{opts: r.opts, inListItem: r.inListItem} }

func (r *renderer) full() bool { return r.sb.Len() > maxOutputBytes }

// text appends inline text, collapsing whitespace runs to single spaces.
func (r *renderer) text(s string) {
	for len(s) > 0 && !r.full() {
		c, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		if unicode.IsSpace(c) {
			if r.sb.Len() > 0 && r.nl == 0 {
				r.space = true
			}
			continue
		}
		if r.space {
			r.sb.WriteByte(' ')
			r.space = false
		}
		r.sb.WriteRune(c)
		r.nl = 0
	}
}

// inline appends pre-rendered inline text verbatim (honouring a pending space).
func (r *renderer) inline(s string) {
	if s == "" || r.full() {
		return
	}
	if r.space && r.nl == 0 {
		r.sb.WriteByte(' ')
	}
	r.space = false
	r.sb.WriteString(s)
	r.nl = 0
	if strings.HasSuffix(s, "\n") {
		r.nl = len(s) - len(strings.TrimRight(s, "\n"))
	}
}

// breakLine ensures the output ends with at least n newlines (no-op at start).
func (r *renderer) breakLine(n int) {
	r.space = false
	if r.sb.Len() == 0 {
		return
	}
	for r.nl < n {
		r.sb.WriteByte('\n')
		r.nl++
	}
}

func (r *renderer) children(n *html.Node, depth int) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.walk(c, depth+1)
	}
}

func (r *renderer) walk(n *html.Node, depth int) {
	if depth > maxDepth || r.full() {
		return
	}
	switch n.Type {
	case html.TextNode:
		r.text(n.Data)
	case html.DocumentNode:
		r.children(n, depth)
	case html.ElementNode:
		r.element(n, depth)
	}
}

func (r *renderer) skipped(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Svg, atom.Math,
		atom.Iframe, atom.Object, atom.Embed, atom.Canvas, atom.Head, atom.Button,
		atom.Input, atom.Select, atom.Textarea, atom.Option, atom.Link, atom.Meta, atom.Title:
		return true
	case atom.Nav, atom.Header, atom.Footer, atom.Aside, atom.Form, atom.Dialog, atom.Menu:
		return r.opts.SkipChrome
	}
	if n.Data == "svg" { // foreign-content svg has no atom in some trees
		return true
	}
	if hasAttr(n, "hidden") || attr(n, "aria-hidden") == "true" {
		return true
	}
	style := strings.ReplaceAll(strings.ToLower(attr(n, "style")), " ", "")
	return strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden")
}

func (r *renderer) element(n *html.Node, depth int) {
	if r.skipped(n) {
		return
	}
	switch n.DataAtom {
	case atom.Br:
		r.space = false
		if r.sb.Len() > 0 && r.nl < 2 {
			r.sb.WriteByte('\n')
			r.nl++
		}
	case atom.Hr:
		r.breakLine(2)
		r.inline("---")
		r.breakLine(2)
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		title := r.renderInline(n, depth)
		if title != "" {
			r.breakLine(2)
			r.inline(strings.Repeat("#", level) + " " + title)
		}
		r.breakLine(2)
	case atom.Blockquote:
		r.blockquote(n, depth)
	case atom.P, atom.Figure, atom.Details, atom.Address, atom.Dl, atom.Fieldset:
		r.breakLine(2)
		r.children(n, depth)
		r.breakLine(2)
	case atom.Ul, atom.Ol:
		r.list(n, depth)
	case atom.Li:
		r.listItem(n, "- ", depth)
	case atom.Pre:
		r.pre(n)
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		code := strings.Join(strings.Fields(rawText(n)), " ")
		if code != "" {
			r.spaceFrom(n, true)
			r.inline("`" + code + "`")
			r.spaceFrom(n, false)
		}
	case atom.Strong, atom.B:
		r.wrapInline(n, "**", depth)
	case atom.Em, atom.I:
		r.wrapInline(n, "*", depth)
	case atom.A:
		r.link(n, depth)
	case atom.Img:
		if alt := strings.Join(strings.Fields(attr(n, "alt")), " "); alt != "" {
			r.inline("[image: " + alt + "]")
		}
	case atom.Table:
		r.table(n, depth)
	case atom.Div, atom.Section, atom.Article, atom.Main, atom.Header, atom.Footer, atom.Nav,
		atom.Aside, atom.Figcaption, atom.Dt, atom.Dd, atom.Summary, atom.Center, atom.Form,
		atom.Tr, atom.Caption, atom.Body, atom.Html:
		r.breakLine(1)
		r.children(n, depth)
		r.breakLine(1)
	default:
		r.children(n, depth)
	}
}

// spaceFrom carries a leading (or trailing) whitespace of n's raw text over
// to the surrounding inline flow, e.g. "a<b> b</b>" keeps its space.
func (r *renderer) spaceFrom(n *html.Node, leading bool) {
	t := rawText(n)
	if t == "" {
		return
	}
	var c rune
	if leading {
		c, _ = utf8.DecodeRuneInString(t)
	} else {
		c, _ = utf8.DecodeLastRuneInString(t)
	}
	if unicode.IsSpace(c) && r.sb.Len() > 0 && r.nl == 0 {
		r.space = true
	}
}

// renderInline renders n's children as a single line.
func (r *renderer) renderInline(n *html.Node, depth int) string {
	s := r.sub()
	s.children(n, depth)
	return strings.Join(strings.Fields(s.sb.String()), " ")
}

func (r *renderer) wrapInline(n *html.Node, marker string, depth int) {
	inner := r.renderInline(n, depth)
	if inner == "" {
		return
	}
	r.spaceFrom(n, true)
	r.inline(marker + inner + marker)
	r.spaceFrom(n, false)
}

func (r *renderer) link(n *html.Node, depth int) {
	inner := r.renderInline(n, depth)
	href := strings.TrimSpace(attr(n, "href"))
	lower := strings.ToLower(href)
	r.spaceFrom(n, true)
	switch {
	case inner == "":
	case (strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) && inner != href:
		r.inline("[" + inner + "](" + href + ")")
	default:
		r.inline(inner)
	}
	r.spaceFrom(n, false)
}

func (r *renderer) blockquote(n *html.Node, depth int) {
	s := r.sub()
	s.children(n, depth)
	body := s.result()
	if body == "" {
		return
	}
	r.breakLine(2)
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	r.inline(strings.Join(lines, "\n"))
	r.breakLine(2)
}

func (r *renderer) list(n *html.Node, depth int) {
	if r.inListItem {
		r.breakLine(1)
	} else {
		r.breakLine(2)
	}
	ordered := n.DataAtom == atom.Ol
	idx := 1
	if v, err := strconv.Atoi(attr(n, "start")); err == nil && v >= 0 && v < 1_000_000 {
		idx = v
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.ElementNode && c.DataAtom == atom.Li:
			marker := "- "
			if ordered {
				marker = fmt.Sprintf("%d. ", idx)
				idx++
			}
			r.listItem(c, marker, depth+1)
		default:
			r.walk(c, depth+1)
		}
	}
	if r.inListItem {
		r.breakLine(1)
	} else {
		r.breakLine(2)
	}
}

func (r *renderer) listItem(n *html.Node, marker string, depth int) {
	if r.skipped(n) {
		return
	}
	s := r.sub()
	s.inListItem = true
	s.children(n, depth)
	body := s.result()
	if body == "" {
		return
	}
	r.breakLine(1)
	indent := strings.Repeat(" ", len(marker))
	lines := strings.Split(body, "\n")
	var sb strings.Builder
	sb.WriteString(marker)
	sb.WriteString(lines[0])
	for _, l := range lines[1:] {
		sb.WriteByte('\n')
		if l != "" {
			sb.WriteString(indent)
			sb.WriteString(l)
		}
	}
	r.inline(sb.String())
	r.breakLine(1)
}

func (r *renderer) pre(n *html.Node) {
	code := strings.Trim(rawText(n), "\n")
	if strings.TrimSpace(code) == "" {
		return
	}
	lang := ""
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Code {
			lang = codeLanguage(attr(c, "class"))
			break
		}
	}
	r.breakLine(2)
	r.inline("```" + lang + "\n" + code + "\n```")
	r.breakLine(2)
}

func codeLanguage(class string) string {
	for _, f := range strings.Fields(class) {
		for _, p := range []string{"language-", "lang-"} {
			if strings.HasPrefix(f, p) {
				lang := strings.TrimPrefix(f, p)
				if lang != "" && len(lang) <= 32 && !strings.ContainsAny(lang, "`\n") {
					return lang
				}
			}
		}
	}
	return ""
}

func (r *renderer) table(n *html.Node, depth int) {
	var rows [][]string
	header := false
	var visit func(*html.Node, int)
	visit = func(t *html.Node, d int) {
		if d > maxDepth {
			return
		}
		for c := t.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Thead, atom.Tbody, atom.Tfoot:
				visit(c, d+1)
			case atom.Tr:
				var cells []string
				for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type == html.ElementNode && (cell.DataAtom == atom.Td || cell.DataAtom == atom.Th) {
						if len(rows) == 0 && cell.DataAtom == atom.Th {
							header = true
						}
						cells = append(cells, strings.ReplaceAll(r.renderInline(cell, d+1), "|", `\|`))
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
			}
		}
	}
	visit(n, depth)
	if len(rows) == 0 {
		return
	}
	var sb strings.Builder
	for i, row := range rows {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString("| " + strings.Join(row, " | ") + " |")
		if i == 0 && header {
			sep := make([]string, len(row))
			for j := range sep {
				sep[j] = "---"
			}
			sb.WriteString("\n| " + strings.Join(sep, " | ") + " |")
		}
	}
	r.breakLine(2)
	r.inline(sb.String())
	r.breakLine(2)
}

// result returns the cleaned output: trailing spaces trimmed per line, at
// most one blank line in a row, no leading/trailing blank lines.
func (r *renderer) result() string {
	lines := strings.Split(r.sb.String(), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRightFunc(l, unicode.IsSpace)
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// rawText returns n's text content with whitespace preserved.
func rawText(n *html.Node) string {
	var sb strings.Builder
	var visit func(*html.Node, int)
	visit = func(c *html.Node, d int) {
		if d > maxDepth || sb.Len() > maxOutputBytes {
			return
		}
		switch {
		case c.Type == html.TextNode:
			sb.WriteString(c.Data)
		case c.Type == html.ElementNode && c.DataAtom == atom.Br:
			sb.WriteByte('\n')
		case c.Type == html.ElementNode && (c.DataAtom == atom.Script || c.DataAtom == atom.Style):
		default:
			for k := c.FirstChild; k != nil; k = k.NextSibling {
				visit(k, d+1)
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		visit(c, 0)
	}
	return sb.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}

// Attr returns the value of attribute key on n ("" when absent).
func Attr(n *html.Node, key string) string { return attr(n, key) }
