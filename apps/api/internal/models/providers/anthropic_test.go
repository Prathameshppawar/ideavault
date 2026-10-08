package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

const testAnthropicKey = "sk-ant-test-0123456789SECRET"

func newAnthropicTestProvider(baseURL string) *Anthropic {
	return NewAnthropic(AnthropicConfig{APIKey: testAnthropicKey, BaseURL: baseURL})
}

const anthropicToolTurnJSON = `{
  "id":"msg_01","type":"message","role":"assistant","model":"claude-opus-5-5",
  "content":[
    {"type":"thinking","thinking":"","signature":"SIG-ABC"},
    {"type":"text","text":"Let me search."},
    {"type":"tool_use","id":"toolu_01","name":"search_notes","input":{"query":"go"}},
    {"type":"tool_use","id":"toolu_02","name":"list_tags","input":{}}
  ],
  "stop_reason":"tool_use","stop_sequence":null,
  "usage":{"input_tokens":100,"output_tokens":40,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
}`

// anthropicToolTurnSSE streams the same message as anthropicToolTurnJSON.
var anthropicToolTurnSSE = []string{
	sseEventFrame("message_start", `{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`),
	sseEventFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`),
	sseEventFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG-ABC"}}`),
	sseEventFrame("content_block_stop", `{"type":"content_block_stop","index":0}`),
	sseEventFrame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
	sseEventFrame("ping", `{"type":"ping"}`),
	sseEventFrame("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Let me "}}`),
	sseEventFrame("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"search."}}`),
	sseEventFrame("content_block_stop", `{"type":"content_block_stop","index":1}`),
	sseEventFrame("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_01","name":"search_notes","input":{}}}`),
	sseEventFrame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"que"}}`),
	sseEventFrame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"ry\": \"go\"}"}}`),
	sseEventFrame("content_block_stop", `{"type":"content_block_stop","index":2}`),
	sseEventFrame("content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_02","name":"list_tags","input":{}}}`),
	sseEventFrame("content_block_stop", `{"type":"content_block_stop","index":3}`),
	sseEventFrame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":40}}`),
	sseEventFrame("message_stop", `{"type":"message_stop"}`),
}

const anthropicTextJSON = `{"id":"msg_02","type":"message","role":"assistant","model":"m",
  "content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn","stop_sequence":null,
  "usage":{"input_tokens":10,"output_tokens":2}}`

func TestAnthropicRequestMapping(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "complete"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
				if stream {
					writeSSE(w, anthropicToolTurnSSE...)
					return
				}
				writeJSON(w, 200, anthropicToolTurnJSON)
			})
			p := newAnthropicTestProvider(srv.URL)
			req := toolConversation("claude-opus-5-5")
			req.MaxTokens = 0
			// A user message after the tool results joins the same user turn.
			req.Messages = append(req.Messages, models.Message{Role: models.RoleUser, Content: "Also check tags."})
			req.JSONSchema = json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","minLength":3}},"required":["title"]}`)
			var err error
			if stream {
				_, err = p.Stream(context.Background(), req, nil)
			} else {
				_, err = p.Complete(context.Background(), req)
			}
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			rec := srv.last(t)
			if rec.Path != "/v1/messages" {
				t.Errorf("path = %s", rec.Path)
			}
			if rec.Header.Get("X-Api-Key") != testAnthropicKey {
				t.Errorf("x-api-key header missing")
			}
			body := rec.JSON(t)

			wantMax := float64(anthropicDefaultMaxTokens)
			if stream {
				wantMax = anthropicDefaultStreamMaxTokens
				if body["stream"] != true {
					t.Errorf("stream = %v", body["stream"])
				}
			}
			if body["max_tokens"] != wantMax {
				t.Errorf("max_tokens = %v, want %v", body["max_tokens"], wantMax)
			}
			if _, ok := body["thinking"]; ok {
				t.Errorf("thinking must not be sent: %v", body["thinking"])
			}
			if _, ok := body["temperature"]; ok {
				t.Errorf("temperature must not be sent to claude-opus-5-5")
			}
			if path(t, body, "system", 0, "text") != "You are IdeaVault." {
				t.Errorf("system = %v", body["system"])
			}
			if path(t, body, "tool_choice", "type") != "auto" {
				t.Errorf("tool_choice = %v", body["tool_choice"])
			}
			if path(t, body, "tools", 0, "name") != "search_notes" ||
				path(t, body, "tools", 0, "input_schema", "type") != "object" ||
				path(t, body, "tools", 0, "input_schema", "properties", "query", "type") != "string" ||
				path(t, body, "tools", 0, "input_schema", "required", 0) != "query" {
				t.Errorf("tools[0] = %v", path(t, body, "tools", 0))
			}
			if path(t, body, "tools", 1, "input_schema", "type") != "object" {
				t.Errorf("tools[1] = %v", path(t, body, "tools", 1))
			}

			// Structured output via output_config.format, normalized by the SDK.
			if path(t, body, "output_config", "format", "type") != "json_schema" ||
				path(t, body, "output_config", "format", "schema", "additionalProperties") != false {
				t.Errorf("output_config = %v", body["output_config"])
			}

			msgs := body["messages"].([]any)
			if len(msgs) != 3 {
				t.Fatalf("got %d messages, want 3: %s", len(msgs), mustJSON(msgs))
			}
			if path(t, msgs, 0, "role") != "user" || path(t, msgs, 0, "content", 0, "text") != "Find notes about Go." {
				t.Errorf("msg0 = %v", msgs[0])
			}
			asst := path(t, msgs, 1, "content").([]any)
			if path(t, msgs, 1, "role") != "assistant" || len(asst) != 3 ||
				path(t, asst, 0, "type") != "text" ||
				path(t, asst, 1, "type") != "tool_use" || path(t, asst, 1, "id") != "call_a" ||
				path(t, asst, 1, "input", "query") != "go" ||
				path(t, asst, 2, "name") != "list_tags" {
				t.Errorf("assistant = %s", mustJSON(msgs[1]))
			}
			user := path(t, msgs, 2, "content").([]any)
			if path(t, msgs, 2, "role") != "user" || len(user) != 3 ||
				path(t, user, 0, "type") != "tool_result" || path(t, user, 0, "tool_use_id") != "call_a" ||
				path(t, user, 1, "type") != "tool_result" || path(t, user, 1, "tool_use_id") != "call_b" ||
				path(t, user, 1, "content", 0, "text") != "go, rust" ||
				path(t, user, 2, "type") != "text" {
				t.Errorf("tool results turn = %s", mustJSON(msgs[2]))
			}
		})
	}
}

