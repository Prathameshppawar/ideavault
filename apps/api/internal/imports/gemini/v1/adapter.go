// Package geminiv1 parses Google Takeout "My Activity" exports for Gemini
// Apps (formerly Bard): My Activity/Gemini Apps/MyActivity.json.
//
// Takeout records one activity per prompt ("Prompted <text>") with the
// response as HTML, but no conversation ids. Activities are therefore sorted
// by time and grouped into conversations whenever the gap between two
// prompts exceeds SessionGap.
package geminiv1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/htmltext"
)

// Adapter identity.
const (
	Name     = "gemini/v1"
	Provider = "gemini"
)

// SessionGap splits activities into separate conversations.
const SessionGap = 30 * time.Minute

const promptPrefix = "Prompted "

// Adapter implements imports.Adapter for Gemini Takeout activity.
type Adapter struct{}

// New returns the Gemini Takeout adapter.
func New() *Adapter { return &Adapter{} }

// Name implements imports.Adapter.
func (*Adapter) Name() string { return Name }

// Provider implements imports.Adapter.
func (*Adapter) Provider() string { return Provider }

var geminiHeader = regexp.MustCompile(`"header"\s*:\s*"(?:Gemini Apps|Gemini|Bard)"`)

// Detect implements imports.Adapter.
func (*Adapter) Detect(in imports.Input) float64 {
	h := imports.Head(in.Data)
	if len(h) == 0 || h[0] != '[' {
		return 0
	}
	switch {
	case geminiHeader.Match(h):
		return 0.95
	case imports.HasJSONKey(h, "safeHtmlItem") && bytes.Contains(h, []byte(`"`+promptPrefix)):
		return 0.85
	}
	return 0
}

// Activity is one Takeout "My Activity" record.
type Activity struct {
	Header       string            `json:"header"`
	Title        string            `json:"title"`
	Time         imports.Timestamp `json:"time"`
	Products     []string          `json:"products"`
	SafeHTMLItem []struct {
		HTML string `json:"html"`
	} `json:"safeHtmlItem"`
}

type turn struct {
	index    int
	at       time.Time
	prompt   string
	response string
}

// Parse implements imports.Adapter.
func (*Adapter) Parse(ctx context.Context, in imports.Input) (*imports.ParseResult, error) {
	data := bytes.TrimSpace(imports.NormalizeEncoding(in.Data))
	var raws []json.RawMessage
	if len(data) == 0 || data[0] != '[' || json.Unmarshal(data, &raws) != nil {
		return nil, fmt.Errorf("%w: gemini activity export must be a JSON array", imports.ErrUnsupportedFormat)
	}
	res := &imports.ParseResult{Adapter: Name, Provider: Provider}
	var turns []turn
	otherProducts, nonPrompt := 0, 0
	for i, raw := range raws {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		var a Activity
		if err := json.Unmarshal(raw, &a); err != nil {
			res.Failures = append(res.Failures, imports.ItemFailure{Index: i, Error: "malformed activity entry: " + err.Error()})
			continue
		}
		if !isGemini(a) {
			otherProducts++
			continue
		}
		if !strings.HasPrefix(a.Title, promptPrefix) {
			nonPrompt++
			continue
		}
		prompt := strings.TrimSpace(html.UnescapeString(strings.TrimPrefix(a.Title, promptPrefix)))
		if a.Time.IsZero() {
			res.Failures = append(res.Failures, imports.ItemFailure{Index: i, Title: imports.Truncate(prompt, imports.DefaultTitleRunes), Error: "activity entry has no valid time"})
			continue
		}
		var htmlParts []string
		for _, item := range a.SafeHTMLItem {
			htmlParts = append(htmlParts, item.HTML)
		}
		turns = append(turns, turn{
			index:    i,
			at:       a.Time.UTC(),
			prompt:   prompt,
			response: htmltext.FromHTML(strings.Join(htmlParts, "\n"), htmltext.Options{}),
		})
	}
	if otherProducts > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("skipped %d activity entries from other products", otherProducts))
	}
	if nonPrompt > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("skipped %d non-prompt Gemini activity entries", nonPrompt))
	}
	if len(turns) == 0 && len(res.Failures) == 0 {
		return nil, fmt.Errorf("%w: no Gemini prompts found in activity export", imports.ErrEmptyInput)
	}
	res.Conversations = group(turns)
	imports.SanitizeResult(res)
	return res, nil
}

func isGemini(a Activity) bool {
	names := append([]string{a.Header}, a.Products...)
	for _, n := range names {
		l := strings.ToLower(n)
		if strings.Contains(l, "gemini") || strings.Contains(l, "bard") {
			return true
		}
	}
	return false
}

// group sorts turns by time and splits them into conversations at gaps
// longer than SessionGap.
func group(turns []turn) []imports.NormalizedConversation {
	sort.SliceStable(turns, func(i, j int) bool { return turns[i].at.Before(turns[j].at) })
	var convs []imports.NormalizedConversation
	var cur *imports.NormalizedConversation
	var last time.Time
	for _, t := range turns {
		if cur == nil || t.at.Sub(last) > SessionGap {
			convs = append(convs, imports.NormalizedConversation{
				ExternalID: "gemini-activity-" + t.at.Format("20060102T150405.000000Z"),
				Title:      imports.TitleFromText(t.prompt, imports.DefaultTitleRunes),
				Kind:       imports.KindConversation,
				Provider:   Provider,
				Adapter:    Name,
			})
			cur = &convs[len(convs)-1]
			at := t.at
			cur.CreatedAt = &at
		}
		at := t.at
		cur.UpdatedAt = &at
		last = t.at
		cur.Messages = append(cur.Messages, imports.NormalizedMessage{Role: imports.RoleUser, Content: t.prompt, CreatedAt: &at})
		if t.response != "" {
			cur.Messages = append(cur.Messages, imports.NormalizedMessage{Role: imports.RoleAssistant, Content: t.response, CreatedAt: &at})
		}
	}
	return convs
}
