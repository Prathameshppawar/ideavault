package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// AnthropicReplayProvider tags models.ReplayState produced by the Anthropic provider.
const AnthropicReplayProvider = "anthropic"

const (
	anthropicDefaultMaxTokens       = 16000
	anthropicDefaultStreamMaxTokens = 32000
	// The SDK refuses non-streaming requests whose max_tokens implies more than
	// ~10 minutes of generation (≈21,333 tokens); larger Complete calls are
	// transparently served over the streaming endpoint.
	anthropicMaxNonStreamingTokens = 21000
)

// AnthropicConfig configures the Anthropic (Claude) provider.
type AnthropicConfig struct {
	// ID is the registry provider key (default "anthropic").
	ID string
	// APIKey is the Anthropic API key. Without it every call returns
	// models.ErrNotConfigured.
	APIKey string
	// BaseURL overrides the API root (tests, proxies). Empty uses the SDK default.
	BaseURL string
	// HTTPClient overrides the HTTP client.
	HTTPClient *http.Client
	// Effort optionally sets output_config.effort ("low", "medium", "high",
	// "xhigh", "max"). Empty leaves the model default. On Claude Opus 5.5 /
	// Sonnet 5.5 / Haiku 5.5 thinking cannot be disabled; effort is the control.
	Effort string
	// ExtraHeaders are added to every request.
	ExtraHeaders map[string]string
}

// Anthropic is a models.Provider backed by the official Anthropic Go SDK.
//
// Requests never send a thinking config (current models think adaptively and
// reject disabling it), never force tool_choice (only "auto"), and never put
// thinking text into Response.Content. When a response contains thinking
// blocks, its full assistant content is returned in Response.Replay and is
// replayed verbatim on the next request for the same model.
type Anthropic struct {
	id         string
	apiKey     string
	effort     string
	configured bool
	client     anthropic.Client
}

var _ models.Provider = (*Anthropic)(nil)

// NewAnthropic returns an Anthropic provider. The SDK's own retries are
// disabled (IdeaVault's gateway owns retries and fallbacks) and environment
// variables are not consulted.
func NewAnthropic(cfg AnthropicConfig) *Anthropic {
	id := cfg.ID
	if id == "" {
		id = "anthropic"
	}
	opts := []option.RequestOption{
		option.WithoutEnvironmentDefaults(),
		option.WithMaxRetries(0),
		option.WithHTTPClient(httpClientOrDefault(cfg.HTTPClient)),
	}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	for k, v := range cfg.ExtraHeaders {
		opts = append(opts, option.WithHeader(k, v))
	}
	return &Anthropic{
		id:         id,
		apiKey:     cfg.APIKey,
		effort:     cfg.Effort,
		configured: cfg.APIKey != "",
		client:     anthropic.NewClient(opts...),
	}
}

// ID returns the registry provider key.
func (p *Anthropic) ID() string { return p.id }

