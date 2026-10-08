package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// Default base URLs for OpenAI-compatible chat/embedding endpoints.
const (
	DefaultOpenAIBaseURL       = "https://api.openai.com/v1"
	DefaultGroqBaseURL         = "https://api.groq.com/openai/v1"
	DefaultOllamaBaseURL       = "http://localhost:11434/v1"
	DefaultLMStudioBaseURL     = "http://localhost:1234/v1"
	DefaultGeminiOpenAIBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
)

// defaultOpenAICompatBaseURL returns the well-known base URL for id, or "".
func defaultOpenAICompatBaseURL(id string) string {
	switch id {
	case "openai":
		return DefaultOpenAIBaseURL
	case "groq":
		return DefaultGroqBaseURL
	case "ollama":
		return DefaultOllamaBaseURL
	case "lmstudio":
		return DefaultLMStudioBaseURL
	case "gemini-openai":
		return DefaultGeminiOpenAIBaseURL
	}
	return ""
}

// OpenAICompatConfig configures an OpenAI-compatible chat provider (OpenAI,
// Groq, Ollama, LM Studio, or any server implementing /chat/completions).
type OpenAICompatConfig struct {
	// ID is the registry provider key, e.g. "openai", "groq", "ollama",
	// "lmstudio", "gemini-openai". It selects defaults (base URL, token
	// parameter name, stream usage) and whether an API key is required.
	ID string
	// BaseURL is the API root including the version segment (e.g.
	// "https://api.openai.com/v1"). Empty selects the default for ID.
	BaseURL string
	// APIKey is sent as "Authorization: Bearer <key>" when non-empty. Hosted
	// IDs (openai, groq, gemini-openai) return models.ErrNotConfigured without one.
	APIKey string
	// HTTPClient overrides the HTTP client. Nil uses a client without a global
	// timeout; request lifetime is governed by the context.
	HTTPClient *http.Client
	// ExtraHeaders are added to every request (e.g. "OpenAI-Organization").
	ExtraHeaders map[string]string
	// StreamUsage controls sending stream_options.include_usage on streaming
	// requests. Nil defaults to true for "openai" and "groq", false otherwise
	// (some local servers reject the field).
	StreamUsage *bool
	// MaxCompletionTokens selects "max_completion_tokens" instead of
	// "max_tokens". Nil defaults to true for "openai" and "groq".
	MaxCompletionTokens *bool
	// ReasoningParams returns extra top-level request fields for a model,
	// used to keep reasoning models from emitting chain-of-thought. Nil
	// defaults to GroqReasoningParams for ID "groq" and to none otherwise.
	// Returned keys never override the standard request fields.
	ReasoningParams func(model string) map[string]any
}

// GroqReasoningParams suppresses reasoning output on Groq reasoning models:
// "include_reasoning": false for openai/gpt-oss-* and "reasoning_format":
// "hidden" for qwen/* models. Other models get no extra fields.
func GroqReasoningParams(model string) map[string]any {
	switch {
	case strings.HasPrefix(model, "openai/gpt-oss"):
		return map[string]any{"include_reasoning": false}
	case strings.HasPrefix(model, "qwen/"):
		return map[string]any{"reasoning_format": "hidden"}
	}
	return nil
}

// OpenAICompatible is a models.Provider for OpenAI-compatible chat APIs.
// It is safe for concurrent use.
type OpenAICompatible struct {
	id               string
	baseURL          string
	apiKey           string
	headers          map[string]string
	http             httpCaller
	streamUsage      bool
	maxCompletionTok bool
	requireKey       bool
	reasoningParams  func(model string) map[string]any
}

var _ models.Provider = (*OpenAICompatible)(nil)

// NewOpenAICompatible returns a provider for an OpenAI-compatible API.
func NewOpenAICompatible(cfg OpenAICompatConfig) *OpenAICompatible {
	id := cfg.ID
	if id == "" {
		id = "openai"
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultOpenAICompatBaseURL(id)
	}
	streamUsage := id == "openai" || id == "groq"
	if cfg.StreamUsage != nil {
		streamUsage = *cfg.StreamUsage
	}
	maxCompletion := id == "openai" || id == "groq"
	if cfg.MaxCompletionTokens != nil {
		maxCompletion = *cfg.MaxCompletionTokens
	}
	reasoning := cfg.ReasoningParams
	if reasoning == nil && id == "groq" {
		reasoning = GroqReasoningParams
	}
	headers := map[string]string{}
	for k, v := range cfg.ExtraHeaders {
		headers[k] = v
	}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	return &OpenAICompatible{
		id:               id,
		baseURL:          strings.TrimRight(base, "/"),
		apiKey:           cfg.APIKey,
		headers:          headers,
		http:             httpCaller{client: httpClientOrDefault(cfg.HTTPClient), provider: id, secrets: []string{cfg.APIKey}},
		streamUsage:      streamUsage,
		maxCompletionTok: maxCompletion,
		requireKey:       hostedProviderIDs[id],
		reasoningParams:  reasoning,
	}
}

