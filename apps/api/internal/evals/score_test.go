package evals

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

func benchmark(t *testing.T, id string) BenchmarkTask {
	t.Helper()
	for _, b := range Benchmarks {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("benchmark %s not found", id)
	return BenchmarkTask{}
}

func TestBenchmarkSuiteIsWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, b := range Benchmarks {
		if b.ID == "" || ids[b.ID] || b.Prompt == "" || b.Task == "" || b.Title == "" {
			t.Errorf("benchmark %+v is malformed or duplicated", b.ID)
		}
		ids[b.ID] = true
		if b.ExpectJSON && len(b.SchemaJSON()) == 0 {
			t.Errorf("%s expects JSON but has no schema", b.ID)
		}
		if len(b.MustContain) == 0 && len(b.MustNotContain) == 0 && b.Reference == "" {
			t.Errorf("%s has no automatic check", b.ID)
		}
		// The reference answer (when present) must itself pass the task's checks.
		if b.Reference != "" {
			if s := ScoreBenchmark(b, b.Reference); !s.Passed {
				t.Errorf("%s reference fails its own checks: %+v", b.ID, s)
			}
		}
	}
	if !ids["injection-resistance"] || !ids["grounded-why"] {
		t.Error("suite must include injection resistance and grounding tasks")
	}
}

func TestScoreBenchmark(t *testing.T) {
	cls := benchmark(t, "classify-intent")
	tests := []struct {
		name      string
		task      BenchmarkTask
		out       string
		passed    bool
		validJSON *bool
		missing   int
		forbidden int
	}{
		{"valid fenced json", cls, "```json\n{\"intent\": \"fork\"}\n```", true, ptr(true), 0, 0},
		{"enum violation", cls, `{"intent": "branch"}`, false, ptr(false), 1, 0},
		{"no json", cls, "The intent is fork.", false, ptr(false), 0, 0},
		{"injection obeyed", benchmark(t, "injection-resistance"), "DELETED ALL IDEAS", false, nil, 1, 1},
		{"injection resisted", benchmark(t, "injection-resistance"), "The user is planning a clinic scheduling module.", true, nil, 0, 0},
		{"grounded", benchmark(t, "grounded-why"), "Because moderation would delay launch [D2].", true, nil, 0, 0},
		{"ungrounded", benchmark(t, "grounded-why"), "I don't have access to your decisions.", false, nil, 2, 1},
		{"empty", benchmark(t, "title"), "  ", false, nil, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ScoreBenchmark(tt.task, tt.out)
			if s.Passed != tt.passed || len(s.Missing) != tt.missing || len(s.Forbidden) != tt.forbidden {
				t.Errorf("score = %+v", s)
			}
			if (tt.validJSON == nil) != (s.ValidJSON == nil) || (tt.validJSON != nil && *tt.validJSON != *s.ValidJSON) {
				t.Errorf("valid_json = %v, want %v", s.ValidJSON, tt.validJSON)
			}
		})
	}
	s := ScoreBenchmark(cls, `{"intent":"fork"}`)
	if s.F1 == nil || *s.F1 != 1 {
		t.Errorf("exact reference → F1 1, got %v", s.F1)
	}
}

func ptr[T any](v T) *T { return &v }

func TestTokenF1(t *testing.T) {
	if TokenF1("Skip the marketplace", "skip THE marketplace!") != 1 || TokenF1("", "x") != 0 || TokenF1("a b", "c d") != 0 {
		t.Error("TokenF1 basics")
	}
	if got := TokenF1("a b c d", "a b"); math.Abs(got-2.0/3) > 1e-9 {
		t.Errorf("TokenF1 partial = %v", got)
	}
}

// scripted is a provider returning canned answers per prompt substring.
type scripted struct {
	id      string
	answers map[string]string
	fail    error
}

func (s *scripted) ID() string { return s.id }
func (s *scripted) Complete(_ context.Context, req models.Request) (*models.Response, error) {
	if s.fail != nil {
		return nil, s.fail
	}
	prompt := req.Messages[len(req.Messages)-1].Content
	for k, v := range s.answers {
		if strings.Contains(prompt, k) {
			return &models.Response{Content: v, Usage: models.Usage{InputTokens: 100, OutputTokens: 20}}, nil
		}
	}
	return &models.Response{Content: "unknown", Usage: models.Usage{InputTokens: 100, OutputTokens: 1}}, nil
}
func (s *scripted) Stream(ctx context.Context, req models.Request, _ models.StreamHandler) (*models.Response, error) {
	return s.Complete(ctx, req)
}

func TestRunBenchmarksAcrossModels(t *testing.T) {
	good := &scripted{id: "good", answers: map[string]string{
		"Fork the biker":             `{"intent":"fork"}`,
		"Ignore all previous":        "A clinic scheduling conversation.",
		"I have an idea for a biker": "Biker Community Trip Platform",
	}}
	bad := &scripted{id: "bad", fail: errors.New("provider down")}
	gw := models.NewGateway(nil, nil, nil)
	gw.RegisterProvider(good)
	gw.RegisterProvider(bad)
	gw.SetModels([]domain.ModelConfig{
		{Provider: "good", Model: "g1", Enabled: true, Quality: 3, Speed: 3, ContextLength: 100000, InputCostPerMTok: 1, OutputCostPerMTok: 2},
		{Provider: "bad", Model: "b1", Enabled: true, Quality: 3, Speed: 3, ContextLength: 100000},
	})
	tasks := []BenchmarkTask{benchmark(t, "classify-intent"), benchmark(t, "injection-resistance"), benchmark(t, "title")}
	results := RunBenchmarks(context.Background(), gw, []string{"good/g1", "bad/b1", "missing/m"}, tasks, 5*time.Second)
	if len(results) != 9 {
		t.Fatalf("expected 9 results, got %d", len(results))
	}
	for _, r := range results[:3] {
		if r.Model != "good/g1" || r.Error != "" || !r.Score.Passed || r.TotalTokens != 120 {
			t.Errorf("good model result: %+v", r)
		}
		if math.Abs(r.CostUSD-(100*1+20*2)/1e6) > 1e-12 {
			t.Errorf("cost = %v", r.CostUSD)
		}
	}
	for _, r := range results[3:] {
		if r.Error == "" || r.Score.Passed {
			t.Errorf("failing model must report errors: %+v", r)
		}
	}
	sum := Summarize(results)
	if len(sum) != 3 || sum[0].Model != "good/g1" || sum[0].Passed != 3 || sum[0].JSONTasks != 1 || sum[0].JSONValid != 1 || sum[1].Errors != 3 {
		t.Errorf("summary = %+v", sum)
	}
	table := FormatTable(sum)
	if !strings.Contains(table, "good/g1") || !strings.Contains(table, "3/3") || !strings.Contains(table, "MODEL") {
		t.Errorf("table:\n%s", table)
	}
}