// Complete performs a non-streaming Messages API call (MaxTokens defaults to
// 16000). Requests with very large MaxTokens use the streaming endpoint.
func (p *Anthropic) Complete(ctx context.Context, req models.Request) (*models.Response, error) {
	if !p.configured {
		return nil, fmt.Errorf("%s: %w", p.id, models.ErrNotConfigured)
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	if maxTokens > anthropicMaxNonStreamingTokens {
		return p.stream(ctx, req, maxTokens, nil)
	}
	return p.execute(ctx, req, maxTokens, func(params anthropic.MessageNewParams) (*anthropic.Message, error) {
		return p.client.Messages.New(ctx, params)
	})
}

// Stream performs a streaming Messages API call (MaxTokens defaults to
// 32000), calling onDelta for visible text deltas only.
func (p *Anthropic) Stream(ctx context.Context, req models.Request, onDelta models.StreamHandler) (*models.Response, error) {
	if !p.configured {
		return nil, fmt.Errorf("%s: %w", p.id, models.ErrNotConfigured)
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultStreamMaxTokens
	}
	return p.stream(ctx, req, maxTokens, onDelta)
}

func (p *Anthropic) stream(ctx context.Context, req models.Request, maxTokens int, onDelta models.StreamHandler) (*models.Response, error) {
	return p.execute(ctx, req, maxTokens, func(params anthropic.MessageNewParams) (*anthropic.Message, error) {
		return p.streamOnce(ctx, params, onDelta)
	})
}

type anthropicBuildOpts struct {
	jsonFallback  bool // structured output via instructions instead of output_config.format
	noTemperature bool
}

// execute builds params, calls send, retries once without optional features
// the model rejects, and converts the result.
func (p *Anthropic) execute(ctx context.Context, req models.Request, maxTokens int,
	send func(anthropic.MessageNewParams) (*anthropic.Message, error)) (*models.Response, error) {
	var opts anthropicBuildOpts
	for attempt := 0; ; attempt++ {
		params, err := p.buildParams(req, maxTokens, opts)
		if err != nil {
			return nil, err
		}
		msg, err := send(params)
		if err == nil {
			return p.toResponse(msg, req), nil
		}
		err = p.mapError(ctx, err)
		if attempt < 2 && !opts.jsonFallback && len(req.JSONSchema) > 0 &&
			isRejection(err, "output_config", "output_format", "output format", "structured output", "json_schema", "json schema") {
			opts.jsonFallback = true
			continue
		}
		if attempt < 2 && !opts.noTemperature && params.Temperature.Valid() && isRejection(err, "temperature") {
			opts.noTemperature = true
			continue
		}
		return nil, err
	}
}

// streamOnce runs one streaming request and returns the accumulated message.
func (p *Anthropic) streamOnce(ctx context.Context, params anthropic.MessageNewParams, onDelta models.StreamHandler) (*anthropic.Message, error) {
	stream := p.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var msg anthropic.Message
	stopped := false
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return nil, &models.ProviderError{Provider: p.id, Retryable: true, Message: redact("malformed stream: "+err.Error(), p.apiKey)}
		}
		switch ev.Type {
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" && onDelta != nil {
				onDelta(ev.Delta.Text)
			}
		case "message_stop":
			stopped = true
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	if !stopped {
		return nil, &models.ProviderError{Provider: p.id, Retryable: true, Message: "stream ended before message_stop"}
	}
	return &msg, nil
}

// ---------------------------------------------------------------------------
// Request building

// legacySamplingModel matches models that still accept temperature. Current
// models (Opus 4.7+, Sonnet 5+, Haiku 5.5, Fable/Mythos) reject sampling
// parameters with a 400, so temperature is only forwarded to these.
var legacySamplingModel = regexp.MustCompile(`^claude-(3|(opus|sonnet|haiku)-4-(0|1|5|6)\b|(opus|sonnet)-4-20)`)

