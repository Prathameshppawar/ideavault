package geminiv1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

func TestParseFixture(t *testing.T) {
	data := fixtures.Read(t, "gemini", "MyActivity.json")
	res, err := New().Parse(context.Background(), imports.Input{Filename: "MyActivity.json", Data: data})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Conversations) != 2 {
		t.Fatalf("got %d conversations, want 2 (grouped by %v gaps)", len(res.Conversations), SessionGap)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "1 non-prompt") {
		t.Errorf("warnings = %v", res.Warnings)
	}

	morning := res.Conversations[0]
	if morning.Title != "What features should a clinic waiting-room display have?" {
		t.Errorf("title = %q", morning.Title)
	}
	if morning.ExternalID != "gemini-activity-20260107T100500.000000Z" || morning.CreatedAt == nil || morning.UpdatedAt == nil {
		t.Errorf("identity = %q %v %v", morning.ExternalID, morning.CreatedAt, morning.UpdatedAt)
	}
	if len(morning.Messages) != 4 {
		t.Fatalf("morning: %d messages", len(morning.Messages))
	}
	wantResponse := "A good waiting-room display shows:\n\n- **Queue position** for each patient\n- Estimated wait time\n- Health tips between updates\n\n### Privacy\n\nUse ticket numbers instead of names."
	if morning.Messages[1].Role != imports.RoleAssistant || morning.Messages[1].Content != wantResponse {
		t.Errorf("response =\n%q\nwant\n%q", morning.Messages[1].Content, wantResponse)
	}
	// Sorted by time even though the export lists it later; entities decoded.
	if morning.Messages[2].Content != "Should the reminder SMS include the doctor's name?" {
		t.Errorf("second prompt = %q", morning.Messages[2].Content)
	}
	if !strings.Contains(morning.Messages[3].Content, "short & avoid") {
		t.Errorf("entity in response = %q", morning.Messages[3].Content)
	}

	evening := res.Conversations[1]
	if len(evening.Messages) != 3 {
		t.Fatalf("evening: %d messages", len(evening.Messages))
	}
	if !strings.Contains(evening.Messages[1].Content, "```python\nshare = total_fuel * rider_km / group_km\n```") {
		t.Errorf("code block = %q", evening.Messages[1].Content)
	}
	last := evening.Messages[2]
	if last.Role != imports.RoleUser || last.Content != "Draft a tagline for the trip planner" {
		t.Errorf("user-only turn = %+v", last)
	}
}

func TestParseFiltersAndFailures(t *testing.T) {
	data := `[
	  {"header": "Search", "title": "Searched for clinic software", "time": "2026-01-07T10:00:00Z"},
	  {"header": "Gemini Apps", "title": "Prompted no time here"},
	  {"header": "Bard", "title": "Prompted legacy bard prompt", "time": "2024-01-01T10:00:00Z", "safeHtmlItem": [{"html": "<p>old answer</p>"}]},
	  "garbage"
	]`
	res, err := New().Parse(context.Background(), imports.Input{Data: []byte(data)})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Conversations) != 1 || res.Conversations[0].Messages[1].Content != "old answer" {
		t.Fatalf("conversations = %+v", res.Conversations)
	}
	if len(res.Failures) != 2 {
		t.Errorf("failures = %+v", res.Failures)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "other products") {
		t.Errorf("warnings = %v", res.Warnings)
	}

	if _, err := New().Parse(context.Background(), imports.Input{Data: []byte(`[{"header":"Search","title":"x"}]`)}); !errors.Is(err, imports.ErrEmptyInput) {
		t.Errorf("err = %v, want ErrEmptyInput", err)
	}
	if _, err := New().Parse(context.Background(), imports.Input{Data: []byte(`{"header":"Gemini Apps"}`)}); !errors.Is(err, imports.ErrUnsupportedFormat) {
		t.Errorf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		data     string
		min, max float64
	}{
		{string(fixtures.Read(t, "gemini", "MyActivity.json")), 0.9, 1},
		{`[{"header": "YouTube", "title": "Watched a video", "titleUrl": "https://example.com"}]`, 0, 0},
		{`[{"header":"x","safeHtmlItem":[{"html":"<p>a</p>"}],"title":"Prompted hi"}]`, 0.8, 0.9},
		{string(fixtures.Read(t, "chatgpt", "conversations.json")), 0, 0},
	}
	for i, tt := range tests {
		if got := New().Detect(imports.Input{Data: []byte(tt.data)}); got < tt.min || got > tt.max {
			t.Errorf("case %d: Detect = %v, want [%v, %v]", i, got, tt.min, tt.max)
		}
	}
}
