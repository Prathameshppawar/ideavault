package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// DefaultGeminiBaseURL is the Gemini (Generative Language) REST API root.
const DefaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// GeminiReplayProvider tags models.ReplayState produced by the Gemini provider.
const GeminiReplayProvider = "gemini"

// GeminiConfig configures the native Google Gemini provider.
type GeminiConfig struct {
	// ID is the registry provider key (default "gemini").
	ID string
	// BaseURL overrides the API root (default DefaultGeminiBaseURL).
	BaseURL string
	// APIKey is sent in the x-goog-api-key header. Without it every call
	// returns models.ErrNotConfigured.
	APIKey string
	// HTTPClient overrides the HTTP client.
	HTTPClient *http.Client
	// ExtraHeaders are added to every request.
	ExtraHeaders map[string]string
}

// Gemini is a models.Provider for the Gemini generateContent REST API.
// It is safe for concurrent use.
type Gemini struct {
	id      string
	baseURL string
	apiKey  string
	headers map[string]string
	http    httpCaller
}

var _ models.Provider = (*Gemini)(nil)

// NewGemini returns a Gemini provider.
func NewGemini(cfg GeminiConfig) *Gemini {
	id := cfg.ID
	if id == "" {
		id = "gemini"
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultGeminiBaseURL
	}
	return &Gemini{
		id:      id,
		baseURL: strings.TrimRight(base, "/"),
		apiKey:  cfg.APIKey,
		headers: geminiHeaders(cfg.APIKey, cfg.ExtraHeaders),
		http:    httpCaller{client: httpClientOrDefault(cfg.HTTPClient), provider: id, secrets: []string{cfg.APIKey}},
	}
}

func geminiHeaders(apiKey string, extra map[string]string) map[string]string {
	h := map[string]string{}
	for k, v := range extra {
		h[k] = v
	}
	if apiKey != "" {
		h["x-goog-api-key"] = apiKey
	}
	return h
}

// geminiModelURL builds {base}/models/{model}:{method}.
func geminiModelURL(base, model, method string) string {
	model = strings.TrimPrefix(model, "models/")
	return base + "/models/" + url.PathEscape(model) + ":" + method
}

// ID returns the registry provider key.
func (p *Gemini) ID() string { return p.id }

// Complete performs a generateContent call.
func (p *Gemini) Complete(ctx context.Context, req models.Request) (*models.Response, error) {
	return p.run(ctx, req, false, nil)
}

// Stream performs a streamGenerateContent (SSE) call, calling onDelta with
// visible text only (thought parts are skipped).
func (p *Gemini) Stream(ctx context.Context, req models.Request, onDelta models.StreamHandler) (*models.Response, error) {
	return p.run(ctx, req, true, onDelta)
}

// ---------------------------------------------------------------------------
// Wire types

type gemRequest struct {
	SystemInstruction *gemContent       `json:"systemInstruction,omitempty"`
	Contents          []json.RawMessage `json:"contents"`
	Tools             []gemTool         `json:"tools,omitempty"`
	ToolConfig        *gemToolConfig    `json:"toolConfig,omitempty"`
	GenerationConfig  *gemGenConfig     `json:"generationConfig,omitempty"`
}

type gemContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []gemPart `json:"parts"`
}

type gemPart struct {
	Text             string               `json:"text,omitempty"`
	Thought          bool                 `json:"thought,omitempty"`
	ThoughtSignature string               `json:"thoughtSignature,omitempty"`
	FunctionCall     *gemFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *gemFunctionResponse `json:"functionResponse,omitempty"`
}

type gemFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type gemFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type gemTool struct {
	FunctionDeclarations []gemFuncDecl `json:"functionDeclarations"`
}

type gemFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type gemToolConfig struct {
	FunctionCallingConfig struct {
		Mode string `json:"mode"`
	} `json:"functionCallingConfig"`
}

type gemGenConfig struct {
	Temperature      *float64        `json:"temperature,omitempty"`
	MaxOutputTokens  int             `json:"maxOutputTokens,omitempty"`
	ResponseMimeType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
}

type gemUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
}