func (p *Anthropic) buildParams(req models.Request, maxTokens int, o anthropicBuildOpts) (anthropic.MessageNewParams, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
	}

	var system []string
	if req.System != "" {
		system = append(system, req.System)
	}

	var b anthropicTurnBuilder
	for i, m := range req.Messages {
		switch m.Role {
		case models.RoleSystem:
			if m.Content != "" {
				system = append(system, m.Content)
			}
		case models.RoleUser:
			if strings.TrimSpace(m.Content) != "" {
				b.add(anthropic.MessageParamRoleUser, nil, anthropic.NewTextBlock(m.Content))
			}
		case models.RoleAssistant:
			if blocks, ok := anthropicReplayBlocks(m.Replay, req.Model); ok {
				b.add(anthropic.MessageParamRoleAssistant, nil, blocks...)
				continue
			}
			var blocks []anthropic.ContentBlockParamUnion
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, objectArgs(tc.Arguments), tc.Name))
			}
			b.add(anthropic.MessageParamRoleAssistant, nil, blocks...)
		case models.RoleTool:
			tr := anthropic.ToolResultBlockParam{ToolUseID: m.ToolCallID}
			if m.Content != "" {
				tr.Content = []anthropic.ToolResultBlockParamContentUnion{{OfText: &anthropic.TextBlockParam{Text: m.Content}}}
			}
			// Consecutive tool results accumulate into ONE user message,
			// ahead of any user text in the same turn.
			b.add(anthropic.MessageParamRoleUser, []anthropic.ContentBlockParamUnion{{OfToolResult: &tr}})
		default:
			return params, &models.ProviderError{Provider: p.id, Message: fmt.Sprintf("invalid request: message %d has unsupported role %q", i, m.Role)}
		}
	}
	params.Messages = b.finish()

	for _, t := range req.Tools {
		schema, err := anthropicToolSchema(t.Parameters)
		if err != nil {
			return params, &models.ProviderError{Provider: p.id, Message: fmt.Sprintf("invalid request: tool %q parameters: %v", t.Name, err)}
		}
		tp := anthropic.ToolParam{Name: t.Name, InputSchema: schema}
		if t.Description != "" {
			tp.Description = anthropic.String(t.Description)
		}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &tp})
	}
	if len(params.Tools) > 0 {
		// Forced tool use ("any"/"tool") returns 400 on current models.
		params.ToolChoice = anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}
	}

	if len(req.JSONSchema) > 0 {
		if o.jsonFallback {
			system = append(system, jsonInstruction(req.JSONSchema))
		} else {
			var raw map[string]any
			if err := json.Unmarshal(req.JSONSchema, &raw); err != nil {
				return params, &models.ProviderError{Provider: p.id, Message: fmt.Sprintf("invalid request: JSON schema: %v", err)}
			}
			// The SDK helper normalizes the schema for the API (additionalProperties:
			// false on objects, unsupported constraints moved into descriptions).
			schema := raw
			if m, ok := anthropic.BetaJSONSchemaOutputFormat(raw).Schema.(map[string]any); ok && m != nil {
				schema = m
			}
			params.OutputConfig.Format = anthropic.JSONOutputFormatParam{Schema: schema}
		}
	}
	if p.effort != "" {
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(p.effort)
	}

	if len(system) > 0 {
		params.System = []anthropic.TextBlockParam{{Text: strings.Join(system, "\n\n")}}
	}
	if req.Temperature != nil && !o.noTemperature && legacySamplingModel.MatchString(req.Model) {
		params.Temperature = anthropic.Float(*req.Temperature)
	}
	return params, nil
}

// anthropicTurnBuilder merges consecutive same-role content into single
// messages, placing tool_result blocks before other blocks in a user turn.
type anthropicTurnBuilder struct {
	msgs    []anthropic.MessageParam
	role    anthropic.MessageParamRole
	results []anthropic.ContentBlockParamUnion
	blocks  []anthropic.ContentBlockParamUnion
}

func (b *anthropicTurnBuilder) add(role anthropic.MessageParamRole, results []anthropic.ContentBlockParamUnion, blocks ...anthropic.ContentBlockParamUnion) {
	if len(results) == 0 && len(blocks) == 0 {
		return
	}
	if role != b.role {
		b.flush()
		b.role = role
	}
	b.results = append(b.results, results...)
	b.blocks = append(b.blocks, blocks...)
}

func (b *anthropicTurnBuilder) flush() {
	if len(b.results)+len(b.blocks) == 0 {
		return
	}
	content := make([]anthropic.ContentBlockParamUnion, 0, len(b.results)+len(b.blocks))
	content = append(content, b.results...)
	content = append(content, b.blocks...)
	b.msgs = append(b.msgs, anthropic.MessageParam{Role: b.role, Content: content})
	b.results, b.blocks = nil, nil
}

func (b *anthropicTurnBuilder) finish() []anthropic.MessageParam {
	b.flush()
	return b.msgs
}

