package models

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// fakeProvider returns scripted responses/errors and records requests.
type fakeProvider struct {
	id      string
	mu      sync.Mutex
	replies []fakeReply
	calls   []Request
}

type fakeReply struct {
	content string
	err     error
	usage   Usage
	deltas  []string // streamed before returning (err may still follow)
}

func (p *fakeProvider) ID() string { return p.id }

func (p *fakeProvider) next(req Request) fakeReply {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, req)
	if len(p.replies) == 0 {
		return fakeReply{content: "default reply"}
	}
	r := p.replies[0]
	if len(p.replies) > 1 {
		p.replies = p.replies[1:]
	}
	return r
}

func (p *fakeProvider) Complete(_ context.Context, req Request) (*Response, error) {
	r := p.next(req)
	if r.err != nil {
		return nil, r.err
	}
	return &Response{Content: r.content, Usage: r.usage, FinishReason: "stop"}, nil
}

func (p *fakeProvider) Stream(_ context.Context, req Request, onDelta StreamHandler) (*Response, error) {
	r := p.next(req)
	for _, d := range r.deltas {
		onDelta(d)
	}
	if r.err != nil {
		return nil, r.err
	}
	return &Response{Content: r.content, Usage: r.usage, FinishReason: "stop"}, nil
}

func (p *fakeProvider) numCalls() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.calls) }

// fakeRecorder captures usage events.
type fakeRecorder struct {
	mu     sync.Mutex
	events []domain.UsageEvent
	users  []*uuid.UUID
}

func (r *fakeRecorder) InsertUsage(_ context.Context, userID *uuid.UUID, u *domain.UsageEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, *u)
	r.users = append(r.users, userID)
	return nil
}

// newTestGateway wires providers a (best), b, c into a gateway for a strong-tier task.
func newTestGateway(rec UsageRecorder, provs ...*fakeProvider) *Gateway {
	gw := NewGateway(nil, rec, nil)
	var cfgs []domain.ModelConfig
	for i, p := range provs {
		gw.RegisterProvider(p)
		// Earlier providers get higher quality so they rank first.
		cfgs = append(cfgs, domain.ModelConfig{Provider: p.id, Model: p.id + "-model", Speed: 3, Quality: 5 - i, RelativeCost: 1, ContextLength: 200000,
			ToolCalling: true, StructuredOutput: true, Enabled: true, InputCostPerMTok: 2, OutputCostPerMTok: 10})
	}
	gw.SetModels(cfgs)
	return gw
}

func TestGatewayFallsBackAcrossProviders(t *testing.T) {
	a := &fakeProvider{id: "a", replies: []fakeReply{{err: &ProviderError{Provider: "a", StatusCode: 500, Retryable: true, Message: "boom"}}}}
	b := &fakeProvider{id: "b", replies: []fakeReply{{err: errors.New("connection reset")}}}
	c := &fakeProvider{id: "c", replies: []fakeReply{{content: "answer from c", usage: Usage{InputTokens: 1000, OutputTokens: 500}}}}
	rec := &fakeRecorder{}
	gw := newTestGateway(rec, a, b, c)
	user := uuid.New()
	res, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, UserID: &user, Request: Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if res.Content != "answer from c" || res.Provider != "c" || res.ModelKey != "c/c-model" {
		t.Errorf("result from wrong provider: %+v", res)
	}
	if len(res.Attempts) != 3 || res.Attempts[0].Error == "" || res.Attempts[1].Error == "" || res.Attempts[2].Error != "" {
		t.Errorf("attempts not recorded: %+v", res.Attempts)
	}
	if a.calls[0].Model != "a-model" || c.calls[0].Model != "c-model" {
		t.Error("gateway must set the model id on each attempt")
	}
	// cost = 1000*2/1e6 + 500*10/1e6 = 0.007
	if math.Abs(res.CostUSD-0.007) > 1e-12 {
		t.Errorf("CostUSD = %v, want 0.007", res.CostUSD)
	}
	if len(rec.events) != 3 {
		t.Fatalf("expected a usage event per attempt, got %d", len(rec.events))
	}
	if rec.events[0].Success || rec.events[0].Error == "" || rec.events[2].Success != true {
		t.Errorf("usage success flags wrong: %+v", rec.events)
	}
	last := rec.events[2]
	if last.Provider != "c" || last.InputTokens != 1000 || last.OutputTokens != 500 || last.TotalTokens != 1500 || last.Task != string(TaskSynthesis) || last.Operation != "chat" {
		t.Errorf("usage event wrong: %+v", last)
	}
	if math.Abs(last.EstimatedCostUSD-0.007) > 1e-12 || *rec.users[2] != user {
		t.Errorf("usage cost/user wrong: %+v", last)
	}
}

