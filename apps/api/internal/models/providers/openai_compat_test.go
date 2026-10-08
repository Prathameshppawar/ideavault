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

const testOpenAIKey = "sk-test-0123456789abcdefSECRET"

// toolConversation is a canned multi-turn request with a tool loop.
func toolConversation(model string) models.Request {
	return models.Request{
		Model:  model,
		System: "You are IdeaVault.",
		Messages: []models.Message{
			{Role: models.RoleUser, Content: "Find notes about Go."},
			{Role: models.RoleAssistant, Content: "Searching.", ToolCalls: []models.ToolCall{
				{ID: "call_a", Name: "search_notes", Arguments: json.RawMessage(`{"query":"go"}`)},
				{ID: "call_b", Name: "list_tags", Arguments: json.RawMessage(`{}`)},
			}},
			{Role: models.RoleTool, ToolCallID: "call_a", Name: "search_notes", Content: `{"results":["n1"]}`},
			{Role: models.RoleTool, ToolCallID: "call_b", Name: "list_tags", Content: "go, rust"},
		},
		Tools: []models.ToolDef{{
			Name:        "search_notes",
			Description: "Search the user's notes.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		}, {
			Name:        "list_tags",
			Description: "List tags.",
		}},
		Temperature: ptrFloat(0.2),
		MaxTokens:   512,
	}
}

func newOpenAITestProvider(id, baseURL string) *OpenAICompatible {
	return NewOpenAICompatible(OpenAICompatConfig{ID: id, BaseURL: baseURL, APIKey: testOpenAIKey})
}

const openAITextAndToolsJSON = `{
  "id":"chatcmpl-1","object":"chat.completion","model":"gpt-x-2026",
  "choices":[{"index":0,"message":{"role":"assistant","content":"Here are results.",
    "reasoning_content":"SECRET CHAIN OF THOUGHT",
    "tool_calls":[
      {"id":"call_1","type":"function","function":{"name":"search_notes","arguments":"{\"query\":\"go\"}"}},
      {"id":"call_2","type":"function","function":{"name":"list_tags","arguments":"{}"}}
    ]},"finish_reason":"tool_calls"}],
  "usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}
}`

// openAITextAndToolsSSE is the streamed equivalent of openAITextAndToolsJSON,
// with tool-call arguments split across chunks.
var openAITextAndToolsSSE = []string{
	sseData(`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`),
	sseData(`{"choices":[{"index":0,"delta":{"reasoning":"SECRET CHAIN OF THOUGHT"}}]}`),
	sseData(`{"choices":[{"index":0,"delta":{"content":"Here are "}}]}`),
	sseData(`{"choices":[{"index":0,"delta":{"content":"results."}}]}`),
	sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search_notes","arguments":""}}]}}]}`),
	sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"que"}}]}}]}`),
	sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"list_tags","arguments":"{}"}}]}}]}`),
	sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ry\":\"go\"}"}}]}}]}`),
	sseData(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`),
	": keep-alive comment\n\n",
	sseData(`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}`),
	sseData(`[DONE]`),
}

