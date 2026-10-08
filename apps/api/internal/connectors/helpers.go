package connectors

import (
	"encoding/base64"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/htmltext"
)

// htmlToText converts HTML responses into readable text; other types pass through.
func htmlToText(body, contentType string) string {
	ct := strings.ToLower(contentType)
	trimmed := strings.TrimSpace(body)
	if strings.Contains(ct, "html") || strings.HasPrefix(strings.ToLower(trimmed), "<!doctype html") || strings.HasPrefix(strings.ToLower(trimmed), "<html") {
		return htmltext.FromHTML(body, htmltext.Options{SkipChrome: true})
	}
	return body
}

func decodeB64(s string) string {
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(s, "\n", ""))
	if err != nil {
		return ""
	}
	return string(b)
}
