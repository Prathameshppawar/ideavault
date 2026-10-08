package claudev1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

func TestParseFixture(t *testing.T) {
	data := fixtures.Read(t, "claude", "conversations.json")
	res, err := New().Parse(context.Background(), imports.Input{Filename: "conversations.json", Data: data})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Adapter != Name || len(res.Conversations) != 2 || len(res.Failures) != 0 {
		t.Fatalf("result = %+v", res)
	}
	c := res.Conversations[0]
	if c.Title != "Clinic scheduling assistant" || c.ExternalID != "3f2a1b0c-0d1e-4f20-8a31-b2c3d4e5f601" {
		t.Errorf("identity = %q / %q", c.Title, c.ExternalID)
	}
	if c.URL != "https://claude.ai/chat/3f2a1b0c-0d1e-4f20-8a31-b2c3d4e5f601" || c.CreatedAt == nil || c.UpdatedAt == nil {
		t.Errorf("url/timestamps = %q %v %v", c.URL, c.CreatedAt, c.UpdatedAt)
	}
	want := []struct{ role, content string }{
		{imports.RoleUser, "How should a small clinic handle double bookings?\n\n[attachment: current_schedule.csv]"},
		{imports.RoleAssistant, "Prevent them at booking time: lock a slot as soon as a receptionist opens it.\n\nThen add a daily conflict report so staff can call affected patients early."},
		{imports.RoleUser, "Could patients rebook themselves from an SMS link?"},
		{imports.RoleAssistant, "Yes. Send a signed link that opens the three nearest free slots for the same doctor.\n\nExpire the link after 48 hours."},
	}
	if len(c.Messages) != len(want) {
		t.Fatalf("got %d messages, want %d", len(c.Messages), len(want))
	}
	for i, w := range want {
		m := c.Messages[i]
		if m.Role != w.role || m.Content != w.content {
			t.Errorf("message %d = %s %q\nwant %s %q", i, m.Role, m.Content, w.role, w.content)
		}
		if m.CreatedAt == nil || m.ExternalID == "" {
			t.Errorf("message %d missing id/time", i)
		}
	}
	for _, m := range c.Messages {
		if strings.Contains(m.Content, "SHOULD NOT BE IMPORTED") || strings.Contains(m.Content, "doctor,slot") {
			t.Errorf("leaked hidden content: %q", m.Content)
		}
	}
	untitled := res.Conversations[1]
	if untitled.Title != "Name ideas for a biker trip planning app?" {
		t.Errorf("derived title = %q", untitled.Title)
	}
}

func TestParsePartialFailures(t *testing.T) {
	data := `[
	  {"uuid": "conv-good-0001", "name": "ok", "chat_messages": [{"sender": "human", "text": "hi"}, {"sender": "assistant", "text": "hello"}]},
	  {"uuid": "conv-bad-00001", "name": "bad", "chat_messages": "nope"},
	  {"uuid": "conv-empty-001", "name": "empty", "chat_messages": [{"sender": "human", "content": [{"type": "thinking", "thinking": "x"}]}]},
	  {"uuid": "conv-mixed-001", "name": "mixed", "chat_messages": [{"sender": 5}, {"sender": "robot", "text": "beep"}, {"sender": "human", "text": "real"}]}
	]`
	res, err := New().Parse(context.Background(), imports.Input{Data: []byte(data)})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Conversations) != 2 || len(res.Failures) != 2 {
		t.Fatalf("conversations=%d failures=%+v", len(res.Conversations), res.Failures)
	}
	mixed := res.Conversations[1]
	if len(mixed.Messages) != 1 || len(mixed.Warnings) != 2 {
		t.Errorf("mixed = %+v", mixed)
	}
}

func TestParseErrorsAndDetect(t *testing.T) {
	if _, err := New().Parse(context.Background(), imports.Input{Data: []byte("nope")}); !errors.Is(err, imports.ErrUnsupportedFormat) {
		t.Errorf("err = %v", err)
	}
	if _, err := New().Parse(context.Background(), imports.Input{Data: []byte("[]")}); !errors.Is(err, imports.ErrEmptyInput) {
		t.Errorf("err = %v", err)
	}
	tests := []struct {
		data     string
		min, max float64
	}{
		{string(fixtures.Read(t, "claude", "conversations.json")), 0.9, 1},
		{string(fixtures.Read(t, "chatgpt", "conversations.json")), 0, 0},
		{`{"chat_messages": []}`, 0.7, 0.9},
		{`hello`, 0, 0},
	}
	for i, tt := range tests {
		if got := New().Detect(imports.Input{Data: []byte(tt.data)}); got < tt.min || got > tt.max {
			t.Errorf("case %d: Detect = %v, want [%v, %v]", i, got, tt.min, tt.max)
		}
	}
}
