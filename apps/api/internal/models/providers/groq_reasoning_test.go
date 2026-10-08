package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

func TestGroqReasoningParams(t *testing.T) {
	tests := []struct {
		id, model     string
		custom        func(string) map[string]any
		wantInclude   any // expected include_reasoning (nil = absent)
		wantFormat    any // expected reasoning_format (nil = absent)
		wantMaxParam  string
		wantExtraKeys map[string]any
	}{
		{id: "groq", model: "openai/gpt-oss-20b", wantInclude: false, wantMaxParam: "max_completion_tokens"},
		{id: "groq", model: "openai/gpt-oss-120b", wantInclude: false, wantMaxParam: "max_completion_tokens"},
		{id: "groq", model: "qwen/qwen3.8-27b", wantFormat: "hidden", wantMaxParam: "max_completion_tokens"},
		{id: "groq", model: "llama-3.3-70b-versatile", wantMaxParam: "max_completion_tokens"},
		// The Groq rule only applies to the groq provider.
		{id: "openai", model: "openai/gpt-oss-20b", wantMaxParam: "max_completion_tokens"},
		{id: "ollama", model: "qwen/qwen3.8-27b", wantMaxParam: "max_tokens"},
		// Custom hook; extras never override standard fields.
		{id: "ollama", model: "qwen3", wantMaxParam: "max_tokens",
			custom:        func(string) map[string]any { return map[string]any{"think": false, "model": "evil", "max_tokens": 1} },
			wantExtraKeys: map[string]any{"think": false}},
	}
	for _, tc := range tests {
		for _, stream := range []bool{false, true} {
			name := tc.id + "/" + tc.model
			if stream {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
					if stream {
						writeSSE(w, sseData(`{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`), sseData("[DONE]"))
						return
					}
					writeJSON(w, 200, `{"choices":[{"index":0,"message":{"content":"ok"},"finish_reason":"stop"}]}`)
				})
				p := NewOpenAICompatible(OpenAICompatConfig{ID: tc.id, BaseURL: srv.URL, APIKey: "k-123456", ReasoningParams: tc.custom})
				req := models.Request{Model: tc.model, MaxTokens: 100, Messages: []models.Message{{Role: models.RoleUser, Content: "hi"}}}
				var err error
				if stream {
					_, err = p.Stream(context.Background(), req, nil)
				} else {
					_, err = p.Complete(context.Background(), req)
				}
				if err != nil {
					t.Fatal(err)
				}
				body := srv.last(t).JSON(t)
				if got := body["include_reasoning"]; got != tc.wantInclude {
					t.Errorf("include_reasoning = %v, want %v", got, tc.wantInclude)
				}
				if got := body["reasoning_format"]; got != tc.wantFormat {
					t.Errorf("reasoning_format = %v, want %v", got, tc.wantFormat)
				}
				if body[tc.wantMaxParam] != float64(100) {
					t.Errorf("%s = %v", tc.wantMaxParam, body[tc.wantMaxParam])
				}
				if body["model"] != tc.model {
					t.Errorf("model overridden: %v", body["model"])
				}
				for k, v := range tc.wantExtraKeys {
					if body[k] != v {
						t.Errorf("extra %s = %v, want %v", k, body[k], v)
					}
				}
			})
		}
	}
}

func TestGroqReasoningParamRejectedIsRetriedWithout(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 400, `{"error":{"message":"property 'include_reasoning' is unsupported for this model","type":"invalid_request_error"}}`)
			return
		}
		writeJSON(w, 200, `{"choices":[{"index":0,"message":{"content":"ok","reasoning":"hidden thoughts"},"finish_reason":"stop"}]}`)
	})
	p := NewOpenAICompatible(OpenAICompatConfig{ID: "groq", BaseURL: srv.URL, APIKey: "k-123456"})
	resp, err := p.Complete(context.Background(), models.Request{Model: "openai/gpt-oss-20b"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q", resp.Content)
	}
	reqs := srv.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	if _, ok := reqs[1].JSON(t)["include_reasoning"]; ok {
		t.Error("include_reasoning still sent after rejection")
	}
}