// anthropicReplayBlocks returns the verbatim content blocks stored in r when
// it was produced by Anthropic for the same model.
func anthropicReplayBlocks(r *models.ReplayState, model string) ([]anthropic.ContentBlockParamUnion, bool) {
	if r == nil || r.Provider != AnthropicReplayProvider || r.Model != model || len(r.Content) == 0 {
		return nil, false
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(r.Content, &raws); err != nil || len(raws) == 0 {
		return nil, false
	}
	blocks := make([]anthropic.ContentBlockParamUnion, len(raws))
	for i, raw := range raws {
		blocks[i] = param.Override[anthropic.ContentBlockParamUnion](raw)
	}
	return blocks, true
}

// anthropicToolSchema converts a JSON Schema object into the SDK's input schema.
func anthropicToolSchema(raw json.RawMessage) (anthropic.ToolInputSchemaParam, error) {
	var s anthropic.ToolInputSchemaParam
	if len(bytes.TrimSpace(raw)) == 0 {
		s.Properties = map[string]any{}
		return s, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return s, err
	}
	if props, ok := m["properties"]; ok {
		s.Properties = props
	} else {
		s.Properties = map[string]any{}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				s.Required = append(s.Required, name)
			}
		}
	}
	delete(m, "type")
	delete(m, "properties")
	delete(m, "required")
	delete(m, "$schema")
	if len(m) > 0 {
		s.ExtraFields = m
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Response conversion

func (p *Anthropic) toResponse(msg *anthropic.Message, req models.Request) *models.Response {
	out := &models.Response{Model: req.Model}
	var text strings.Builder
	hasThinking := false
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, models.ToolCall{ID: block.ID, Name: block.Name, Arguments: normalizeArgs(block.Input)})
		case "thinking", "redacted_thinking":
			// Never surfaced; only replayed.
			hasThinking = true
		}
	}
	out.Content = text.String()

	switch msg.StopReason {
	case anthropic.StopReasonToolUse:
		out.FinishReason = finishToolCalls
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		out.FinishReason = finishLength
	case anthropic.StopReasonRefusal:
		out.FinishReason = finishRefusal
	default: // end_turn, stop_sequence, pause_turn
		out.FinishReason = finishStop
		if len(out.ToolCalls) > 0 {
			out.FinishReason = finishToolCalls
		}
	}

	if out.FinishReason == finishRefusal {
		// A refusal can cut content (and tool input) off mid-way: never surface
		// partial output or execute that turn's tools.
		out.Content = refusalMessage
		if exp := strings.TrimSpace(msg.StopDetails.Explanation); exp != "" {
			out.Content += " " + exp
		}
		out.ToolCalls = nil
	} else if len(req.JSONSchema) > 0 && out.FinishReason == finishStop {
		out.Content = extractJSON(out.Content)
	}

	if hasThinking && out.FinishReason != finishRefusal {
		if raw, err := json.Marshal(msg.ToParam().Content); err == nil {
			out.Replay = &models.ReplayState{Provider: AnthropicReplayProvider, Model: req.Model, Content: raw}
		}
	}

	u := msg.Usage
	in := int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens)
	if in > 0 || u.OutputTokens > 0 {
		out.Usage = models.Usage{InputTokens: in, OutputTokens: int(u.OutputTokens)}
	} else {
		out.Usage = estimateUsage(req, out)
	}
	return out
}

// mapError converts SDK errors into *models.ProviderError (or a wrapped
// context error when ctx is done).
func (p *Anthropic) mapError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", p.id, ctxErr)
	}
	var pe *models.ProviderError
	if errors.As(err, &pe) {
		return err
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		typ := string(apiErr.Type())
		if status < 400 {
			// Errors delivered as an SSE "error" event carry the stream's 200.
			status = anthropicStatusForType(typ)
		}
		msg := errorMessageFromBody([]byte(apiErr.RawJSON()))
		if msg == "" {
			msg = http.StatusText(status)
		}
		if typ != "" && !strings.HasPrefix(msg, typ) {
			msg = typ + ": " + msg
		}
		retryable := retryableStatus(status)
		switch typ {
		case "overloaded_error", "rate_limit_error", "api_error", "timeout_error":
			retryable = true
		}
		return &models.ProviderError{Provider: p.id, StatusCode: status, Retryable: retryable, Message: redact(msg, p.apiKey)}
	}
	return transportError(ctx, p.id, err, p.apiKey)
}

func anthropicStatusForType(typ string) int {
	switch typ {
	case "invalid_request_error":
		return http.StatusBadRequest
	case "authentication_error":
		return http.StatusUnauthorized
	case "permission_error":
		return http.StatusForbidden
	case "not_found_error":
		return http.StatusNotFound
	case "request_too_large":
		return http.StatusRequestEntityTooLarge
	case "rate_limit_error":
		return http.StatusTooManyRequests
	case "timeout_error":
		return http.StatusGatewayTimeout
	case "overloaded_error":
		return 529
	default:
		return http.StatusInternalServerError
	}
}
