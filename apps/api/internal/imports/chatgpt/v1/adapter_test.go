package chatgptv1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

func parseFixture(t *testing.T) *imports.ParseResult {
	t.Helper()
	data := fixtures.Read(t, "chatgpt", "conversations.json")
	res, err := New().Parse(context.Background(), imports.Input{Filename: "conversations.json", Data: data})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return res
}

func TestParseExportFixture(t *testing.T) {
	res := parseFixture(t)
	if res.Adapter != Name || res.Provider != Provider {
		t.Errorf("labels = %s/%s", res.Adapter, res.Provider)
	}
	if len(res.Failures) != 0 {
		t.Errorf("failures = %+v", res.Failures)
	}
	if len(res.Conversations) != 2 {
		t.Fatalf("got %d conversations, want 2", len(res.Conversations))
	}

	biker := res.Conversations[0]
	if biker.Title != "Biker community trip platform" || biker.ExternalID != "6a1f0c2e-1b2c-4d3e-9f10-aa11bb22cc01" {
		t.Errorf("biker identity = %q / %q", biker.Title, biker.ExternalID)
	}
	if biker.URL != "https://chatgpt.com/c/6a1f0c2e-1b2c-4d3e-9f10-aa11bb22cc01" || biker.Kind != imports.KindConversation {
		t.Errorf("biker url/kind = %q / %q", biker.URL, biker.Kind)
	}
	if biker.CreatedAt == nil || biker.CreatedAt.Year() != 2026 || biker.UpdatedAt == nil {
		t.Errorf("biker timestamps = %v / %v", biker.CreatedAt, biker.UpdatedAt)
	}
	wantRoles := []string{imports.RoleUser, imports.RoleAssistant, imports.RoleUser, imports.RoleAssistant}
	if len(biker.Messages) != len(wantRoles) {
		for _, m := range biker.Messages {
			t.Logf("%s: %q", m.Role, m.Content)
		}
		t.Fatalf("got %d messages, want %d", len(biker.Messages), len(wantRoles))
	}
	for i, m := range biker.Messages {
		if m.Role != wantRoles[i] {
			t.Errorf("message %d role = %s, want %s", i, m.Role, wantRoles[i])
		}
		for _, banned := range []string{"PRIVATE REASONING", "TOOL OUTPUT", "OLD BRANCH", "search_query", "Let me check", "Be concise", "Thought for", "print(sum"} {
			if strings.Contains(m.Content, banned) {
				t.Errorf("message %d leaks %q: %q", i, banned, m.Content)
			}
		}
		if strings.ContainsAny(m.Content, string([]rune{0xE200, 0xE201, 0xE202})) {
			t.Errorf("message %d keeps private-use markers", i)
		}
	}
	answer := biker.Messages[1].Content
	for _, want := range []string{
		"waypoints and a meeting time. ([Ride Planner Weekly](https://example.com/ride-planning-features))",
		"who is coming. ([Group Ride Safety Guide](https://example.org/group-ride-safety))",
		"for last-minute changes.\n",
		"Apps like RoadLoop already cover",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("answer missing %q:\n%s", want, answer)
		}
	}
	if strings.Contains(answer, "Related reading") || strings.Contains(answer, "turn0") {
		t.Errorf("unresolved markers left in answer:\n%s", answer)
	}
	if biker.Messages[1].Model != "gpt-5-thinking" {
		t.Errorf("model = %q", biker.Messages[1].Model)
	}
	if got := biker.Messages[2].Content; got != "[image]\n\nHere is a sketch of the trip card. How would you monetize this without annoying riders?" {
		t.Errorf("multimodal message = %q", got)
	}
	merged := biker.Messages[3].Content
	if !strings.HasPrefix(merged, "Keep trip creation free") || !strings.HasSuffix(merged, "\n\nIf you want, I can sketch a pricing page next.") {
		t.Errorf("assistant turn not merged: %q", merged)
	}
	if biker.Messages[3].ExternalID != "a-2b" || biker.Messages[3].CreatedAt == nil {
		t.Errorf("merged message keeps first id/time: %+v", biker.Messages[3])
	}

	clinic := res.Conversations[1]
	if len(clinic.Messages) != 4 {
		t.Fatalf("clinic: got %d messages, want 4", len(clinic.Messages))
	}
	if strings.Contains(clinic.Messages[0].Content, "Custom instructions") {
		t.Errorf("custom-instruction system message imported")
	}
	if clinic.Messages[2].Content != "Also, how should reminders work for missed appointments?" {
		t.Errorf("audio transcription = %q", clinic.Messages[2].Content)
	}
	if clinic.Messages[3].Model != "gpt-4o" {
		t.Errorf("default model slug not applied: %q", clinic.Messages[3].Model)
	}
	if clinic.Messages[0].Model != "" {
		t.Errorf("user message has a model: %q", clinic.Messages[0].Model)
	}
}