func TestAnthropicTemperatureOnlyForLegacyModels(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) { writeJSON(w, 200, anthropicTextJSON) })
	p := newAnthropicTestProvider(srv.URL)
	for model, want := range map[string]bool{
		"claude-haiku-4-5":           true,
		"claude-sonnet-4-5-20250929": true,
		"claude-opus-4-1":            true,
		"claude-opus-4-7":            false,
		"claude-opus-5-5":            false,
		"claude-sonnet-5-5":          false,
		"claude-haiku-5-5":           false,
	} {
		_, err := p.Complete(context.Background(), models.Request{
			Model: model, Temperature: ptrFloat(0.3), Messages: []models.Message{{Role: models.RoleUser, Content: "hi"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, got := srv.last(t).JSON(t)["temperature"]
		if got != want {
			t.Errorf("%s: temperature sent = %v, want %v", model, got, want)
		}
	}
}

func TestAnthropicResponseAndStreamParity(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 200, anthropicToolTurnJSON)
			return
		}
		writeSSE(w, anthropicToolTurnSSE...)
	})
	p := newAnthropicTestProvider(srv.URL)
	req := models.Request{Model: "claude-opus-5-5", Messages: []models.Message{{Role: models.RoleUser, Content: "Find notes about Go."}}}

	full, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var rec deltaRecorder
	streamed, err := p.Stream(context.Background(), req, rec.handle)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if full.Content != "Let me search." || full.FinishReason != "tool_calls" || full.Model != "claude-opus-5-5" {
		t.Errorf("full = %+v", full)
	}
	if len(full.ToolCalls) != 2 || full.ToolCalls[0].ID != "toolu_01" || full.ToolCalls[0].Name != "search_notes" ||
		!jsonEqual(full.ToolCalls[0].Arguments, []byte(`{"query":"go"}`)) || string(full.ToolCalls[1].Arguments) != `{}` {
		t.Errorf("tool calls = %+v", full.ToolCalls)
	}
	if full.Usage != (models.Usage{InputTokens: 100, OutputTokens: 40}) {
		t.Errorf("usage = %+v", full.Usage)
	}
	if full.Replay == nil || full.Replay.Provider != "anthropic" || full.Replay.Model != "claude-opus-5-5" {
		t.Fatalf("replay = %+v", full.Replay)
	}
	if !strings.Contains(string(full.Replay.Content), "SIG-ABC") {
		t.Errorf("replay lacks thinking signature: %s", full.Replay.Content)
	}
	if got := rec.list(); len(got) != 2 || got[0] != "Let me " || got[1] != "search." {
		t.Errorf("deltas = %q", got)
	}
	assertSameResponse(t, full, streamed)
}

