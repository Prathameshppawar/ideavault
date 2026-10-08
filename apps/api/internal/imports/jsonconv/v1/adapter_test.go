package jsonconvv1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

func TestParseFixture(t *testing.T) {
	res, err := New().Parse(context.Background(), imports.Input{Filename: "messages.json", Data: fixtures.Read(t, "json", "messages.json")})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Conversations) != 1 {
		t.Fatalf("got %d conversations", len(res.Conversations))
	}
	c := res.Conversations[0]
	if c.Title != "Clinic onboarding checklist" || c.ExternalID != "json-conv-001" || c.CreatedAt == nil {
		t.Errorf("identity = %q %q %v", c.Title, c.ExternalID, c.CreatedAt)
	}
	roles := []string{imports.RoleSystem, imports.RoleUser, imports.RoleAssistant, imports.RoleUser, imports.RoleAssistant}
	if len(c.Messages) != len(roles) {
		t.Fatalf("got %d messages, want %d", len(c.Messages), len(roles))
	}
	for i, r := range roles {
		if c.Messages[i].Role != r {
			t.Errorf("message %d role = %s, want %s", i, c.Messages[i].Role, r)
		}
		if strings.Contains(c.Messages[i].Content, "TOOL OUTPUT") {
			t.Errorf("tool message imported")
		}
	}
	if c.Messages[2].Content != "1. Import the patient list\n2. Configure doctors and rooms\n\n3. Set reminder templates" {
		t.Errorf("content parts = %q", c.Messages[2].Content)
	}
	if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], `"tool"`) {
		t.Errorf("warnings = %v", c.Warnings)
	}
}

func TestParseShapes(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		convs     int
		failures  int
		firstText string
	}{
		{"bare openai array", `[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"x"}}]}]`, 1, 0, "hi"},
		{"conversations wrapper", `{"conversations":[{"title":"a","messages":[{"role":"user","content":"one"}]},[{"role":"human","content":"two"}]]}`, 2, 0, "one"},
		{"array of conversations", `[{"title":"a","messages":[{"role":"user","content":"x"}]},{"title":"b","messages":"bad"}]`, 1, 1, "x"},
		{"sender and text keys", `{"messages":[{"sender":"me","text":"note"},{"author":{"role":"bot"},"message":"reply"}]}`, 1, 0, "note"},
		{"gemini api parts", `{"messages":[{"role":"user","parts":[{"text":"q"}]},{"role":"model","parts":[{"text":"a"}]}]}`, 1, 0, "q"},
		{"unix timestamps", `{"messages":[{"role":"user","content":"t","timestamp":1767225600}]}`, 1, 0, "t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := New().Parse(context.Background(), imports.Input{Data: []byte(tt.data)})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(res.Conversations) != tt.convs || len(res.Failures) != tt.failures {
				t.Fatalf("convs=%d failures=%+v", len(res.Conversations), res.Failures)
			}
			if got := res.Conversations[0].Messages[0].Content; got != tt.firstText {
				t.Errorf("first message = %q", got)
			}
		})
	}
}

func TestParseImagePlaceholder(t *testing.T) {
	res, err := New().Parse(context.Background(), imports.Input{Data: []byte(`[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"x"}}]}]`)})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Conversations[0].Messages[0].Content; got != "look\n\n[image]" {
		t.Errorf("got %q", got)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		data string
		want error
	}{
		{`not json`, imports.ErrUnsupportedFormat},
		{`{"foo": 1}`, imports.ErrUnsupportedFormat},
		{`"string"`, imports.ErrUnsupportedFormat},
		{`[]`, imports.ErrEmptyInput},
		{`   `, imports.ErrEmptyInput},
	}
	for _, tt := range tests {
		if _, err := New().Parse(context.Background(), imports.Input{Data: []byte(tt.data)}); !errors.Is(err, tt.want) {
			t.Errorf("Parse(%q) err = %v, want %v", tt.data, err, tt.want)
		}
	}
}

func TestDetectDefersToSpecificAdapters(t *testing.T) {
	tests := []struct {
		name     string
		in       imports.Input
		min, max float64
	}{
		{"generic messages", imports.Input{Data: fixtures.Read(t, "json", "messages.json")}, 0.8, 0.8},
		{"chatgpt export", imports.Input{Data: fixtures.Read(t, "chatgpt", "conversations.json")}, 0, 0.2},
		{"claude export", imports.Input{Data: fixtures.Read(t, "claude", "conversations.json")}, 0, 0.2},
		{"gemini takeout", imports.Input{Data: fixtures.Read(t, "gemini", "MyActivity.json")}, 0, 0.2},
		{"other takeout", imports.Input{Data: []byte(`[{"header":"YouTube","title":"Watched","products":["YouTube"]}]`)}, 0, 0.2},
		{"messages key only", imports.Input{Data: []byte(`{"messages": []}`)}, 0.6, 0.6},
		{"json file unknown shape", imports.Input{Filename: "a.json", Data: []byte(`{"a":1}`)}, 0.3, 0.3},
		{"not json", imports.Input{Data: []byte("hello")}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New().Detect(tt.in); got < tt.min || got > tt.max {
				t.Errorf("Detect = %v, want [%v, %v]", got, tt.min, tt.max)
			}
		})
	}
}
