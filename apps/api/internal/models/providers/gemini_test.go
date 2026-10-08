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

const testGeminiKey = "AIzaTEST0123456789abcdefghijklmnopqrstuvw"

func newGeminiTestProvider(baseURL string) *Gemini {
	return NewGemini(GeminiConfig{APIKey: testGeminiKey, BaseURL: baseURL + "/v1beta"})
}

const geminiToolTurnJSON = `{
  "candidates":[{"content":{"role":"model","parts":[
     {"text":"I should search the notes.","thought":true},
     {"text":"Searching your notes."},
     {"functionCall":{"name":"search_notes","args":{"query":"go"}},"thoughtSignature":"GSIG1"},
     {"functionCall":{"name":"list_tags","args":{}}}
   ]},"finishReason":"STOP","index":0}],
  "usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":20,"thoughtsTokenCount":15,"totalTokenCount":85},
  "modelVersion":"gemini-x-001"
}`

var geminiToolTurnSSE = []string{
	sseData(`{"candidates":[{"content":{"role":"model","parts":[{"text":"I should search the notes.","thought":true}]}}],"usageMetadata":{"promptTokenCount":50}}`),
	sseData(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Searching "}]}}]}`),
	sseData(`{"candidates":[{"content":{"role":"model","parts":[{"text":"your notes."}]}}]}`),
	sseData(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"search_notes","args":{"query":"go"}},"thoughtSignature":"GSIG1"},{"functionCall":{"name":"list_tags","args":{}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":20,"thoughtsTokenCount":15,"totalTokenCount":85}}`),
}

func TestGeminiRequestMapping(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "complete"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
				if stream {
					writeSSE(w, geminiToolTurnSSE...)
					return
				}
				writeJSON(w, 200, geminiToolTurnJSON)
			})
			p := newGeminiTestProvider(srv.URL)
			req := toolConversation("gemini-x")
			req.Tools[0].Parameters = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
				"properties":{"query":{"type":"string","default":"","examples":["go"]}},"required":["query"]}`)
			req.JSONSchema = json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`)
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
			wantPath, wantQuery := "/v1beta/models/gemini-x:generateContent", ""
			if stream {
				wantPath, wantQuery = "/v1beta/models/gemini-x:streamGenerateContent", "alt=sse"
			}
			if rec.Path != wantPath || rec.Query != wantQuery {
				t.Errorf("url = %s?%s, want %s?%s", rec.Path, rec.Query, wantPath, wantQuery)
			}
			if rec.Header.Get("X-Goog-Api-Key") != testGeminiKey {
				t.Error("x-goog-api-key header missing")
			}
			if strings.Contains(rec.Query, testGeminiKey) {
				t.Error("API key must not be sent in the URL")
			}
			body := rec.JSON(t)

			sys, _ := path(t, body, "systemInstruction", "parts", 0, "text").(string)
			if !strings.HasPrefix(sys, "You are IdeaVault.") {
				t.Errorf("systemInstruction = %q", sys)
			}
			// Tools + JSON schema: JSON requested via instructions, not responseSchema.
			if !strings.Contains(sys, "JSON Schema") {
				t.Errorf("expected JSON instruction in system prompt: %q", sys)
			}
			if _, ok := body["generationConfig"].(map[string]any)["responseSchema"]; ok {
				t.Error("responseSchema must not be combined with function calling")
			}
			if path(t, body, "generationConfig", "temperature") != 0.2 || path(t, body, "generationConfig", "maxOutputTokens") != float64(512) {
				t.Errorf("generationConfig = %v", body["generationConfig"])
			}

			contents := body["contents"].([]any)
			if len(contents) != 3 {
				t.Fatalf("got %d contents: %s", len(contents), mustJSON(contents))
			}
			if path(t, contents, 0, "role") != "user" || path(t, contents, 0, "parts", 0, "text") != "Find notes about Go." {
				t.Errorf("contents[0] = %v", contents[0])
			}
			if path(t, contents, 1, "role") != "model" ||
				path(t, contents, 1, "parts", 0, "text") != "Searching." ||
				path(t, contents, 1, "parts", 1, "functionCall", "name") != "search_notes" ||
				path(t, contents, 1, "parts", 1, "functionCall", "args", "query") != "go" ||
				path(t, contents, 1, "parts", 2, "functionCall", "name") != "list_tags" {
				t.Errorf("contents[1] = %s", mustJSON(contents[1]))
			}
			resp := path(t, contents, 2, "parts").([]any)
			if path(t, contents, 2, "role") != "user" || len(resp) != 2 ||
				path(t, resp, 0, "functionResponse", "name") != "search_notes" ||
				path(t, resp, 0, "functionResponse", "response", "result", "results", 0) != "n1" ||
				path(t, resp, 1, "functionResponse", "name") != "list_tags" ||
				path(t, resp, 1, "functionResponse", "response", "result", "content") != "go, rust" {
				t.Errorf("contents[2] (grouped tool results) = %s", mustJSON(contents[2]))
			}

			decl0 := path(t, body, "tools", 0, "functionDeclarations", 0)
			if path(t, decl0, "name") != "search_notes" || path(t, decl0, "parameters", "type") != "OBJECT" ||
				path(t, decl0, "parameters", "properties", "query", "type") != "STRING" {
				t.Errorf("decl0 = %s", mustJSON(decl0))
			}
			for _, banned := range []string{"$schema", "additionalProperties", `"default"`, "examples"} {
				if strings.Contains(mustJSON(decl0), banned) {
					t.Errorf("declaration still contains %s: %s", banned, mustJSON(decl0))
				}
			}
			if _, ok := path(t, body, "tools", 0, "functionDeclarations", 1).(map[string]any)["parameters"]; ok {
				t.Error("parameter-less tool should omit parameters")
			}
			if path(t, body, "toolConfig", "functionCallingConfig", "mode") != "AUTO" {
				t.Errorf("toolConfig = %v", body["toolConfig"])
			}
		})
	}
}