func TestAnthropicReplayRoundTrip(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 200, anthropicToolTurnJSON)
			return
		}
		writeJSON(w, 200, anthropicTextJSON)
	})
	p := newAnthropicTestProvider(srv.URL)
	user := models.Message{Role: models.RoleUser, Content: "Find notes about Go."}
	first, err := p.Complete(context.Background(), models.Request{Model: "claude-opus-5-5", Messages: []models.Message{user}})
	if err != nil {
		t.Fatal(err)
	}
	assistant := models.Message{Role: models.RoleAssistant, Content: first.Content, ToolCalls: first.ToolCalls, Replay: first.Replay}
	history := []models.Message{
		user, assistant,
		{Role: models.RoleTool, ToolCallID: "toolu_01", Name: "search_notes", Content: "n1"},
		{Role: models.RoleTool, ToolCallID: "toolu_02", Name: "list_tags", Content: "go"},
	}

	// Same model: the assistant turn is replayed verbatim, thinking block first.
	if _, err := p.Complete(context.Background(), models.Request{Model: "claude-opus-5-5", Messages: history}); err != nil {
		t.Fatal(err)
	}
	body := srv.last(t).JSON(t)
	asst := path(t, body, "messages", 1, "content")
	if path(t, asst, 0, "type") != "thinking" || path(t, asst, 0, "signature") != "SIG-ABC" {
		t.Errorf("thinking block not replayed first: %s", mustJSON(asst))
	}
	if !jsonEqual([]byte(mustJSON(asst)), first.Replay.Content) {
		t.Errorf("replayed content differs from Replay:\n  sent:   %s\n  replay: %s", mustJSON(asst), first.Replay.Content)
	}
	if n := len(path(t, body, "messages", 2, "content").([]any)); n != 2 {
		t.Errorf("tool results not grouped into one user message: %d blocks", n)
	}

	// Different model: thinking dropped, turn rebuilt from Content + ToolCalls.
	if _, err := p.Complete(context.Background(), models.Request{Model: "claude-sonnet-5-5", Messages: history}); err != nil {
		t.Fatal(err)
	}
	body = srv.last(t).JSON(t)
	asst = path(t, body, "messages", 1, "content")
	blocks := asst.([]any)
	if len(blocks) != 3 || path(t, blocks, 0, "type") != "text" || path(t, blocks, 1, "type") != "tool_use" || path(t, blocks, 2, "type") != "tool_use" {
		t.Errorf("rebuilt assistant turn = %s", mustJSON(asst))
	}
	if strings.Contains(mustJSON(body), "SIG-ABC") {
		t.Error("thinking signature sent to a different model")
	}

	// Non-thinking responses carry no replay state.
	second, err := p.Complete(context.Background(), models.Request{Model: "claude-opus-5-5", Messages: []models.Message{user}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Replay != nil {
		t.Errorf("unexpected replay for response without thinking: %s", second.Replay.Content)
	}
}

func TestAnthropicRefusal(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeJSON(w, 200, `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5",
		  "content":[{"type":"thinking","thinking":"","signature":"S"},{"type":"text","text":"partial out"},
		    {"type":"tool_use","id":"t","name":"x","input":{}}],
		  "stop_reason":"refusal","stop_sequence":null,
		  "stop_details":{"type":"refusal","category":"cyber","explanation":"This request was declined by a safety classifier."},
		  "usage":{"input_tokens":5,"output_tokens":3}}`)
	})
	resp, err := newAnthropicTestProvider(srv.URL).Complete(context.Background(), models.Request{Model: "claude-opus-5-5"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.FinishReason != "refusal" || !strings.HasPrefix(resp.Content, refusalMessage) ||
		!strings.Contains(resp.Content, "safety classifier") || strings.Contains(resp.Content, "partial out") {
		t.Errorf("refusal response = %+v", resp)
	}
	if len(resp.ToolCalls) != 0 || resp.Replay != nil {
		t.Errorf("refusal must not carry tool calls or replay: %+v", resp)
	}
}

func TestAnthropicStopReasons(t *testing.T) {
	for stop, want := range map[string]string{
		"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length",
		"model_context_window_exceeded": "length", "tool_use": "tool_calls",
	} {
		body := strings.Replace(anthropicTextJSON, `"end_turn"`, `"`+stop+`"`, 1)
		srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) { writeJSON(w, 200, body) })
		resp, err := newAnthropicTestProvider(srv.URL).Complete(context.Background(), models.Request{Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if resp.FinishReason != want {
			t.Errorf("%s -> %q, want %q", stop, resp.FinishReason, want)
		}
	}
}

func TestAnthropicErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		retryable bool
	}{
		{"rate limit", 429, `{"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}}`, true},
		{"invalid request", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"messages: field required"}}`, false},
		{"auth echoes key", 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key ` + testAnthropicKey + `"}}`, false},
		{"overloaded", 529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, true},
		{"server error", 500, `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) { writeJSON(w, tc.status, tc.body) })
			p := newAnthropicTestProvider(srv.URL)
			for _, stream := range []bool{false, true} {
				var err error
				if stream {
					_, err = p.Stream(context.Background(), models.Request{Model: "m"}, nil)
				} else {
					_, err = p.Complete(context.Background(), models.Request{Model: "m"})
				}
				pe := providerError(t, err)
				if pe.StatusCode != tc.status || pe.Retryable != tc.retryable || pe.Provider != "anthropic" {
					t.Errorf("stream=%v: got %+v", stream, pe)
				}
				if strings.Contains(err.Error(), testAnthropicKey) {
					t.Errorf("error leaks key: %v", err)
				}
			}
			if n := len(srv.requests()); n != 2 {
				t.Errorf("SDK retried: %d requests, want 2 (one per call)", n)
			}
		})
	}
}

func TestAnthropicMidStreamError(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeSSE(w,
			anthropicToolTurnSSE[0],
			sseEventFrame("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`),
		)
	})
	_, err := newAnthropicTestProvider(srv.URL).Stream(context.Background(), models.Request{Model: "m"}, nil)
	pe := providerError(t, err)
	if !pe.Retryable || pe.StatusCode != 529 || !strings.Contains(pe.Message, "Overloaded") {
		t.Errorf("mid-stream error = %+v", pe)
	}
}

