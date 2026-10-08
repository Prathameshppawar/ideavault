package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// BenchmarkScore is the automatic score of one model output for one benchmark task.
type BenchmarkScore struct {
	// ValidJSON is set for tasks that expect JSON (valid JSON that matches the schema).
	ValidJSON    *bool    `json:"valid_json,omitempty"`
	SchemaErrors []string `json:"schema_errors,omitempty"`
	// Missing lists MustContain substrings absent from the output.
	Missing []string `json:"missing,omitempty"`
	// Forbidden lists MustNotContain substrings present in the output.
	Forbidden []string `json:"forbidden,omitempty"`
	// F1 is token-level F1 against the reference answer (when the task has one).
	F1     *float64 `json:"f1,omitempty"`
	Passed bool     `json:"passed"`
}

// ScoreBenchmark scores output against the task's automatic checks: JSON validity
// (and schema), required and forbidden substrings (case-insensitive) and F1.
// A task passes when the JSON is valid (if expected) and the substring checks hold.
func ScoreBenchmark(task BenchmarkTask, output string) BenchmarkScore {
	var s BenchmarkScore
	lower := strings.ToLower(output)
	for _, m := range task.MustContain {
		if !strings.Contains(lower, strings.ToLower(m)) {
			s.Missing = append(s.Missing, m)
		}
	}
	for _, m := range task.MustNotContain {
		if strings.Contains(lower, strings.ToLower(m)) {
			s.Forbidden = append(s.Forbidden, m)
		}
	}
	jsonOK := true
	if task.ExpectJSON {
		valid := false
		if raw, ok := models.ExtractJSON(output); ok {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				valid = true
				if schema := task.SchemaJSON(); len(schema) > 0 {
					s.SchemaErrors = models.ValidateJSONSchema(schema, v)
					valid = len(s.SchemaErrors) == 0
				}
			}
		} else {
			s.SchemaErrors = []string{"no JSON found in output"}
		}
		s.ValidJSON = &valid
		jsonOK = valid
	}
	if strings.TrimSpace(task.Reference) != "" {
		f := TokenF1(task.Reference, output)
		s.F1 = &f
	}
	s.Passed = jsonOK && len(s.Missing) == 0 && len(s.Forbidden) == 0 && strings.TrimSpace(output) != ""
	return s
}

// SchemaJSON returns the task's JSON schema encoded as JSON (nil when absent).
func (t BenchmarkTask) SchemaJSON() json.RawMessage {
	if t.JSONSchema == nil {
		return nil
	}
	b, err := json.Marshal(t.JSONSchema)
	if err != nil {
		return nil
	}
	return b
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func tokens(s string) []string {
	return strings.Fields(nonAlnum.ReplaceAllString(strings.ToLower(s), " "))
}

// TokenF1 is the harmonic mean of token precision and recall of out against ref (0..1).
func TokenF1(ref, out string) float64 {
	rt, ot := tokens(ref), tokens(out)
	if len(rt) == 0 || len(ot) == 0 {
		return 0
	}
	counts := map[string]int{}
	for _, w := range rt {
		counts[w]++
	}
	common := 0
	for _, w := range ot {
		if counts[w] > 0 {
			common++
			counts[w]--
		}
	}
	if common == 0 {
		return 0
	}
	p := float64(common) / float64(len(ot))
	r := float64(common) / float64(len(rt))
	return 2 * p * r / (p + r)
}

// BenchmarkResult is one model's run of one benchmark task.
type BenchmarkResult struct {
	TaskID          string         `json:"task_id"`
	Task            string         `json:"task"`
	Model           string         `json:"model"` // provider/model
	Output          string         `json:"output"`
	Error           string         `json:"error,omitempty"`
	LatencyMS       int            `json:"latency_ms"`
	InputTokens     int            `json:"input_tokens"`
	OutputTokens    int            `json:"output_tokens"`
	TotalTokens     int            `json:"total_tokens"`
	TokensEstimated bool           `json:"tokens_estimated"`
	CostUSD         float64        `json:"cost_usd"`
	Score           BenchmarkScore `json:"score"`
}

// Caller performs routed model calls (implemented by *models.Gateway).
type Caller interface {
	Do(ctx context.Context, c models.Call) (*models.Result, error)
}

// RunBenchmarks runs every task on every model (pinned, no fallback) and scores the
// outputs. Models run concurrently; tasks for one model run sequentially to respect
// provider rate limits. Results are ordered by model then task.
func RunBenchmarks(ctx context.Context, gw Caller, modelKeys []string, tasks []BenchmarkTask, perCall time.Duration) []BenchmarkResult {
	if perCall <= 0 {
		perCall = 2 * time.Minute
	}
	out := make([][]BenchmarkResult, len(modelKeys))
	var wg sync.WaitGroup
	for i, key := range modelKeys {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			for _, task := range tasks {
				out[i] = append(out[i], runOne(ctx, gw, key, task, perCall))
			}
		}(i, key)
	}
	wg.Wait()
	var flat []BenchmarkResult
	for _, rs := range out {
		flat = append(flat, rs...)
	}
	return flat
}

