package imports

import (
	"bytes"
	"mime"
	"path"
	"strings"
	"unicode/utf8"
)

// SniffLimit is how many leading bytes Detect implementations inspect, so
// detection stays cheap on very large inputs.
const SniffLimit = 4 << 20

// Head returns at most SniffLimit leading bytes of data with a UTF-8 BOM and
// leading whitespace removed.
func Head(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if len(data) > SniffLimit {
		data = data[:SniffLimit]
	}
	return bytes.TrimLeft(data, " \t\r\n")
}

// LooksLikeJSON reports whether data starts (after whitespace) like a JSON
// object or array.
func LooksLikeJSON(data []byte) bool {
	h := Head(data)
	return len(h) > 0 && (h[0] == '{' || h[0] == '[')
}

// LooksLikeHTML reports whether data looks like an HTML document.
func LooksLikeHTML(data []byte) bool {
	h := Head(data)
	if len(h) == 0 || h[0] != '<' {
		return false
	}
	if len(h) > 4096 {
		h = h[:4096]
	}
	lower := bytes.ToLower(h)
	for _, marker := range []string{"<!doctype html", "<html", "<head", "<body", "<meta", "<title"} {
		if bytes.Contains(lower, []byte(marker)) {
			return true
		}
	}
	return false
}

// LooksLikeText reports whether data is plausibly human-readable text: valid
// UTF-8 in its head, no NUL bytes and few control characters.
func LooksLikeText(data []byte) bool {
	h := Head(data)
	if len(h) == 0 {
		return false
	}
	if len(h) > 64<<10 {
		h = h[:64<<10]
		// Do not let a cut multi-byte rune fail validation.
		for i := 0; i < utf8.UTFMax && len(h) > 0 && !utf8.Valid(h); i++ {
			h = h[:len(h)-1]
		}
	}
	if !utf8.Valid(h) || bytes.IndexByte(h, 0) >= 0 {
		return false
	}
	controls := 0
	for _, b := range h {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' {
			controls++
		}
	}
	return controls*100 < len(h)
}

// HasJSONKey reports whether data contains `"key"` used as an object key
// (followed by optional whitespace and a colon) outside an escaped string.
// It is a cheap structural hint for Detect, not a parser.
func HasJSONKey(data []byte, key string) bool {
	needle := []byte(`"` + key + `"`)
	for off := 0; off < len(data); {
		i := bytes.Index(data[off:], needle)
		if i < 0 {
			return false
		}
		i += off
		off = i + len(needle)
		if i > 0 && data[i-1] == '\\' {
			continue // inside a string: \"key\"
		}
		j := off
		for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\n' || data[j] == '\r') {
			j++
		}
		if j < len(data) && data[j] == ':' {
			return true
		}
	}
	return false
}

// HasExt reports whether filename has one of exts (case-insensitive, with dot).
func HasExt(filename string, exts ...string) bool {
	ext := strings.ToLower(path.Ext(strings.ReplaceAll(filename, `\`, "/")))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// MediaTypeIs reports whether a Content-Type header has one of the given
// media types (parameters such as charset are ignored).
func MediaTypeIs(contentType string, types ...string) bool {
	if contentType == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	}
	for _, t := range types {
		if mt == t {
			return true
		}
	}
	return false
}