func TestOpenAIRequestMapping(t *testing.T) {
	tests := []struct {
		id              string
		stream          bool
		wantMaxParam    string
		wantNoMaxParam  string
		wantStreamUsage bool
	}{
		{id: "openai", wantMaxParam: "max_completion_tokens", wantNoMaxParam: "max_tokens"},
		{id: "groq", wantMaxParam: "max_completion_tokens", wantNoMaxParam: "max_tokens"},
		{id: "ollama", wantMaxParam: "max_tokens", wantNoMaxParam: "max_completion_tokens"},
		{id: "openai", stream: true, wantMaxParam: "max_completion_tokens", wantNoMaxParam: "max_tokens", wantStreamUsage: true},
		{id: "groq", stream: true, wantMaxParam: "max_completion_tokens", wantNoMaxParam: "max_tokens", wantStreamUsage: true},
		{id: "ollama", stream: true, wantMaxParam: "max_tokens", wantNoMaxParam: "max_completion_tokens", wantStreamUsage: false},
	}
	for _, tc := range tests {
		name := tc.id
		if tc.stream {
			name += "/stream"
		}
		t.Run(name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
				if tc.stream {
					writeSSE(w, openAITextAndToolsSSE...)
					return
				}
				writeJSON(w, 200, openAITextAndToolsJSON)
			})
			p := newOpenAITestProvider(tc.id, srv.URL+"/v1/")
			req := toolConversation("model-x")
			req.JSONSchema = json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`)
			req.SchemaName = "note summary!"
			var err error
			if tc.stream {
				_, err = p.Stream(context.Background(), req, nil)
			} else {
				_, err = p.Complete(context.Background(), req)
			}
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			rec := srv.last(t)
			if rec.Path != "/v1/chat/completions" || rec.Method != http.MethodPost {
				t.Errorf("request = %s %s", rec.Method, rec.Path)
			}
			if got := rec.Header.Get("Authorization"); got != "Bearer "+testOpenAIKey {
				t.Errorf("Authorization = %q", got)
			}
			body := rec.JSON(t)

			if body[tc.wantMaxParam] != float64(512) {
				t.Errorf("%s = %v, want 512", tc.wantMaxParam, body[tc.wantMaxParam])
			}
			if _, ok := body[tc.wantNoMaxParam]; ok {
				t.Errorf("unexpected %s in body", tc.wantNoMaxParam)
			}
			if body["temperature"] != 0.2 {
				t.Errorf("temperature = %v", body["temperature"])
			}
			_, hasStreamOpts := body["stream_options"]
			if hasStreamOpts != tc.wantStreamUsage {
				t.Errorf("stream_options present = %v, want %v", hasStreamOpts, tc.wantStreamUsage)
			}
			if tc.wantStreamUsage && path(t, body, "stream_options", "include_usage") != true {
				t.Error("include_usage not true")
			}
			if (body["stream"] == true) != tc.stream {
				t.Errorf("stream = %v", body["stream"])
			}

			// System prompt first; then user; assistant with tool_calls; two tool messages.
			msgs := body["messages"].([]any)
			if len(msgs) != 5 {
				t.Fatalf("got %d messages: %v", len(msgs), msgs)
			}
			if path(t, msgs, 0, "role") != "system" || path(t, msgs, 0, "content") != "You are IdeaVault." {
				t.Errorf("first message = %v", msgs[0])
			}
			if path(t, msgs, 1, "role") != "user" {
				t.Errorf("second message = %v", msgs[1])
			}
			if path(t, msgs, 2, "role") != "assistant" || path(t, msgs, 2, "content") != "Searching." {
				t.Errorf("assistant = %v", msgs[2])
			}
			if path(t, msgs, 2, "tool_calls", 0, "type") != "function" ||
				path(t, msgs, 2, "tool_calls", 0, "id") != "call_a" ||
				path(t, msgs, 2, "tool_calls", 0, "function", "name") != "search_notes" ||
				path(t, msgs, 2, "tool_calls", 0, "function", "arguments") != `{"query":"go"}` {
				t.Errorf("assistant tool_calls = %v", path(t, msgs, 2, "tool_calls"))
			}
			if path(t, msgs, 3, "role") != "tool" || path(t, msgs, 3, "tool_call_id") != "call_a" ||
				path(t, msgs, 3, "content") != `{"results":["n1"]}` {
				t.Errorf("tool msg = %v", msgs[3])
			}
			if path(t, msgs, 4, "tool_call_id") != "call_b" {
				t.Errorf("second tool msg = %v", msgs[4])
			}

			// Tools.
			if body["tool_choice"] != "auto" {
				t.Errorf("tool_choice = %v", body["tool_choice"])
			}
			if path(t, body, "tools", 0, "type") != "function" ||
				path(t, body, "tools", 0, "function", "name") != "search_notes" ||
				path(t, body, "tools", 0, "function", "parameters", "required", 0) != "query" {
				t.Errorf("tools[0] = %v", path(t, body, "tools", 0))
			}
			if path(t, body, "tools", 1, "function", "parameters", "type") != "object" {
				t.Errorf("tools[1] params default = %v", path(t, body, "tools", 1, "function", "parameters"))
			}

			// Structured output.
			if path(t, body, "response_format", "type") != "json_schema" ||
				path(t, body, "response_format", "json_schema", "name") != "note_summary_" ||
				path(t, body, "response_format", "json_schema", "strict") != false ||
				path(t, body, "response_format", "json_schema", "schema", "type") != "object" {
				t.Errorf("response_format = %v", body["response_format"])
			}
		})
	}
}

func TestOpenAICompleteAndStreamParity(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if strings.Contains(r.Header.Get("Accept"), "event-stream") {
			writeSSE(w, openAITextAndToolsSSE...)
			return
		}
		writeJSON(w, 200, openAITextAndToolsJSON)
	})
	p := newOpenAITestProvider("openai", srv.URL)
	req := toolConversation("gpt-x")

	full, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var rec deltaRecorder
	streamed, err := p.Stream(context.Background(), req, rec.handle)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if full.Content != "Here are results." {
		t.Errorf("content = %q", full.Content)
	}
	if strings.Contains(full.Content, "SECRET") || strings.Contains(streamed.Content, "SECRET") || strings.Contains(rec.joined(), "SECRET") {
		t.Error("reasoning leaked into visible output")
	}
	if got := rec.list(); len(got) != 2 || got[0] != "Here are " || got[1] != "results." {
		t.Errorf("deltas = %q", got)
	}
	if full.FinishReason != "tool_calls" || full.Model != "gpt-x" {
		t.Errorf("finish/model = %q/%q", full.FinishReason, full.Model)
	}
	if len(full.ToolCalls) != 2 || full.ToolCalls[0].ID != "call_1" || string(full.ToolCalls[0].Arguments) != `{"query":"go"}` ||
		full.ToolCalls[1].Name != "list_tags" {
		t.Errorf("tool calls = %+v", full.ToolCalls)
	}
	if full.Usage != (models.Usage{InputTokens: 120, OutputTokens: 30}) {
		t.Errorf("usage = %+v", full.Usage)
	}
	assertSameResponse(t, full, streamed)
}

func TestOpenAIFinishReasons(t *testing.T) {
	tests := []struct {
		finish, content, refusal, want, wantContent string
	}{
		{finish: "stop", content: "hi", want: "stop", wantContent: "hi"},
		{finish: "length", content: "trunc", want: "length", wantContent: "trunc"},
		{finish: "content_filter", content: "partial", want: "refusal", wantContent: refusalMessage},
		{finish: "stop", refusal: "I can't help with that.", want: "refusal", wantContent: "I can't help with that."},
	}
	for _, tc := range tests {
		t.Run(tc.finish+"/"+tc.want, func(t *testing.T) {
			msg := map[string]any{"role": "assistant", "content": tc.content}
			if tc.refusal != "" {
				msg["content"] = nil
				msg["refusal"] = tc.refusal
			}
			body := mustJSON(map[string]any{
				"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": tc.finish}},
				"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
			})
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) { writeJSON(w, 200, body) })
			resp, err := newOpenAITestProvider("groq", srv.URL).Complete(context.Background(), models.Request{Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			if resp.FinishReason != tc.want || resp.Content != tc.wantContent {
				t.Errorf("got %q/%q, want %q/%q", resp.FinishReason, resp.Content, tc.want, tc.wantContent)
			}
		})
	}
}

func TestOpenAIUsageEstimatedAndThinkStripped(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 200, `{"choices":[{"index":0,"message":{"role":"assistant","content":"<think>private</think>\nVisible answer"},"finish_reason":"stop"}]}`)
			return
		}
		writeSSE(w,
			sseData(`{"choices":[{"index":0,"delta":{"content":"<think>pri"}}]}`),
			sseData(`{"choices":[{"index":0,"delta":{"content":"vate</think>\n"}}]}`),
			sseData(`{"choices":[{"index":0,"delta":{"content":"Visible answer"}}]}`),
			sseData(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
			sseData(`[DONE]`),
		)
	})
	p := newOpenAITestProvider("ollama", srv.URL)
	req := models.Request{Model: "deepseek-r1", Messages: []models.Message{{Role: models.RoleUser, Content: "question?"}}}
	full, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var rec deltaRecorder
	streamed, err := p.Stream(context.Background(), req, rec.handle)
	if err != nil {
		t.Fatal(err)
	}
	if full.Content != "Visible answer" || rec.joined() != "Visible answer" {
		t.Errorf("content = %q, deltas = %q", full.Content, rec.joined())
	}
	if !full.Usage.Estimated || full.Usage.InputTokens == 0 || full.Usage.OutputTokens == 0 {
		t.Errorf("usage = %+v, want estimated", full.Usage)
	}
	assertSameResponse(t, full, streamed)
}

func TestOpenAIErrorClassification(t *testing.T) {
	tests := []struct {
		status    int
		body      string
		retryable bool
	}{
		{429, `{"error":{"message":"Rate limit reached","type":"requests"}}`, true},
		{400, `{"error":{"message":"Invalid value for messages"}}`, false},
		{401, `{"error":{"message":"Incorrect API key provided: ` + testOpenAIKey + `"}}`, false},
		{500, `{"error":{"message":"internal"}}`, true},
		{503, `Service Unavailable`, true},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) { writeJSON(w, tc.status, tc.body) })
			for _, stream := range []bool{false, true} {
				p := newOpenAITestProvider("openai", srv.URL)
				var err error
				if stream {
					_, err = p.Stream(context.Background(), models.Request{Model: "m"}, nil)
				} else {
					_, err = p.Complete(context.Background(), models.Request{Model: "m"})
				}
				pe := providerError(t, err)
				if pe.StatusCode != tc.status || pe.Retryable != tc.retryable || pe.Provider != "openai" {
					t.Errorf("stream=%v: got %+v", stream, pe)
				}
				if models.IsRetryable(err) != tc.retryable {
					t.Errorf("IsRetryable = %v", !tc.retryable)
				}
				if strings.Contains(err.Error(), testOpenAIKey) {
					t.Errorf("error leaks key: %v", err)
				}
			}
		})
	}
}

func TestOpenAIJSONObjectFallback(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 400, `{"error":{"message":"response_format json_schema is not supported with this model","type":"invalid_request_error"}}`)
			return
		}
		writeJSON(w, 200, `{"choices":[{"index":0,"message":{"role":"assistant","content":"{\"title\":\"Go\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`)
	})
	p := newOpenAITestProvider("groq", srv.URL)
	req := models.Request{
		Model:      "llama-x",
		System:     "Summarize.",
		Messages:   []models.Message{{Role: models.RoleUser, Content: "Go is great"}},
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`),
	}
	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != `{"title":"Go"}` {
		t.Errorf("content = %q", resp.Content)
	}
	reqs := srv.requests()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2", len(reqs))
	}
	first, second := reqs[0].JSON(t), reqs[1].JSON(t)
	if path(t, first, "response_format", "type") != "json_schema" {
		t.Errorf("first response_format = %v", first["response_format"])
	}
	if path(t, second, "response_format", "type") != "json_object" {
		t.Errorf("second response_format = %v", second["response_format"])
	}
	sys, _ := path(t, second, "messages", 0, "content").(string)
	if !strings.HasPrefix(sys, "Summarize.") || !strings.Contains(sys, "JSON Schema") || !strings.Contains(sys, `"required":["title"]`) {
		t.Errorf("fallback system prompt = %q", sys)
	}

	// A 400 unrelated to response_format is not retried.
	srv2 := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeJSON(w, 400, `{"error":{"message":"messages must not be empty"}}`)
	})
	_, err = newOpenAITestProvider("groq", srv2.URL).Complete(context.Background(), req)
	if err == nil || len(srv2.requests()) != 1 {
		t.Errorf("unrelated 400: err=%v requests=%d", err, len(srv2.requests()))
	}
}

func TestOpenAITemperatureRetry(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 400, `{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model."}}`)
			return
		}
		writeJSON(w, 200, `{"choices":[{"index":0,"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	resp, err := newOpenAITestProvider("openai", srv.URL).Complete(context.Background(),
		models.Request{Model: "o-x", Temperature: ptrFloat(0.7), Messages: []models.Message{{Role: models.RoleUser, Content: "hi"}}})
	if err != nil || resp.Content != "ok" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if _, ok := srv.requests()[1].JSON(t)["temperature"]; ok {
		t.Error("temperature still sent on retry")
	}
}

func TestOpenAIStreamCancel(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeSSE(w, sseData(`{"choices":[{"index":0,"delta":{"content":"first"}}]}`))
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	p := newOpenAITestProvider("openai", srv.URL)
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

func TestOpenAIStreamTruncated(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeSSE(w, sseData(`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`))
	})
	_, err := newOpenAITestProvider("openai", srv.URL).Stream(context.Background(), models.Request{Model: "m"}, nil)
	if pe := providerError(t, err); !pe.Retryable {
		t.Errorf("truncated stream should be retryable: %+v", pe)
	}
}

func TestOpenAIToolCallsWithoutIndex(t *testing.T) {
	// Some local servers send whole tool calls without index, with object args.
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeSSE(w,
			sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"t1","function":{"name":"a","arguments":{"x":1}}}]}}]}`),
			sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"t2","function":{"name":"b","arguments":"{\"y\":2}"}}]}}]}`),
			sseData(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
			sseData(`[DONE]`),
		)
	})
	resp, err := newOpenAITestProvider("ollama", srv.URL).Stream(context.Background(), models.Request{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.FinishReason != "tool_calls" || len(resp.ToolCalls) != 2 ||
		string(resp.ToolCalls[0].Arguments) != `{"x":1}` || string(resp.ToolCalls[1].Arguments) != `{"y":2}` {
		t.Errorf("resp = %+v", resp)
	}
}

func TestOpenAIListModels(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeJSON(w, 200, `{"object":"list","data":[{"id":"gpt-b"},{"id":"gpt-a"},{"id":"gpt-a"}]}`)
	})
	ids, err := newOpenAITestProvider("openai", srv.URL+"/v1").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "gpt-a,gpt-b" {
		t.Errorf("ids = %v", ids)
	}
	if rec := srv.last(t); rec.Method != http.MethodGet || rec.Path != "/v1/models" {
		t.Errorf("request = %s %s", rec.Method, rec.Path)
	}
}

func TestOpenAINotConfigured(t *testing.T) {
	p := NewOpenAICompatible(OpenAICompatConfig{ID: "groq"})
	if _, err := p.Complete(context.Background(), models.Request{Model: "m"}); !errors.Is(err, models.ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
	// Local servers do not need a key.
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header")
		}
		writeJSON(w, 200, `{"choices":[{"index":0,"message":{"content":"local"},"finish_reason":"stop"}]}`)
	})
	resp, err := NewOpenAICompatible(OpenAICompatConfig{ID: "ollama", BaseURL: srv.URL}).Complete(context.Background(), models.Request{Model: "llama"})
	if err != nil || resp.Content != "local" {
		t.Errorf("ollama: resp=%v err=%v", resp, err)
	}
	if got := NewOpenAICompatible(OpenAICompatConfig{ID: "groq"}).baseURL; got != DefaultGroqBaseURL {
		t.Errorf("groq default base = %q", got)
	}
}
