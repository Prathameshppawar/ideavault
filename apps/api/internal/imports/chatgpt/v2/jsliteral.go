package chatgptv2

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

const enqueueCall = "streamController.enqueue("

// extractEnqueuePayloads returns the decoded string arguments of every
// `streamController.enqueue("...")` call in page, in document order.
func extractEnqueuePayloads(page string) []string {
	var out []string
	for i := 0; i < len(page); {
		j := strings.Index(page[i:], enqueueCall)
		if j < 0 {
			break
		}
		k := i + j + len(enqueueCall)
		for k < len(page) && (page[k] == ' ' || page[k] == '\n' || page[k] == '\t' || page[k] == '\r') {
			k++
		}
		if k >= len(page) || (page[k] != '"' && page[k] != '\'') {
			i = k
			continue
		}
		lit, end, ok := scanJSString(page, k)
		if ok {
			if s, err := decodeJSString(lit); err == nil {
				out = append(out, s)
			}
		}
		i = end
	}
	return out
}

// scanJSString returns the string literal (quotes included) that starts at
// s[start] and the index just past it. ok is false when it is unterminated.
func scanJSString(s string, start int) (lit string, end int, ok bool) {
	q := s[start]
	for i := start + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case q:
			return s[start : i+1], i + 1, true
		case '\n':
			return "", i, false
		}
	}
	return "", len(s), false
}

// decodeJSString decodes a JavaScript string literal (quotes included). JSON
// string syntax is tried first; the fallback handles JS-only escapes
// (\x41, \u{1F600}, \', \v, \0, line continuations).
func decodeJSString(lit string) (string, error) {
	if len(lit) < 2 {
		return "", errors.New("short string literal")
	}
	if lit[0] == '"' {
		var s string
		if json.Unmarshal([]byte(lit), &s) == nil {
			return s, nil
		}
	}
	body := lit[1 : len(lit)-1]
	var sb strings.Builder
	sb.Grow(len(body))
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			sb.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", errors.New("dangling escape")
		}
		switch e := body[i]; e {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'v':
			sb.WriteByte('\v')
		case '0':
			sb.WriteByte(0)
		case '\r':
			if i+1 < len(body) && body[i+1] == '\n' {
				i++
			}
		case '\n':
			// line continuation
		case 'x':
			if i+2 >= len(body) {
				return "", errors.New("bad \\x escape")
			}
			v, err := strconv.ParseUint(body[i+1:i+3], 16, 8)
			if err != nil {
				return "", err
			}
			sb.WriteRune(rune(v))
			i += 2
		case 'u':
			r, n, err := decodeUnicodeEscape(body[i+1:])
			if err != nil {
				return "", err
			}
			i += n
			// Combine UTF-16 surrogate pairs written as two \u escapes.
			if utf16.IsSurrogate(r) && i+2 < len(body) && body[i+1] == '\\' && body[i+2] == 'u' {
				if r2, n2, err := decodeUnicodeEscape(body[i+3:]); err == nil {
					if combined := utf16.DecodeRune(r, r2); combined != unicode.ReplacementChar {
						r = combined
						i += 2 + n2
					}
				}
			}
			sb.WriteRune(r)
		default:
			sb.WriteByte(e) // \" \' \\ \/ and identity escapes
		}
	}
	return sb.String(), nil
}

// decodeUnicodeEscape decodes the part after "\u": either XXXX or {X...}.
// It returns the rune and the number of bytes consumed.
func decodeUnicodeEscape(s string) (rune, int, error) {
	if strings.HasPrefix(s, "{") {
		end := strings.IndexByte(s, '}')
		if end < 2 || end > 7 {
			return 0, 0, errors.New("bad \\u{} escape")
		}
		v, err := strconv.ParseUint(s[1:end], 16, 32)
		if err != nil || v > 0x10FFFF {
			return 0, 0, errors.New("bad \\u{} escape")
		}
		return rune(v), end + 1, nil
	}
	if len(s) < 4 {
		return 0, 0, errors.New("bad \\u escape")
	}
	v, err := strconv.ParseUint(s[:4], 16, 16)
	if err != nil {
		return 0, 0, err
	}
	return rune(v), 4, nil
}