type gemResponse struct {
	Candidates []struct {
		Content      *gemRawContent `json:"content"`
		FinishReason string         `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata *gemUsage       `json:"usageMetadata"`
	Error         json.RawMessage `json:"error"`
}

// gemRawContent keeps each part's raw JSON so model turns can be replayed
// byte-for-byte (thought signatures must be returned unchanged).
type gemRawContent struct {
	Role  string            `json:"role"`
	Parts []json.RawMessage `json:"parts"`
}

// ---------------------------------------------------------------------------
// Request building

type gemBuildOpts struct {
	jsonFallback bool // structured output via instructions instead of responseSchema
}

func (p *Gemini) buildRequest(req models.Request, o gemBuildOpts) (*gemRequest, error) {
	body := &gemRequest{}
	var system []string
	if req.System != "" {
		system = append(system, req.System)
	}

	type entry struct {
		role      string
		raw       json.RawMessage // verbatim replay content
		responses []gemPart       // functionResponse parts (first in a user turn)
		parts     []gemPart
	}
	var entries []*entry
	add := func(role string, raw json.RawMessage, responses, parts []gemPart) {
		if raw == nil && len(responses) == 0 && len(parts) == 0 {
			return
		}
		if n := len(entries); n > 0 && raw == nil && entries[n-1].raw == nil && entries[n-1].role == role {
			entries[n-1].responses = append(entries[n-1].responses, responses...)
			entries[n-1].parts = append(entries[n-1].parts, parts...)
			return
		}
		entries = append(entries, &entry{role: role, raw: raw, responses: responses, parts: parts})
	}

	callNames := map[string]string{} // tool call id -> function name
	for i, m := range req.Messages {
		switch m.Role {
		case models.RoleSystem:
			if m.Content != "" {
				system = append(system, m.Content)
			}
		case models.RoleUser:
			if strings.TrimSpace(m.Content) != "" {
				add("user", nil, nil, []gemPart{{Text: m.Content}})
			}
		case models.RoleAssistant:
			for _, tc := range m.ToolCalls {
				callNames[tc.ID] = tc.Name
			}
			if raw, ok := geminiReplayContent(m.Replay, req.Model); ok {
				add("model", raw, nil, nil)
				continue
			}
			var parts []gemPart
			if strings.TrimSpace(m.Content) != "" {
				parts = append(parts, gemPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				parts = append(parts, gemPart{FunctionCall: &gemFunctionCall{Name: tc.Name, Args: objectArgs(tc.Arguments)}})
			}
			add("model", nil, nil, parts)
		case models.RoleTool:
			name := m.Name
			if name == "" {
				name = callNames[m.ToolCallID]
			}
			if name == "" {
				return nil, p.http.errorf(0, false, "invalid request: tool message %d has no function name", i)
			}
			// Consecutive tool results are grouped into one user-role content.
			add("user", nil, []gemPart{{FunctionResponse: &gemFunctionResponse{Name: name, Response: geminiToolResponse(m.Content)}}}, nil)
		default:
			return nil, p.http.errorf(0, false, "invalid request: message %d has unsupported role %q", i, m.Role)
		}
	}
	for _, e := range entries {
		if e.raw != nil {
			body.Contents = append(body.Contents, e.raw)
			continue
		}
		b, err := json.Marshal(gemContent{Role: e.role, Parts: append(e.responses, e.parts...)})
		if err != nil {
			return nil, p.http.errorf(0, false, "encode contents: %v", err)
		}
		body.Contents = append(body.Contents, b)
	}

	if len(req.Tools) > 0 {
		decls := make([]gemFuncDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			params, err := sanitizeGeminiSchema(t.Parameters)
			if err != nil {
				return nil, p.http.errorf(0, false, "invalid request: tool %q parameters: %v", t.Name, err)
			}
			decls = append(decls, gemFuncDecl{Name: t.Name, Description: t.Description, Parameters: params})
		}
		body.Tools = []gemTool{{FunctionDeclarations: decls}}
		body.ToolConfig = &gemToolConfig{}
		body.ToolConfig.FunctionCallingConfig.Mode = "AUTO"
	}

	gen := &gemGenConfig{}
	if req.Temperature != nil {
		t := *req.Temperature
		gen.Temperature = &t
	}
	if req.MaxTokens > 0 {
		gen.MaxOutputTokens = req.MaxTokens
	}
	if len(req.JSONSchema) > 0 {
		// JSON mode cannot be combined with function calling; with tools (or
		// after a rejection) request JSON through instructions instead.
		if o.jsonFallback || len(req.Tools) > 0 {
			system = append(system, jsonInstruction(req.JSONSchema))
		} else {
			schema, err := sanitizeGeminiSchema(req.JSONSchema)
			if err != nil {
				return nil, p.http.errorf(0, false, "invalid request: JSON schema: %v", err)
			}
			gen.ResponseMimeType = "application/json"
			gen.ResponseSchema = schema
		}
	}
	if gen.Temperature != nil || gen.MaxOutputTokens > 0 || gen.ResponseMimeType != "" {
		body.GenerationConfig = gen
	}

	if len(system) > 0 {
		body.SystemInstruction = &gemContent{Parts: []gemPart{{Text: strings.Join(system, "\n\n")}}}
	}
	return body, nil
}

// geminiToolResponse wraps a tool result as {"result": <parsed JSON>} or,
// for non-JSON text, {"result": {"content": text}}.
func geminiToolResponse(content string) json.RawMessage {
	t := strings.TrimSpace(content)
	var result json.RawMessage
	if t != "" && json.Valid([]byte(t)) {
		result = json.RawMessage(t)
	} else {
		b, _ := json.Marshal(map[string]string{"content": content})
		result = b
	}
	out, _ := json.Marshal(map[string]json.RawMessage{"result": result})
	return out
}

// geminiReplayContent returns the stored model content when it was produced
// by Gemini for the same model.
func geminiReplayContent(r *models.ReplayState, model string) (json.RawMessage, bool) {
	if r == nil || r.Provider != GeminiReplayProvider || r.Model != model || len(r.Content) == 0 {
		return nil, false
	}
	if !json.Valid(r.Content) {
		return nil, false
	}
	return r.Content, true
}

// ---------------------------------------------------------------------------
// Execution

func (p *Gemini) run(ctx context.Context, req models.Request, stream bool, onDelta models.StreamHandler) (*models.Response, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("%s: %w", p.id, models.ErrNotConfigured)
	}
	method := "generateContent"
	if stream {
		method = "streamGenerateContent?alt=sse"
	}
	endpoint := geminiModelURL(p.baseURL, req.Model, method)
	headers := make(map[string]string, len(p.headers)+1)
	for k, v := range p.headers {
		headers[k] = v
	}
	if stream {
		headers["Accept"] = "text/event-stream"
	}

	var (
		opts gemBuildOpts
		resp *http.Response
	)
	for attempt := 0; ; attempt++ {
		body, err := p.buildRequest(req, opts)
		if err != nil {
			return nil, err
		}
		resp, err = p.http.do(ctx, http.MethodPost, endpoint, headers, body)
		if err == nil {
			break
		}
		if attempt == 0 && body.GenerationConfig != nil && body.GenerationConfig.ResponseSchema != nil &&
			isRejection(err, "response_schema", "responseschema", "response_mime_type", "responsemimetype") {
			opts.jsonFallback = true
			continue
		}
		return nil, err
	}

	acc := gemAccumulator{onDelta: onDelta}
	if stream {
		if err := p.readStream(ctx, resp, &acc); err != nil {
			return nil, err
		}
	} else {
		var gr gemResponse
		if err := p.http.decodeJSON(ctx, resp, &gr); err != nil {
			return nil, err
		}
		if err := acc.add(gr); err != nil {
			return nil, p.http.errorf(0, true, "%v", err)
		}
	}
	if !acc.complete() {
		return nil, p.http.errorf(0, true, "response ended without a finish reason")
	}
	return acc.response(req), nil
}

func (p *Gemini) readStream(ctx context.Context, resp *http.Response, acc *gemAccumulator) error {
	defer resp.Body.Close()
	sse := newSSEReader(resp.Body)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: %w", p.id, err)
		}
		ev, err := sse.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return p.http.readError(ctx, err)
		}
		data := strings.TrimSpace(ev.Data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var gr gemResponse
		if err := json.Unmarshal([]byte(data), &gr); err != nil {
			return p.http.errorf(0, true, "malformed stream chunk: %v", err)
		}
		if err := acc.add(gr); err != nil {
			return p.http.errorf(0, true, "%v", err)
		}
	}
}

// gemAccumulator assembles a response from one generateContent result or a
// sequence of streamed chunks, so both paths produce identical Responses.
type gemAccumulator struct {
	onDelta      models.StreamHandler
	text         strings.Builder
	calls        []models.ToolCall
	parts        []json.RawMessage // model parts for replay (adjacent plain text merged)
	hasSignature bool
	finish       string
	blockReason  string
	usage        *gemUsage
}

func (a *gemAccumulator) add(gr gemResponse) error {
	if msg := embeddedErrorMessage(gr.Error); msg != "" {
		return fmt.Errorf("stream error: %s", msg)
	}
	if gr.UsageMetadata != nil {
		a.usage = gr.UsageMetadata
	}
	if gr.PromptFeedback != nil && gr.PromptFeedback.BlockReason != "" {
		a.blockReason = gr.PromptFeedback.BlockReason
	}
	if len(gr.Candidates) == 0 {
		return nil
	}
	c := gr.Candidates[0]
	if c.FinishReason != "" {
		a.finish = c.FinishReason
	}
	if c.Content == nil {
		return nil
	}
	for _, raw := range c.Content.Parts {
		var part gemPart
		if err := json.Unmarshal(raw, &part); err != nil {
			return fmt.Errorf("malformed content part: %w", err)
		}
		a.appendReplayPart(raw)
		if part.ThoughtSignature != "" {
			a.hasSignature = true
		}
		if part.Thought {
			continue // hidden reasoning is never surfaced
		}
		if part.Text != "" {
			a.text.WriteString(part.Text)
			if a.onDelta != nil {
				a.onDelta(part.Text)
			}
		}
		if fc := part.FunctionCall; fc != nil {
			args := objectArgs(fc.Args)
			id := fc.ID
			if id == "" {
				id = geminiCallID(len(a.calls), fc.Name, args)
			}
			a.calls = append(a.calls, models.ToolCall{ID: id, Name: fc.Name, Arguments: args})
		}
	}
	return nil
}

// geminiCallID synthesizes a deterministic id for a function call that has none.
func geminiCallID(n int, name string, args []byte) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write(args)
	return fmt.Sprintf("call_%d_%08x", n, h.Sum32())
}

// appendReplayPart records raw for replay, merging adjacent parts that are
// plain text only (no signature / thought flag), as streams split text into
// many chunks.
func (a *gemAccumulator) appendReplayPart(raw json.RawMessage) {
	text, plain := plainTextPart(raw)
	if plain && len(a.parts) > 0 {
		if prev, ok := plainTextPart(a.parts[len(a.parts)-1]); ok {
			merged, _ := json.Marshal(map[string]string{"text": prev + text})
			a.parts[len(a.parts)-1] = merged
			return
		}
	}
	a.parts = append(a.parts, append(json.RawMessage(nil), raw...))
}

// plainTextPart reports whether raw is exactly {"text": "..."}.
func plainTextPart(raw json.RawMessage) (string, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || len(m) != 1 {
		return "", false
	}
	t, ok := m["text"]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(t, &s) != nil {
		return "", false
	}
	return s, true
}

func (a *gemAccumulator) complete() bool {
	return a.finish != "" || a.blockReason != ""
}

func (a *gemAccumulator) response(req models.Request) *models.Response {
	out := &models.Response{Model: req.Model, Content: a.text.String(), ToolCalls: a.calls}
	switch a.finish {
	case "MAX_TOKENS":
		out.FinishReason = finishLength
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "LANGUAGE":
		out.FinishReason = finishRefusal
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
		out.FinishReason = finishError
	default: // STOP, FINISH_REASON_UNSPECIFIED, OTHER
		out.FinishReason = finishStop
		if len(out.ToolCalls) > 0 {
			out.FinishReason = finishToolCalls
		}
	}
	if a.blockReason != "" {
		out.FinishReason = finishRefusal
	}
	switch out.FinishReason {
	case finishRefusal:
		reason := a.blockReason
		if reason == "" {
			reason = a.finish
		}
		out.Content = fmt.Sprintf("%s (reason: %s)", refusalMessage, reason)
		out.ToolCalls = nil
	case finishError:
		if out.Content == "" {
			out.Content = "The model produced an invalid function call (" + a.finish + ")."
		}
		out.ToolCalls = nil
	case finishStop:
		if len(req.JSONSchema) > 0 {
			out.Content = extractJSON(out.Content)
		}
	}

	if a.hasSignature && out.FinishReason != finishRefusal && len(a.parts) > 0 {
		var buf bytes.Buffer
		buf.WriteString(`{"role":"model","parts":[`)
		for i, part := range a.parts {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(part)
		}
		buf.WriteString(`]}`)
		out.Replay = &models.ReplayState{Provider: GeminiReplayProvider, Model: req.Model, Content: buf.Bytes()}
	}

	if a.usage != nil && (a.usage.PromptTokenCount > 0 || a.usage.CandidatesTokenCount > 0 || a.usage.ThoughtsTokenCount > 0) {
		out.Usage = models.Usage{
			InputTokens:  a.usage.PromptTokenCount,
			OutputTokens: a.usage.CandidatesTokenCount + a.usage.ThoughtsTokenCount,
		}
	} else {
		out.Usage = estimateUsage(req, out)
	}
	return out
}