func TestAnthropicStreamCancel(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeSSE(w,
			anthropicToolTurnSSE[0],
			sseEventFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseEventFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"first"}}`),
		)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	p := newAnthropicTestProvider(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var err error
	within(t, 3*time.Second, func() {
		_, err = p.Stream(ctx, models.Request{Model: "m"}, func(string) { cancel() })
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestAnthropicStructuredOutputFallback(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"output_config.format: structured outputs are not supported for this model"}}`)
			return
		}
		writeJSON(w, 200, `{"id":"m","type":"message","role":"assistant","model":"m",
		  "content":[{"type":"text","text":"Here it is:\n`+"```json\\n{\\\"title\\\": \\\"Go\\\"}\\n```"+`"}],
		  "stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":5}}`)
	})
	req := models.Request{
		Model:      "claude-legacy",
		System:     "Summarize.",
		Messages:   []models.Message{{Role: models.RoleUser, Content: "Go"}},
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
	}
	resp, err := newAnthropicTestProvider(srv.URL).Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"title": "Go"}` {
		t.Errorf("content = %q", resp.Content)
	}
	reqs := srv.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	second := reqs[1].JSON(t)
	if _, ok := second["output_config"]; ok {
		t.Error("output_config still sent on fallback")
	}
	if sys, _ := path(t, second, "system", 0, "text").(string); !strings.HasPrefix(sys, "Summarize.") || !strings.Contains(sys, "JSON Schema") {
		t.Errorf("fallback system = %q", sys)
	}
}

func TestAnthropicLargeMaxTokensCompleteUsesStreaming(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) { writeSSE(w, anthropicToolTurnSSE...) })
	resp, err := newAnthropicTestProvider(srv.URL).Complete(context.Background(), models.Request{Model: "claude-opus-5-5", MaxTokens: 64000})
	if err != nil {
		t.Fatal(err)
	}
	body := srv.last(t).JSON(t)
	if body["stream"] != true || body["max_tokens"] != float64(64000) {
		t.Errorf("body stream=%v max_tokens=%v", body["stream"], body["max_tokens"])
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestAnthropicNotConfigured(t *testing.T) {
	p := NewAnthropic(AnthropicConfig{})
	if _, err := p.Complete(context.Background(), models.Request{Model: "m"}); !errors.Is(err, models.ErrNotConfigured) {
		t.Errorf("Complete err = %v", err)
	}
	if _, err := p.Stream(context.Background(), models.Request{Model: "m"}, nil); !errors.Is(err, models.ErrNotConfigured) {
		t.Errorf("Stream err = %v", err)
	}
	if p.ID() != "anthropic" {
		t.Errorf("ID = %q", p.ID())
	}
}