// TestGroqReasoningNeverSurfaces covers gpt-oss (separate reasoning fields,
// fc_ tool ids) and Qwen (inline <think> / <thinking> blocks split across
// chunks) for both Complete and Stream.
func TestGroqReasoningNeverSurfaces(t *testing.T) {
	const secret = "PRIVATE-REASONING"
	tests := []struct {
		name        string
		model       string
		json        string
		sse         []string
		wantContent string
		wantCalls   []models.ToolCall
	}{
		{
			name:  "gpt-oss reasoning field and fc ids",
			model: "openai/gpt-oss-120b",
			json: `{"choices":[{"index":0,"message":{"role":"assistant","content":"Checking notes.",
			  "reasoning":"` + secret + `","reasoning_content":"` + secret + `",
			  "tool_calls":[{"id":"fc_3f2b9c1e-8a4d-4f1e-9b7a-2c6d5e4f3a21","type":"function","function":{"name":"search_notes","arguments":"{\"query\":\"go\"}"}},
			                {"id":"x7","type":"function","function":{"name":"list_tags","arguments":""}}]},
			  "finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9,"completion_tokens":4}}`,
			sse: []string{
				sseData(`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"` + secret + `"}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"reasoning_content":"` + secret + `"}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"content":"Checking "}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"content":"notes."}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"fc_3f2b9c1e-8a4d-4f1e-9b7a-2c6d5e4f3a21","type":"function","function":{"name":"search_notes","arguments":"{\"query\":"}}]}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"go\"}"}}]}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"x7","type":"function","function":{"name":"list_tags","arguments":""}}]}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"x_groq":{"usage":{"prompt_tokens":9,"completion_tokens":4}}}`),
				sseData(`[DONE]`),
			},
			wantContent: "Checking notes.",
			wantCalls: []models.ToolCall{
				{ID: "fc_3f2b9c1e-8a4d-4f1e-9b7a-2c6d5e4f3a21", Name: "search_notes", Arguments: json.RawMessage(`{"query":"go"}`)},
				{ID: "x7", Name: "list_tags", Arguments: json.RawMessage(`{}`)},
			},
		},
		{
			name:  "qwen inline think blocks",
			model: "qwen/qwen3.8-27b",
			json: `{"choices":[{"index":0,"message":{"role":"assistant",
			  "content":"<think>` + secret + `</think>\n\nThe answer is 42. <thinking>` + secret + ` again</thinking> Done."},
			  "finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":4}}`,
			sse: []string{
				sseData(`{"choices":[{"index":0,"delta":{"content":"<thi"}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"content":"nk>` + secret[:7] + `"}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"content":"` + secret[7:] + `</thi"}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"content":"nk>\n\nThe answer"}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"content":" is 42. <"}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"content":"thinking>` + secret + ` again</thinking"}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{"content":"> Done."}}]}`),
				sseData(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
				sseData(`{"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":4}}`),
				sseData(`[DONE]`),
			},
			wantContent: "The answer is 42. Done.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
				if call == 0 {
					writeJSON(w, 200, tc.json)
					return
				}
				writeSSE(w, tc.sse...)
			})
			p := NewOpenAICompatible(OpenAICompatConfig{ID: "groq", BaseURL: srv.URL, APIKey: "k-123456"})
			req := models.Request{Model: tc.model, Messages: []models.Message{{Role: models.RoleUser, Content: "q"}}}
			full, err := p.Complete(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			var rec deltaRecorder
			streamed, err := p.Stream(context.Background(), req, rec.handle)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range append(rec.list(), full.Content, streamed.Content) {
				if strings.Contains(s, secret) || strings.Contains(s, "think") {
					t.Errorf("reasoning surfaced: %q", s)
				}
			}
			if full.Content != tc.wantContent || rec.joined() != tc.wantContent {
				t.Errorf("content = %q, deltas = %q, want %q", full.Content, rec.joined(), tc.wantContent)
			}
			if len(full.ToolCalls) != len(tc.wantCalls) {
				t.Fatalf("tool calls = %+v", full.ToolCalls)
			}
			for i, want := range tc.wantCalls {
				got := full.ToolCalls[i]
				if got.ID != want.ID || got.Name != want.Name || !jsonEqual(got.Arguments, want.Arguments) {
					t.Errorf("tool call %d = %+v (%s), want %+v (%s)", i, got, got.Arguments, want, want.Arguments)
				}
			}
			if full.Usage != (models.Usage{InputTokens: 9, OutputTokens: 4}) {
				t.Errorf("usage = %+v", full.Usage)
			}
			assertSameResponse(t, full, streamed)
		})
	}
}
