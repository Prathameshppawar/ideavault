package chatgptv1

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ChatGPT wraps inline references in private-use characters:
// U+E200 name U+E202 arg U+E202 arg ... U+E201, e.g. a "cite" marker listing
// search results or an "entity" marker whose argument is a JSON array.
const (
	markStart rune = 0xE200
	markEnd   rune = 0xE201
	markSep   rune = 0xE202
)

// legacyCite matches older file-search citations such as 【4:0†source】.
var legacyCite = regexp.MustCompile(`【[^【】\n]{1,80}†[^【】\n]{0,80}】`)

// entityName pulls the display name out of an entity marker whose JSON
// argument does not parse: entity["kind","Display Name",...].
var entityName = regexp.MustCompile(`^\s*\[\s*"(?:[^"\\]|\\.)*"\s*,\s*"((?:[^"\\]|\\.)*)"`)

// CleanMarkup resolves ChatGPT's private-use citation/entity markers in text.
// A marker whose full text equals a content reference's matched_text is
// replaced by that reference's alt text (usually Markdown such as
// "([Source](https://...))") or, lacking alt, by Markdown links built from
// the reference's items. Unmatched markers are removed (cite, navlist,
// filecite, ...) except entity markers, which become their display name. The
// result never contains U+E200–U+E202.
func CleanMarkup(text string, refs []ContentReference) string {
	hasMarkers := strings.ContainsRune(text, markStart) || strings.ContainsRune(text, markEnd) || strings.ContainsRune(text, markSep)
	hasLegacy := strings.Contains(text, "†")
	if !hasMarkers && !hasLegacy {
		return text
	}
	byMatch := make(map[string]ContentReference, len(refs))
	for _, r := range refs {
		if strings.ContainsRune(r.MatchedText, markStart) || strings.Contains(r.MatchedText, "†") {
			if _, dup := byMatch[r.MatchedText]; !dup {
				byMatch[r.MatchedText] = r
			}
		}
	}
	if hasMarkers {
		text = replaceMarkers(text, byMatch)
	}
	if hasLegacy {
		text = legacyCite.ReplaceAllStringFunc(text, func(m string) string {
			if r, ok := byMatch[m]; ok {
				return referenceText(r)
			}
			return ""
		})
	}
	return strings.Map(func(r rune) rune {
		if r == markStart || r == markEnd || r == markSep {
			return -1
		}
		return r
	}, text)
}

func replaceMarkers(text string, byMatch map[string]ContentReference) string {
	buf := make([]byte, 0, len(text))
	startLen := utf8.RuneLen(markStart)
	endLen := utf8.RuneLen(markEnd)
	for i := 0; i < len(text); {
		j := strings.IndexRune(text[i:], markStart)
		if j < 0 {
			buf = append(buf, text[i:]...)
			break
		}
		start := i + j
		buf = append(buf, text[i:start]...)
		var next int
		var repl string
		if k := strings.IndexRune(text[start+startLen:], markEnd); k >= 0 {
			next = start + startLen + k + endLen
			marker := text[start:next]
			if r, ok := byMatch[marker]; ok {
				repl = referenceText(r)
			}
			if repl == "" {
				repl = defaultReplacement(text[start+startLen : next-endLen])
			}
		} else {
			// Unterminated marker (truncated text): drop it up to the line end.
			next = len(text)
			if nl := strings.IndexByte(text[start:], '\n'); nl >= 0 {
				next = start + nl
			}
		}
		if repl == "" && endsMarkerGap(text, next) {
			for len(buf) > 0 && (buf[len(buf)-1] == ' ' || buf[len(buf)-1] == '\t') {
				buf = buf[:len(buf)-1]
			}
		}
		if strings.HasPrefix(repl, "(") && len(buf) > 0 && !isSpaceByte(buf[len(buf)-1]) {
			buf = append(buf, ' ')
		}
		buf = append(buf, repl...)
		i = next
	}
	return string(buf)
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' || b == '\n' }

// endsMarkerGap reports whether a removed marker ending at pos is followed by
// whitespace, punctuation or the end of text, so the space before it can go.
func endsMarkerGap(text string, pos int) bool {
	if pos >= len(text) {
		return true
	}
	return strings.ContainsRune(" \t.,;:!?)]}\n", rune(text[pos]))
}

// referenceText renders a matched content reference ("" if it has nothing).
func referenceText(r ContentReference) string {
	if strings.TrimSpace(r.Alt) != "" {
		return r.Alt
	}
	var links []string
	for _, it := range r.Items {
		u, err := url.Parse(strings.TrimSpace(it.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		title := strings.Join(strings.Fields(it.Title), " ")
		if title == "" {
			title = strings.TrimPrefix(u.Host, "www.")
		}
		title = strings.NewReplacer("[", "(", "]", ")").Replace(title)
		links = append(links, "["+title+"]("+u.String()+")")
	}
	if len(links) == 0 {
		return ""
	}
	return "(" + strings.Join(links, ", ") + ")"
}

// defaultReplacement renders an unmatched marker body (without delimiters).
func defaultReplacement(body string) string {
	name, args, _ := strings.Cut(body, string(markSep))
	if strings.TrimSpace(name) != "entity" {
		return "" // cite, navlist, filecite, image_group, ...: not readable without metadata
	}
	args = strings.ReplaceAll(args, string(markSep), "")
	var arr []any
	if json.Unmarshal([]byte(args), &arr) == nil {
		if len(arr) > 1 {
			if s, ok := arr[1].(string); ok {
				return s
			}
		}
		if len(arr) == 1 {
			if s, ok := arr[0].(string); ok {
				return s
			}
		}
		return ""
	}
	if m := entityName.FindStringSubmatch(args); m != nil {
		var s string
		if json.Unmarshal([]byte(`"`+m[1]+`"`), &s) == nil {
			return s
		}
		return m[1]
	}
	return ""
}