func runOne(ctx context.Context, gw Caller, key string, task BenchmarkTask, perCall time.Duration) BenchmarkResult {
	res := BenchmarkResult{TaskID: task.ID, Task: task.Task, Model: key}
	req := models.Request{System: task.System, Messages: []models.Message{{Role: models.RoleUser, Content: task.Prompt}}, MaxTokens: 4000}
	if task.ExpectJSON {
		req.JSONSchema, req.SchemaName = task.SchemaJSON(), "benchmark_output"
	}
	cctx, cancel := context.WithTimeout(ctx, perCall)
	defer cancel()
	start := time.Now()
	r, err := gw.Do(cctx, models.Call{Task: models.TaskLab, Pin: key, Request: req, MaxAttempts: 1})
	res.LatencyMS = int(time.Since(start).Milliseconds())
	if err != nil {
		res.Error = err.Error()
		res.Score = BenchmarkScore{}
		return res
	}
	res.Output = r.Content
	res.LatencyMS = r.LatencyMS
	res.InputTokens, res.OutputTokens, res.TotalTokens = r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.Total()
	res.TokensEstimated = r.Usage.Estimated
	res.CostUSD = r.CostUSD
	res.Score = ScoreBenchmark(task, r.Content)
	return res
}

// ModelSummary aggregates one model's benchmark results.
type ModelSummary struct {
	Model        string  `json:"model"`
	Tasks        int     `json:"tasks"`
	Passed       int     `json:"passed"`
	Errors       int     `json:"errors"`
	JSONTasks    int     `json:"json_tasks"`
	JSONValid    int     `json:"json_valid"`
	AvgF1        float64 `json:"avg_f1"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// Summarize aggregates results per model, best pass rate first.
func Summarize(results []BenchmarkResult) []ModelSummary {
	by := map[string]*ModelSummary{}
	f1n := map[string]int{}
	var order []string
	for _, r := range results {
		s := by[r.Model]
		if s == nil {
			s = &ModelSummary{Model: r.Model}
			by[r.Model] = s
			order = append(order, r.Model)
		}
		s.Tasks++
		if r.Error != "" {
			s.Errors++
		}
		if r.Score.Passed {
			s.Passed++
		}
		if r.Score.ValidJSON != nil {
			s.JSONTasks++
			if *r.Score.ValidJSON {
				s.JSONValid++
			}
		}
		if r.Score.F1 != nil {
			s.AvgF1 += *r.Score.F1
			f1n[r.Model]++
		}
		s.AvgLatencyMS += float64(r.LatencyMS)
		s.TotalTokens += r.TotalTokens
		s.CostUSD += r.CostUSD
	}
	out := make([]ModelSummary, 0, len(order))
	for _, m := range order {
		s := by[m]
		if f1n[m] > 0 {
			s.AvgF1 /= float64(f1n[m])
		}
		if s.Tasks > 0 {
			s.AvgLatencyMS /= float64(s.Tasks)
		}
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Passed > out[j].Passed })
	return out
}

// FormatTable renders summaries as a fixed-width text table.
func FormatTable(sum []ModelSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-40s %7s %6s %9s %7s %10s %9s %10s\n", "MODEL", "PASSED", "ERRORS", "JSON OK", "AVG F1", "AVG MS", "TOKENS", "COST USD")
	for _, s := range sum {
		jsonCol := "-"
		if s.JSONTasks > 0 {
			jsonCol = fmt.Sprintf("%d/%d", s.JSONValid, s.JSONTasks)
		}
		fmt.Fprintf(&b, "%-40s %7s %6d %9s %7.2f %10.0f %9d %10.5f\n", truncate(s.Model, 40), fmt.Sprintf("%d/%d", s.Passed, s.Tasks), s.Errors, jsonCol,
			s.AvgF1, s.AvgLatencyMS, s.TotalTokens, s.CostUSD)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