func TestGatewayMaxAttemptsAndNoModel(t *testing.T) {
	failing := func(id string) *fakeProvider {
		return &fakeProvider{id: id, replies: []fakeReply{{err: errors.New(id + " down")}}}
	}
	a, b, c, d := failing("a"), failing("b"), failing("c"), &fakeProvider{id: "d", replies: []fakeReply{{content: "late"}}}
	gw := newTestGateway(nil, a, b, c, d)
	_, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, MaxAttempts: 3})
	if err == nil || !strings.Contains(err.Error(), "c down") {
		t.Fatalf("expected the last error after 3 attempts, got %v", err)
	}
	if d.numCalls() != 0 {
		t.Error("gateway exceeded MaxAttempts")
	}
	empty := NewGateway(nil, nil, nil)
	if _, err := empty.Do(context.Background(), Call{Task: TaskSynthesis}); !errors.Is(err, ErrNoModel) {
		t.Errorf("no models → ErrNoModel, got %v", err)
	}
}

func TestGatewayStreamingDoesNotFallBackAfterOutput(t *testing.T) {
	a := &fakeProvider{id: "a", replies: []fakeReply{{deltas: []string{"partial "}, err: errors.New("stream cut")}}}
	b := &fakeProvider{id: "b", replies: []fakeReply{{content: "full"}}}
	gw := newTestGateway(&fakeRecorder{}, a, b)
	var got strings.Builder
	_, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, OnDelta: func(d string) { got.WriteString(d) }})
	if err == nil {
		t.Fatal("a stream that already emitted text must not silently switch providers")
	}
	if b.numCalls() != 0 || got.String() != "partial " {
		t.Errorf("fallback after output: b calls=%d text=%q", b.numCalls(), got.String())
	}
	// Without emitted output, streaming falls back normally.
	a2 := &fakeProvider{id: "a", replies: []fakeReply{{err: errors.New("refused")}}}
	b2 := &fakeProvider{id: "b", replies: []fakeReply{{content: "ok", deltas: []string{"o", "k"}}}}
	rec := &fakeRecorder{}
	gw2 := newTestGateway(rec, a2, b2)
	res, err := gw2.Do(context.Background(), Call{Task: TaskSynthesis, OnDelta: func(string) {}})
	if err != nil || res.Provider != "b" {
		t.Fatalf("stream fallback failed: %v %+v", err, res)
	}
	if rec.events[len(rec.events)-1].Operation != "chat_stream" {
		t.Errorf("streamed calls are accounted as chat_stream, got %s", rec.events[len(rec.events)-1].Operation)
	}
}

func TestGatewayPinnedModel(t *testing.T) {
	a := &fakeProvider{id: "a", replies: []fakeReply{{content: "from a"}}}
	b := &fakeProvider{id: "b", replies: []fakeReply{{err: errors.New("b failed")}}}
	gw := newTestGateway(nil, a, b)
	if _, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, Pin: "b/b-model"}); err == nil || !strings.Contains(err.Error(), "b failed") {
		t.Fatalf("pinned call must not fall back, got %v", err)
	}
	if a.numCalls() != 0 {
		t.Error("pinned call fell back to another provider")
	}
	if _, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, Pin: "nope/missing"}); !errors.Is(err, ErrNoModel) {
		t.Errorf("unknown pin → ErrNoModel, got %v", err)
	}
	// A registered model whose provider is not configured is unavailable.
	gw.SetModels(append(gw.Models(), domain.ModelConfig{Provider: "zzz", Model: "m", Enabled: true, ContextLength: 100000, Quality: 3, Speed: 3}))
	if _, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, Pin: "zzz/m"}); !errors.Is(err, ErrNoModel) || !strings.Contains(err.Error(), "not available") {
		t.Errorf("pin of unconfigured provider → ErrNoModel, got %v", err)
	}
}