// ID returns the registry provider key.
func (p *OpenAICompatible) ID() string { return p.id }

// Complete performs a non-streaming chat completion.
func (p *OpenAICompatible) Complete(ctx context.Context, req models.Request) (*models.Response, error) {
	return p.run(ctx, req, false, nil)
}

// Stream performs a streaming chat completion, calling onDelta with visible text.
func (p *OpenAICompatible) Stream(ctx context.Context, req models.Request, onDelta models.StreamHandler) (*models.Response, error) {
	return p.run(ctx, req, true, onDelta)
}

// ListModels returns the model IDs served at GET {base}/models, sorted.
func (p *OpenAICompatible) ListModels(ctx context.Context) ([]string, error) {
	if err := p.checkConfigured(); err != nil {
		return nil, err
	}
	resp, err := p.http.do(ctx, http.MethodGet, p.baseURL+"/models", p.requestHeaders(false), nil)
	if err != nil {
		return nil, err
	}
	var lr struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := p.http.decodeJSON(ctx, resp, &lr); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(lr.Data))
	for _, m := range lr.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return slices.Compact(ids), nil
}

func (p *OpenAICompatible) checkConfigured() error {
	if p.requireKey && p.apiKey == "" {
		return fmt.Errorf("%s: %w", p.id, models.ErrNotConfigured)
	}
	if p.baseURL == "" {
		return fmt.Errorf("%s: base URL: %w", p.id, models.ErrNotConfigured)
	}
	return nil
}

func (p *OpenAICompatible) requestHeaders(stream bool) map[string]string {
	h := make(map[string]string, len(p.headers)+1)
	for k, v := range p.headers {
		h[k] = v
	}
	if stream {
		h["Accept"] = "text/event-stream"
	} else {
		h["Accept"] = "application/json"
	}
	return h
}

// ---------------------------------------------------------------------------
// Wire types

type oaChatRequest struct {
	Model               string            `json:"model"`
	Messages            []oaMessage       `json:"messages"`
	Tools               []oaTool          `json:"tools,omitempty"`
	ToolChoice          string            `json:"tool_choice,omitempty"`
	Temperature         *float64          `json:"temperature,omitempty"`
	MaxTokens           int               `json:"max_tokens,omitempty"`
	MaxCompletionTokens int               `json:"max_completion_tokens,omitempty"`
	ResponseFormat      *oaResponseFormat `json:"response_format,omitempty"`
	Stream              bool              `json:"stream,omitempty"`
	StreamOptions       *oaStreamOptions  `json:"stream_options,omitempty"`
	// Extra holds additional top-level fields (e.g. reasoning controls). They
	// never override the fields above.
	Extra map[string]any `json:"-"`
}