func TestGeminiStructuredOutput(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeJSON(w, 200, `{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"title\":\"Go\"}"}]},"finishReason":"STOP"}],
		  "usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4}}`)
	})
	resp, err := newGeminiTestProvider(srv.URL).Complete(context.Background(), models.Request{
		Model:      "gemini-x",
		Messages:   []models.Message{{Role: models.RoleUser, Content: "Summarize"}},
		JSONSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"title":{"type":"string"}},"required":["title"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"title":"Go"}` {
		t.Errorf("content = %q", resp.Content)
	}
	body := srv.last(t).JSON(t)
	if path(t, body, "generationConfig", "responseMimeType") != "application/json" ||
		path(t, body, "generationConfig", "responseSchema", "type") != "OBJECT" ||
		path(t, body, "generationConfig", "responseSchema", "required", 0) != "title" {
		t.Errorf("generationConfig = %s", mustJSON(body["generationConfig"]))
	}
	if strings.Contains(mustJSON(body), "additionalProperties") {
		t.Error("responseSchema not sanitized")
	}
}

func TestGeminiResponseAndStreamParity(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 200, geminiToolTurnJSON)
			return
		}
		writeSSE(w, geminiToolTurnSSE...)
	})
	p := newGeminiTestProvider(srv.URL)
	req := models.Request{Model: "gemini-x", Messages: []models.Message{{Role: models.RoleUser, Content: "Find notes"}}}
	full, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var rec deltaRecorder
	streamed, err := p.Stream(context.Background(), req, rec.handle)
	if err != nil {
		t.Fatal(err)
	}
	if full.Content != "Searching your notes." || strings.Contains(full.Content, "should search") {
		t.Errorf("content = %q (thoughts must be skipped)", full.Content)
	}
	if strings.Contains(rec.joined(), "should search") || rec.joined() != "Searching your notes." {
		t.Errorf("deltas = %q", rec.list())
	}
	if full.FinishReason != "tool_calls" || full.Model != "gemini-x" {
		t.Errorf("finish/model = %q/%q", full.FinishReason, full.Model)
	}
	if len(full.ToolCalls) != 2 || full.ToolCalls[0].Name != "search_notes" || string(full.ToolCalls[0].Arguments) != `{"query":"go"}` ||
		!strings.HasPrefix(full.ToolCalls[0].ID, "call_0_") || !strings.HasPrefix(full.ToolCalls[1].ID, "call_1_") {
		t.Errorf("tool calls = %+v", full.ToolCalls)
	}
	if full.Usage != (models.Usage{InputTokens: 50, OutputTokens: 35}) {
		t.Errorf("usage = %+v (thoughts count as output)", full.Usage)
	}
	if full.Replay == nil || full.Replay.Provider != "gemini" || !strings.Contains(string(full.Replay.Content), "GSIG1") {
		t.Fatalf("replay = %+v", full.Replay)
	}
	assertSameResponse(t, full, streamed)
}

func TestGeminiReplayRoundTrip(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if call == 0 {
			writeJSON(w, 200, geminiToolTurnJSON)
			return
		}
		writeJSON(w, 200, `{"candidates":[{"content":{"role":"model","parts":[{"text":"Done."}]},"finishReason":"STOP"}]}`)
	})
	p := newGeminiTestProvider(srv.URL)
	user := models.Message{Role: models.RoleUser, Content: "Find notes"}
	first, err := p.Complete(context.Background(), models.Request{Model: "gemini-x", Messages: []models.Message{user}})
	if err != nil {
		t.Fatal(err)
	}
	history := []models.Message{
		user,
		{Role: models.RoleAssistant, Content: first.Content, ToolCalls: first.ToolCalls, Replay: first.Replay},
		{Role: models.RoleTool, ToolCallID: first.ToolCalls[0].ID, Content: `["n1"]`},
		{Role: models.RoleTool, ToolCallID: first.ToolCalls[1].ID, Content: "go"},
	}

	second, err := p.Complete(context.Background(), models.Request{Model: "gemini-x", Messages: history})
	if err != nil {
		t.Fatal(err)
	}
	if second.Replay != nil || !second.Usage.Estimated {
		t.Errorf("second: replay=%v usage=%+v", second.Replay, second.Usage)
	}
	body := srv.last(t).JSON(t)
	model := path(t, body, "contents", 1)
	if !jsonEqual([]byte(mustJSON(model)), first.Replay.Content) {
		t.Errorf("model turn not replayed verbatim:\n  sent:   %s\n  replay: %s", mustJSON(model), first.Replay.Content)
	}
	// Names for function responses are resolved from the assistant tool calls.
	if path(t, body, "contents", 2, "parts", 0, "functionResponse", "name") != "search_notes" ||
		path(t, body, "contents", 2, "parts", 1, "functionResponse", "name") != "list_tags" {
		t.Errorf("function responses = %s", mustJSON(path(t, body, "contents", 2)))
	}

	// A different model gets a rebuilt turn without signatures or thoughts.
	if _, err := p.Complete(context.Background(), models.Request{Model: "gemini-y", Messages: history}); err != nil {
		t.Fatal(err)
	}
	sent := mustJSON(srv.last(t).JSON(t))
	if strings.Contains(sent, "GSIG1") || strings.Contains(sent, "should search") {
		t.Errorf("replay leaked to a different model: %s", sent)
	}
}

func TestGeminiFinishReasons(t *testing.T) {
	tests := []struct {
		body, want string
	}{
		{`{"candidates":[{"content":{"parts":[{"text":"cut"}]},"finishReason":"MAX_TOKENS"}]}`, "length"},
		{`{"candidates":[{"content":{"parts":[{"text":"bad"}]},"finishReason":"SAFETY"}]}`, "refusal"},
		{`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`, "refusal"},
		{`{"candidates":[{"finishReason":"MALFORMED_FUNCTION_CALL"}]}`, "error"},
		{`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`, "stop"},
	}
	for _, tc := range tests {
		srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) { writeJSON(w, 200, tc.body) })
		resp, err := newGeminiTestProvider(srv.URL).Complete(context.Background(), models.Request{Model: "g"})
		if err != nil {
			t.Fatalf("%s: %v", tc.body, err)
		}
		if resp.FinishReason != tc.want {
			t.Errorf("%s: finish = %q, want %q", tc.body, resp.FinishReason, tc.want)
		}
		if tc.want == "refusal" && (!strings.HasPrefix(resp.Content, refusalMessage) || strings.Contains(resp.Content, "bad")) {
			t.Errorf("refusal content = %q", resp.Content)
		}
	}
}

func TestGeminiErrors(t *testing.T) {
	tests := []struct {
		status    int
		body      string
		retryable bool
	}{
		{429, `{"error":{"code":429,"message":"Resource has been exhausted","status":"RESOURCE_EXHAUSTED"}}`, true},
		{400, `{"error":{"code":400,"message":"API key not valid. key=` + testGeminiKey + `","status":"INVALID_ARGUMENT"}}`, false},
		{503, `{"error":{"code":503,"message":"The model is overloaded.","status":"UNAVAILABLE"}}`, true},
	}
	for _, tc := range tests {
		srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) { writeJSON(w, tc.status, tc.body) })
		for _, stream := range []bool{false, true} {
			p := newGeminiTestProvider(srv.URL)
			var err error
			if stream {
				_, err = p.Stream(context.Background(), models.Request{Model: "g"}, nil)
			} else {
				_, err = p.Complete(context.Background(), models.Request{Model: "g"})
			}
			pe := providerError(t, err)
			if pe.StatusCode != tc.status || pe.Retryable != tc.retryable || pe.Provider != "gemini" {
				t.Errorf("%d stream=%v: %+v", tc.status, stream, pe)
			}
			if strings.Contains(err.Error(), testGeminiKey) {
				t.Errorf("error leaks key: %v", err)
			}
		}
	}
}

func TestGeminiStreamCancel(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeSSE(w, geminiToolTurnSSE[1])
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var err error
	within(t, 3*time.Second, func() {
		_, err = newGeminiTestProvider(srv.URL).Stream(ctx, models.Request{Model: "g"}, func(string) { cancel() })
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestGeminiNotConfigured(t *testing.T) {
	if _, err := NewGemini(GeminiConfig{}).Complete(context.Background(), models.Request{Model: "g"}); !errors.Is(err, models.ErrNotConfigured) {
		t.Errorf("err = %v", err)
	}
}

func TestSanitizeGeminiSchema(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "full",
			in: `{
			  "$schema":"http://json-schema.org/draft-07/schema#","$id":"x","type":"object","additionalProperties":false,
			  "properties":{
			    "title":{"type":"string","default":"x","examples":["a"],"minLength":1},
			    "kind":{"const":"note"},
			    "tags":{"type":"array","items":{"type":"string","format":"uri"}},
			    "due":{"type":["string","null"],"format":"date-time"},
			    "ref":{"$ref":"#/$defs/Ref"},
			    "choice":{"anyOf":[{"type":"null"},{"type":"integer","format":"int64"}]},
			    "pick":{"oneOf":[{"type":"string","description":"first"},{"type":"number"}]},
			    "level":{"type":"integer","enum":[1,2,3]},
			    "meta":{"type":"object","additionalProperties":{"type":"string"}}
			  },
			  "required":["title","missing"],
			  "$defs":{"Ref":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}
			}`,
			want: `{"type":"OBJECT","required":["title"],"properties":{
			  "title":{"type":"STRING","minLength":1},
			  "kind":{"type":"STRING","enum":["note"]},
			  "tags":{"type":"ARRAY","items":{"type":"STRING"}},
			  "due":{"type":"STRING","nullable":true,"format":"date-time"},
			  "ref":{"type":"OBJECT","properties":{"id":{"type":"STRING"}},"required":["id"]},
			  "choice":{"type":"INTEGER","nullable":true,"format":"int64"},
			  "pick":{"type":"STRING","description":"first"},
			  "level":{"type":"INTEGER","description":"Allowed values: [1,2,3]"},
			  "meta":{"type":"OBJECT"}
			}}`,
		},
		{
			name: "array without items gets string items",
			in:   `{"type":"object","properties":{"xs":{"type":"array"}}}`,
			want: `{"type":"OBJECT","properties":{"xs":{"type":"ARRAY","items":{"type":"STRING"}}}}`,
		},
		{
			name: "recursive ref is bounded",
			in:   `{"type":"object","properties":{"node":{"$ref":"#/definitions/N"}},"definitions":{"N":{"type":"object","properties":{"v":{"type":"string"},"next":{"$ref":"#/definitions/N"}}}}}`,
		},
		{name: "empty object", in: `{"type":"object","properties":{}}`, want: ``},
		{name: "no schema", in: ``, want: ``},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sanitizeGeminiSchema(json.RawMessage(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "recursive ref is bounded" {
				if got == nil || !strings.Contains(string(got), `"v":{"type":"STRING"}`) {
					t.Errorf("got %s", got)
				}
				return
			}
			if tc.want == "" {
				if got != nil {
					t.Errorf("got %s, want nil", got)
				}
				return
			}
			if !jsonEqual(got, []byte(tc.want)) {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
	if _, err := sanitizeGeminiSchema(json.RawMessage(`[1]`)); err == nil {
		t.Error("non-object schema should error")
	}
}