func TestGatewayEstimatesMissingUsage(t *testing.T) {
	a := &fakeProvider{id: "a", replies: []fakeReply{{content: strings.Repeat("word ", 40)}}}
	gw := newTestGateway(&fakeRecorder{}, a)
	res, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, Request: Request{System: strings.Repeat("s", 400), Messages: []Message{{Role: RoleUser, Content: "hello there"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Usage.Estimated || res.Usage.InputTokens <= 100 || res.Usage.OutputTokens != 50 {
		t.Errorf("usage should be estimated when the provider omits it: %+v", res.Usage)
	}
	if res.Model != "a-model" {
		t.Errorf("model defaulted to %q", res.Model)
	}
}

func TestGatewayMockOnly(t *testing.T) {
	real := &fakeProvider{id: "openai", replies: []fakeReply{{content: "real"}}}
	mock := &fakeProvider{id: ProviderMock, replies: []fakeReply{{content: "offline"}}}
	gw := NewGateway(nil, nil, nil)
	gw.RegisterProvider(real)
	gw.RegisterProvider(mock)
	gw.SetModels([]domain.ModelConfig{
		{Provider: "openai", Model: "gpt", Enabled: true, Quality: 5, Speed: 5, ContextLength: 400000, ToolCalling: true},
		{Provider: ProviderMock, Model: MockModel, Enabled: true, Quality: 1, Speed: 5, ContextLength: 1000000, ToolCalling: true},
	})
	if !gw.HasRealModel(TaskSupervisor) {
		t.Fatal("real provider should serve the supervisor")
	}
	gw.SetMockOnly(true)
	if gw.HasRealModel(TaskSupervisor) || gw.ProviderConfigured("openai") || !gw.ProviderConfigured(ProviderMock) {
		t.Fatal("mock-only mode must hide real providers")
	}
	res, err := gw.Do(context.Background(), Call{Task: TaskSupervisor})
	if err != nil || res.Content != "offline" || real.numCalls() != 0 {
		t.Fatalf("mock-only routed to %+v (%v)", res, err)
	}
	for _, m := range gw.Models() {
		if m.Available != (m.Provider == ProviderMock) {
			t.Errorf("availability in mock-only mode wrong for %s", m.Provider)
		}
	}
}

func TestDoJSONRetriesOnInvalidOutput(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","required":["intent"],"properties":{"intent":{"type":"string","enum":["fork","search"]}}}`)
	a := &fakeProvider{id: "a", replies: []fakeReply{{content: "Sure! here you go: {\"intent\": \"dance\"}"}, {content: "```json\n{\"intent\": \"fork\"}\n```"}}}
	rec := &fakeRecorder{}
	gw := newTestGateway(rec, a)
	var out struct {
		Intent string `json:"intent"`
	}
	res, err := gw.DoJSON(context.Background(), Call{Task: TaskClassification, Request: Request{Messages: []Message{{Role: RoleUser, Content: "Fork it"}}}}, schema, "intent", &out)
	if err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if out.Intent != "fork" || res == nil {
		t.Errorf("decoded %+v", out)
	}
	if a.numCalls() != 2 {
		t.Fatalf("expected one retry, got %d calls", a.numCalls())
	}
	retry := a.calls[1]
	if len(retry.Messages) != 3 || retry.Messages[1].Role != RoleAssistant || !strings.Contains(retry.Messages[2].Content, "not in enum") {
		t.Errorf("retry must include the invalid output and the validation errors: %+v", retry.Messages)
	}
	if string(retry.JSONSchema) != string(schema) || retry.SchemaName != "intent" {
		t.Error("structured output schema must be sent to the provider")
	}
	if rec.events[0].Operation != "structured" {
		t.Errorf("structured calls are accounted as structured, got %s", rec.events[0].Operation)
	}

	// Two invalid outputs → error.
	bad := &fakeProvider{id: "a", replies: []fakeReply{{content: "no json here"}}}
	gw2 := newTestGateway(nil, bad)
	if _, err := gw2.DoJSON(context.Background(), Call{Task: TaskClassification}, schema, "intent", &out); err == nil || !strings.Contains(err.Error(), "no JSON") {
		t.Errorf("expected failure after retry, got %v", err)
	}
	if bad.numCalls() != 2 {
		t.Errorf("DoJSON should try exactly twice, tried %d", bad.numCalls())
	}
}

func TestRetryAfter(t *testing.T) {
	tests := []struct {
		err  error
		want time.Duration
		ok   bool
	}{
		{&ProviderError{StatusCode: 429, Message: "Rate limit reached. Please try again in 2.5s."}, 3 * time.Second, true},
		{&ProviderError{StatusCode: 429, Message: "please try again in 1m2.5s"}, 0, false}, // too long to wait inline
		{&ProviderError{StatusCode: 429, Message: "Try again in 0m10s"}, 10*time.Second + 500*time.Millisecond, true},
		{&ProviderError{StatusCode: 429, Message: "quota exceeded"}, 3 * time.Second, true},
		{&ProviderError{StatusCode: 500, Message: "try again in 1s"}, 0, false},
		{errors.New("try again in 1s"), 0, false},
	}
	for _, tt := range tests {
		got, ok := retryAfter(tt.err)
		if got != tt.want || ok != tt.ok {
			t.Errorf("retryAfter(%v) = (%v, %v), want (%v, %v)", tt.err, got, ok, tt.want, tt.ok)
		}
	}
	if !IsRetryable(&ProviderError{Retryable: true}) || IsRetryable(&ProviderError{}) || !IsRetryable(context.DeadlineExceeded) || IsRetryable(errors.New("x")) {
		t.Error("IsRetryable classification wrong")
	}
	pe := &ProviderError{Provider: "groq", StatusCode: 429, Message: "slow down"}
	if pe.Error() != "groq: HTTP 429: slow down" || (&ProviderError{Provider: "x", Message: "m"}).Error() != "x: m" {
		t.Errorf("ProviderError format: %q", pe.Error())
	}
}

func TestGatewayWaitsOutShortRateLimitOnce(t *testing.T) {
	a := &fakeProvider{id: "a", replies: []fakeReply{
		{err: &ProviderError{Provider: "a", StatusCode: 429, Message: "try again in 0.01s"}},
		{content: "after wait"},
	}}
	gw := newTestGateway(nil, a)
	var waited time.Duration
	res, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, OnRetry: func(w time.Duration, reason string) { waited = w }})
	if err != nil || res.Content != "after wait" {
		t.Fatalf("expected a successful retry after the rate limit: %v", err)
	}
	if waited <= 0 || waited > time.Second {
		t.Errorf("OnRetry got wait %v", waited)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &fakeProvider{id: "a", replies: []fakeReply{{err: &ProviderError{Provider: "a", StatusCode: 429, Message: "try again in 5s"}}}}
	if _, err := newTestGateway(nil, b).Do(ctx, Call{Task: TaskSynthesis}); err == nil {
		t.Error("a cancelled context must not wait for the rate limit")
	}
}

func TestCostUSD(t *testing.T) {
	m := domain.ModelConfig{InputCostPerMTok: 0.15, OutputCostPerMTok: 0.60}
	got := CostUSD(m, Usage{InputTokens: 2_000_000, OutputTokens: 500_000})
	if math.Abs(got-0.6) > 1e-9 {
		t.Errorf("CostUSD = %v, want 0.6", got)
	}
	if CostUSD(domain.ModelConfig{}, Usage{InputTokens: 10}) != 0 {
		t.Error("free models cost nothing")
	}
	if (Usage{InputTokens: 3, OutputTokens: 4}).Total() != 7 {
		t.Error("Usage.Total")
	}
	if embeddingCost(ProviderOpenAI, "text-embedding-3-small", 1_000_000) != 0.02 || embeddingCost("local", "hash", 1_000_000) != 0 {
		t.Error("embedding cost table wrong")
	}
}

type fakeEmbedder struct{ provider string }

func (f fakeEmbedder) ProviderID() string { return f.provider }
func (f fakeEmbedder) Model() string      { return "emb-1" }
func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, Usage, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, EmbeddingDims)
	}
	return out, Usage{InputTokens: 7 * len(texts)}, nil
}