// MarshalJSON encodes the request plus any Extra fields.
func (r oaChatRequest) MarshalJSON() ([]byte, error) {
	type plain oaChatRequest
	b, err := json.Marshal(plain(r))
	if err != nil || len(r.Extra) == 0 {
		return b, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range r.Extra {
		if _, exists := m[k]; exists {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("extra field %q: %w", k, err)
		}
		m[k] = raw
	}
	return json.Marshal(m)
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    *string      `json:"content,omitempty"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function oaFunctionCall `json:"function"`
}

type oaFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaTool struct {
	Type     string        `json:"type"`
	Function oaFunctionDef `json:"function"`
}

type oaFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaResponseFormat struct {
	Type       string        `json:"type"`
	JSONSchema *oaJSONSchema `json:"json_schema,omitempty"`
}

type oaJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type oaStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type oaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// oaRespToolCall tolerates arguments sent as a JSON string (standard) or as a
// raw object (some local servers), and a missing index.
type oaRespToolCall struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type oaChatResponse struct {
	Choices []struct {
		Message struct {
			Content   json.RawMessage  `json:"content"`
			Refusal   *string          `json:"refusal"`
			ToolCalls []oaRespToolCall `json:"tool_calls"`
			// reasoning / reasoning_content are intentionally not decoded.
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaUsage `json:"usage"`
	XGroq *struct {
		Usage *oaUsage `json:"usage"`
	} `json:"x_groq"`
	Error json.RawMessage `json:"error"`
}

type oaStreamChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content   json.RawMessage  `json:"content"`
			Refusal   *string          `json:"refusal"`
			ToolCalls []oaRespToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaUsage `json:"usage"`
	XGroq *struct {
		Usage *oaUsage `json:"usage"`
	} `json:"x_groq"`
	Error json.RawMessage `json:"error"`
}

// ---------------------------------------------------------------------------
// Request building

type oaBuildOpts struct {
	jsonObject    bool // fall back from json_schema to json_object + instruction
	noTemperature bool // drop temperature (rejected by some reasoning models)
	noReasoning   bool // drop ReasoningParams fields (rejected by the model)
}

func (p *OpenAICompatible) buildRequest(req models.Request, stream bool, o oaBuildOpts) (oaChatRequest, error) {
	body := oaChatRequest{Model: req.Model, Stream: stream}

	system := req.System
	if len(req.JSONSchema) > 0 && o.jsonObject {
		system = joinNonEmpty(system, jsonInstruction(req.JSONSchema))
	}
	if system != "" {
		body.Messages = append(body.Messages, oaMessage{Role: "system", Content: strPtr(system)})
	}
	for i, m := range req.Messages {
		switch m.Role {
		case models.RoleSystem:
			body.Messages = append(body.Messages, oaMessage{Role: "system", Content: strPtr(m.Content)})
		case models.RoleUser:
			body.Messages = append(body.Messages, oaMessage{Role: "user", Content: strPtr(m.Content)})
		case models.RoleAssistant:
			om := oaMessage{Role: "assistant"}
			if m.Content != "" || len(m.ToolCalls) == 0 {
				om.Content = strPtr(m.Content)
			}
			for _, tc := range m.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, oaToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: oaFunctionCall{Name: tc.Name, Arguments: string(normalizeArgs(tc.Arguments))},
				})
			}
			body.Messages = append(body.Messages, om)
		case models.RoleTool:
			body.Messages = append(body.Messages, oaMessage{Role: "tool", ToolCallID: m.ToolCallID, Content: strPtr(m.Content)})
		default:
			return body, p.http.errorf(0, false, "invalid request: message %d has unsupported role %q", i, m.Role)
		}
	}

	for _, t := range req.Tools {
		params := t.Parameters
		if len(bytes.TrimSpace(params)) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		body.Tools = append(body.Tools, oaTool{
			Type:     "function",
			Function: oaFunctionDef{Name: t.Name, Description: t.Description, Parameters: params},
		})
	}
	if len(body.Tools) > 0 {
		body.ToolChoice = "auto"
	}

	if req.Temperature != nil && !o.noTemperature {
		t := *req.Temperature
		body.Temperature = &t
	}
	if req.MaxTokens > 0 {
		if p.maxCompletionTok {
			body.MaxCompletionTokens = req.MaxTokens
		} else {
			body.MaxTokens = req.MaxTokens
		}
	}

	if len(req.JSONSchema) > 0 {
		if o.jsonObject {
			body.ResponseFormat = &oaResponseFormat{Type: "json_object"}
		} else {
			body.ResponseFormat = &oaResponseFormat{
				Type: "json_schema",
				JSONSchema: &oaJSONSchema{
					Name:   schemaName(req.SchemaName),
					Schema: req.JSONSchema,
					Strict: false,
				},
			}
		}
	}

	if stream && p.streamUsage {
		body.StreamOptions = &oaStreamOptions{IncludeUsage: true}
	}
	if p.reasoningParams != nil && !o.noReasoning {
		if extra := p.reasoningParams(req.Model); len(extra) > 0 {
			body.Extra = extra
		}
	}
	return body, nil
}

// extraKeys returns the lower-cased keys of m, sorted.
func extraKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	return keys
}

// schemaName sanitizes a structured-output name to ^[A-Za-z0-9_-]{1,64}$.
func schemaName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 64 {
			break
		}
	}
	if b.Len() == 0 {
		return "output"
	}
	return b.String()
}

func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Execution

func (p *OpenAICompatible) run(ctx context.Context, req models.Request, stream bool, onDelta models.StreamHandler) (*models.Response, error) {
	if err := p.checkConfigured(); err != nil {
		return nil, err
	}
	var (
		opts oaBuildOpts
		resp *http.Response
	)
	// Optional features a model rejects with a 400 are dropped one at a time
	// (bounded: each can be dropped once).
	for attempt := 0; ; attempt++ {
		body, err := p.buildRequest(req, stream, opts)
		if err != nil {
			return nil, err
		}
		resp, err = p.http.do(ctx, http.MethodPost, p.baseURL+"/chat/completions", p.requestHeaders(stream), body)
		if err == nil {
			break
		}
		switch {
		case attempt < 3 && !opts.jsonObject && body.ResponseFormat != nil && body.ResponseFormat.Type == "json_schema" &&
			isRejection(err, "response_format", "json_schema", "json schema", "response format"):
			opts.jsonObject = true
		case attempt < 3 && !opts.noReasoning && len(body.Extra) > 0 && isRejection(err, extraKeys(body.Extra)...):
			// Reasoning stays hidden: reasoning fields are ignored and inline
			// <think> blocks are stripped regardless.
			opts.noReasoning = true
		case attempt < 3 && !opts.noTemperature && body.Temperature != nil && isRejection(err, "temperature"):
			opts.noTemperature = true
		default:
			return nil, err
		}
	}

	var acc oaAccumulator
	if stream {
		if err := p.readStream(ctx, resp, &acc, onDelta); err != nil {
			return nil, err
		}
	} else {
		if err := p.readCompletion(ctx, resp, &acc); err != nil {
			return nil, err
		}
	}
	return acc.response(req), nil
}

// oaAccumulator collects the final message from either a full completion or a
// stream so both paths produce identical Responses.
type oaAccumulator struct {
	visible  strings.Builder // content with any <think> block removed
	strip    thinkStripper
	refusal  strings.Builder
	calls    []*oaPendingCall
	finish   string
	usage    *oaUsage
	finished bool
}

type oaPendingCall struct {
	index int
	id    string
	name  string
	args  strings.Builder
	raw   json.RawMessage // set when arguments arrived as a JSON object
}

func (a *oaAccumulator) addContent(text string, onDelta models.StreamHandler) {
	if text == "" {
		return
	}
	if vis := a.strip.Write(text); vis != "" {
		a.visible.WriteString(vis)
		if onDelta != nil {
			onDelta(vis)
		}
	}
}

func (a *oaAccumulator) flush(onDelta models.StreamHandler) {
	if vis := a.strip.Flush(); vis != "" {
		a.visible.WriteString(vis)
		if onDelta != nil {
			onDelta(vis)
		}
	}
}

func (a *oaAccumulator) addToolCall(tc oaRespToolCall) {
	var call *oaPendingCall
	switch {
	case tc.Index != nil:
		for _, c := range a.calls {
			if c.index == *tc.Index {
				call = c
				break
			}
		}
		if call == nil {
			call = &oaPendingCall{index: *tc.Index}
			a.calls = append(a.calls, call)
		}
	default:
		// No index (some servers): match by id, else continue the last call.
		if tc.ID != "" {
			for _, c := range a.calls {
				if c.id == tc.ID {
					call = c
					break
				}
			}
			if call == nil {
				call = &oaPendingCall{index: len(a.calls)}
				a.calls = append(a.calls, call)
			}
		} else if n := len(a.calls); n > 0 {
			call = a.calls[n-1]
		} else {
			call = &oaPendingCall{}
			a.calls = append(a.calls, call)
		}
	}
	if tc.ID != "" {
		call.id = tc.ID
	}
	if tc.Function.Name != "" {
		call.name = tc.Function.Name
	}
	if args := bytes.TrimSpace(tc.Function.Arguments); len(args) > 0 {
		if args[0] == '"' {
			var frag string
			if json.Unmarshal(args, &frag) == nil {
				call.args.WriteString(frag)
			}
		} else if !bytes.Equal(args, []byte("null")) {
			call.raw = append(json.RawMessage(nil), args...)
		}
	}
}

func (a *oaAccumulator) response(req models.Request) *models.Response {
	out := &models.Response{Model: req.Model, Content: a.visible.String()}
	sort.SliceStable(a.calls, func(i, j int) bool { return a.calls[i].index < a.calls[j].index })
	for i, c := range a.calls {
		args := c.raw
		if args == nil {
			args = normalizeArgs([]byte(c.args.String()))
		}
		id := c.id
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		out.ToolCalls = append(out.ToolCalls, models.ToolCall{ID: id, Name: c.name, Arguments: args})
	}

	switch a.finish {
	case "length":
		out.FinishReason = finishLength
	case "content_filter":
		out.FinishReason = finishRefusal
	case "tool_calls", "function_call":
		out.FinishReason = finishToolCalls
	default:
		out.FinishReason = finishStop
	}
	if out.FinishReason == finishStop && len(out.ToolCalls) > 0 {
		out.FinishReason = finishToolCalls
	}
	if a.refusal.Len() > 0 {
		out.FinishReason = finishRefusal
	}
	if out.FinishReason == finishRefusal {
		msg := strings.TrimSpace(a.refusal.String())
		if msg == "" {
			msg = refusalMessage
		}
		out.Content = msg
		out.ToolCalls = nil
	}
	if len(req.JSONSchema) > 0 && out.FinishReason == finishStop {
		out.Content = extractJSON(out.Content)
	}

	if a.usage != nil && (a.usage.PromptTokens > 0 || a.usage.CompletionTokens > 0) {
		out.Usage = models.Usage{InputTokens: a.usage.PromptTokens, OutputTokens: a.usage.CompletionTokens}
	} else {
		out.Usage = estimateUsage(req, out)
	}
	return out
}

func (p *OpenAICompatible) readCompletion(ctx context.Context, resp *http.Response, acc *oaAccumulator) error {
	var cr oaChatResponse
	if err := p.http.decodeJSON(ctx, resp, &cr); err != nil {
		return err
	}
	if msg := embeddedErrorMessage(cr.Error); msg != "" {
		return p.http.errorf(resp.StatusCode, true, "%s", msg)
	}
	if len(cr.Choices) == 0 {
		return p.http.errorf(resp.StatusCode, true, "response contained no choices")
	}
	ch := cr.Choices[0]
	acc.addContent(oaContentText(ch.Message.Content), nil)
	acc.flush(nil)
	if ch.Message.Refusal != nil {
		acc.refusal.WriteString(*ch.Message.Refusal)
	}
	for i, tc := range ch.Message.ToolCalls {
		if tc.Index == nil {
			idx := i
			tc.Index = &idx
		}
		acc.addToolCall(tc)
	}
	acc.finish = ch.FinishReason
	acc.usage = cr.Usage
	if acc.usage == nil && cr.XGroq != nil {
		acc.usage = cr.XGroq.Usage
	}
	acc.finished = true
	return nil
}

func (p *OpenAICompatible) readStream(ctx context.Context, resp *http.Response, acc *oaAccumulator, onDelta models.StreamHandler) error {
	defer resp.Body.Close()
	sse := newSSEReader(resp.Body)
	done := false
	for !done {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: %w", p.id, err)
		}
		ev, err := sse.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return p.http.readError(ctx, err)
		}
		data := strings.TrimSpace(ev.Data)
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk oaStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return p.http.errorf(0, true, "malformed stream chunk: %v", err)
		}
		if msg := embeddedErrorMessage(chunk.Error); msg != "" {
			return p.http.errorf(0, true, "stream error: %s", msg)
		}
		if chunk.Usage != nil {
			acc.usage = chunk.Usage
		} else if chunk.XGroq != nil && chunk.XGroq.Usage != nil {
			acc.usage = chunk.XGroq.Usage
		}
		for _, ch := range chunk.Choices {
			if ch.Index != 0 {
				continue
			}
			acc.addContent(oaContentText(ch.Delta.Content), onDelta)
			if ch.Delta.Refusal != nil {
				acc.refusal.WriteString(*ch.Delta.Refusal)
			}
			for _, tc := range ch.Delta.ToolCalls {
				acc.addToolCall(tc)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				acc.finish = *ch.FinishReason
				acc.finished = true
			}
		}
	}
	acc.flush(onDelta)
	if !done && !acc.finished {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: %w", p.id, err)
		}
		return p.http.errorf(0, true, "stream ended before completion")
	}
	return nil
}

// oaContentText decodes message content that is either a string or an array
// of content parts ({"type":"text","text":...}). Non-text parts are ignored.
func oaContentText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, part := range parts {
			if part.Type == "text" || part.Type == "output_text" {
				b.WriteString(part.Text)
			}
		}
		return b.String()
	}
	return ""
}