func TestParsePartialFailures(t *testing.T) {
	data := `[
	  {"id": "good-conversation-1", "title": "Good", "current_node": "b",
	   "mapping": {"a": {"id": "a", "parent": null, "children": ["b"], "message": {"id": "a", "author": {"role": "user"}, "content": {"content_type": "text", "parts": ["Hello riders"]}, "recipient": "all"}},
	               "b": {"id": "b", "parent": "a", "children": [], "message": {"id": "b", "author": {"role": "assistant"}, "content": {"content_type": "text", "parts": ["Hi!"]}, "recipient": "all"}}}},
	  {"id": "bad-mapping-0001", "title": "Broken", "mapping": "oops"},
	  {"id": "empty-convo-0001", "title": "Empty", "mapping": {"r": {"id": "r", "message": null, "parent": null, "children": []}}},
	  42
	]`
	res, err := New().Parse(context.Background(), imports.Input{Data: []byte(data)})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Conversations) != 1 || res.Conversations[0].Title != "Good" {
		t.Fatalf("conversations = %+v", res.Conversations)
	}
	if len(res.Failures) != 3 {
		t.Fatalf("failures = %+v", res.Failures)
	}
	if f := res.Failures[0]; f.Index != 1 || f.ExternalID != "bad-mapping-0001" || f.Title != "Broken" {
		t.Errorf("failure 0 = %+v", f)
	}
	if f := res.Failures[1]; f.Index != 2 || !strings.Contains(f.Error, "no visible messages") {
		t.Errorf("failure 1 = %+v", f)
	}
}

func TestParseMalformedNodeKeepsThread(t *testing.T) {
	// Node "b" has a malformed message (author is a string) but its links
	// survive, so "a" is still reached from current node "c".
	data := `{"id": "single-object-01", "title": "Single", "current_node": "c", "mapping": {
	  "a": {"id": "a", "parent": null, "children": ["b"], "message": {"author": {"role": "user"}, "content": {"content_type": "text", "parts": ["first"]}}},
	  "b": {"id": "b", "parent": "a", "children": ["c"], "message": {"author": "assistant"}},
	  "c": {"id": "c", "parent": "b", "children": [], "message": {"author": {"role": "assistant"}, "content": {"content_type": "text", "parts": ["third"]}}}}}`
	res, err := New().Parse(context.Background(), imports.Input{Data: []byte(data)})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := res.Conversations[0]
	if len(c.Messages) != 2 || c.Messages[0].Content != "first" || c.Messages[1].Content != "third" {
		t.Fatalf("messages = %+v", c.Messages)
	}
	if len(c.Warnings) == 0 || !strings.Contains(c.Warnings[0], "malformed") {
		t.Errorf("warnings = %v", c.Warnings)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
		want error
	}{
		{"not json", "hello", imports.ErrUnsupportedFormat},
		{"truncated", `[{"mapping": {`, imports.ErrUnsupportedFormat},
		{"empty array", `[]`, imports.ErrEmptyInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Parse(context.Background(), imports.Input{Data: []byte(tt.data)})
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		data string
		min  float64
		max  float64
	}{
		{"export", string(fixtures.Read(t, "chatgpt", "conversations.json")), 0.9, 1},
		{"claude export", `[{"uuid":"x","chat_messages":[]}]`, 0, 0},
		{"mapping only", `{"mapping": {}}`, 0.6, 0.8},
		{"word mapping inside string", `[{"text":"\"mapping\": no"}]`, 0, 0},
		{"markdown", "# Notes", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New().Detect(imports.Input{Data: []byte(tt.data)})
			if got < tt.min || got > tt.max {
				t.Errorf("Detect = %v, want [%v, %v]", got, tt.min, tt.max)
			}
		})
	}
}

func TestParseHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().Parse(ctx, imports.Input{Data: fixtures.Read(t, "chatgpt", "conversations.json")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