func TestGatewayEmbedRecordsUsageForPaidEmbedders(t *testing.T) {
	rec := &fakeRecorder{}
	gw := NewGateway(nil, rec, nil)
	if _, err := gw.Embed(context.Background(), nil, []string{"x"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("no embedder → ErrNotConfigured, got %v", err)
	}
	gw.SetEmbedder(fakeEmbedder{provider: ProviderOpenAI})
	vecs, err := gw.Embed(context.Background(), nil, []string{"a", "b"})
	if err != nil || len(vecs) != 2 || len(vecs[0]) != EmbeddingDims {
		t.Fatalf("Embed = %d vectors, %v", len(vecs), err)
	}
	if len(rec.events) != 1 || rec.events[0].Operation != "embed" || rec.events[0].InputTokens != 14 {
		t.Errorf("embedding usage not recorded: %+v", rec.events)
	}
	gw.SetEmbedder(fakeEmbedder{provider: "local"})
	_, _ = gw.Embed(context.Background(), nil, []string{"a"})
	if len(rec.events) != 1 {
		t.Error("the free local embedder is not accounted")
	}
}

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 || EstimateTokens("a") != 1 || EstimateTokens(strings.Repeat("x", 400)) != 100 {
		t.Error("EstimateTokens heuristic changed")
	}
	req := Request{System: strings.Repeat("s", 40), Messages: []Message{{Role: RoleUser, Content: strings.Repeat("u", 40),
		ToolCalls: []ToolCall{{Arguments: json.RawMessage(`{"a":"bbbbbbbbbbbb"}`)}}}}, Tools: []ToolDef{{Description: strings.Repeat("d", 40), Parameters: json.RawMessage(`{}`)}}}
	if n := EstimateRequestTokens(req); n != 10+10+4+5+8+10+1 {
		t.Errorf("EstimateRequestTokens = %d", n)
	}
}
