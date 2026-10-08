package models

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// UsageRecorder persists usage events (implemented by the postgres store).
type UsageRecorder interface {
	InsertUsage(ctx context.Context, userID *uuid.UUID, u *domain.UsageEvent) error
}

// ProviderCredentials configures one provider instance.
type ProviderCredentials struct {
	APIKey  string
	BaseURL string
}

// ProviderFactory builds a Provider for an id from credentials (wired in main to avoid import cycles).
type ProviderFactory func(providerID string, creds ProviderCredentials) (Provider, error)

// Gateway is the single entry point for model calls: routing, fallbacks, usage and cost accounting.
type Gateway struct {
	mu        sync.RWMutex
	providers map[string]Provider
	creds     map[string]ProviderCredentials
	configs   []domain.ModelConfig
	policy    RoutingPolicy
	embedder  Embedder
	factory   ProviderFactory
	usage     UsageRecorder
	log       *slog.Logger
	mockOnly  bool
	cache     VectorCache
}

// VectorCache stores query embeddings (Redis or in-memory); satisfied by service.Cache.
type VectorCache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration)
}

// queryEmbeddingTTL bounds how long a cached query vector is reused.
const queryEmbeddingTTL = 6 * time.Hour

// NewGateway creates a gateway.
func NewGateway(factory ProviderFactory, usage UsageRecorder, log *slog.Logger) *Gateway {
	return &Gateway{providers: map[string]Provider{}, creds: map[string]ProviderCredentials{}, factory: factory, usage: usage, log: log}
}

// SetMockOnly restricts routing to the offline planner (tests / MOCK_AI).
func (g *Gateway) SetMockOnly(v bool) { g.mu.Lock(); g.mockOnly = v; g.mu.Unlock() }

// RegisterProvider installs an already-built provider (used for mock/offline and tests).
func (g *Gateway) RegisterProvider(p Provider) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.providers[p.ID()] = p
}

// ConfigureProvider (re)builds a provider from credentials. Empty key removes it (except keyless local providers).
func (g *Gateway) ConfigureProvider(id string, creds ProviderCredentials) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if creds.APIKey == "" && !(id == ProviderOllama && creds.BaseURL != "") {
		delete(g.providers, id)
		delete(g.creds, id)
		return nil
	}
	if g.factory == nil {
		return errors.New("no provider factory configured")
	}
	p, err := g.factory(id, creds)
	if err != nil {
		return err
	}
	g.providers[id] = p
	g.creds[id] = creds
	return nil
}

// SetModels replaces the model registry snapshot.
func (g *Gateway) SetModels(ms []domain.ModelConfig) { g.mu.Lock(); g.configs = ms; g.mu.Unlock() }

// SetPolicy replaces routing preferences.
func (g *Gateway) SetPolicy(p RoutingPolicy) { g.mu.Lock(); g.policy = p; g.mu.Unlock() }

// Policy returns current routing preferences.
func (g *Gateway) Policy() RoutingPolicy { g.mu.RLock(); defer g.mu.RUnlock(); return g.policy }

// SetEmbedder installs the embedding backend.
func (g *Gateway) SetEmbedder(e Embedder) { g.mu.Lock(); g.embedder = e; g.mu.Unlock() }

// SetCache enables caching of query embeddings (see EmbedQuery).
func (g *Gateway) SetCache(c VectorCache) { g.mu.Lock(); g.cache = c; g.mu.Unlock() }

// Embedder returns the embedding backend.
func (g *Gateway) Embedder() Embedder { g.mu.RLock(); defer g.mu.RUnlock(); return g.embedder }

// Provider returns a configured provider by id.
func (g *Gateway) Provider(id string) (Provider, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	p, ok := g.providers[id]
	return p, ok
}

// ProviderConfigured reports whether a provider has credentials/instance.
func (g *Gateway) ProviderConfigured(id string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.mockOnly {
		return id == ProviderMock
	}
	_, ok := g.providers[id]
	return ok
}

// ConfiguredProviders lists configured provider ids.
func (g *Gateway) ConfiguredProviders() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []string
	for id := range g.providers {
		out = append(out, id)
	}
	return out
}

// Models returns the registry snapshot with Available computed.
func (g *Gateway) Models() []domain.ModelConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]domain.ModelConfig, len(g.configs))
	for i, m := range g.configs {
		_, ok := g.providers[m.Provider]
		if g.mockOnly {
			ok = m.Provider == ProviderMock
		}
		m.Available = ok && m.Enabled
		out[i] = m
	}
	return out
}

// HasRealModel reports whether any non-mock model can serve the task.
func (g *Gateway) HasRealModel(task Task) bool {
	for _, c := range g.Candidates(task) {
		if c.Config.Provider != ProviderMock {
			return true
		}
	}
	return false
}

// Candidates ranks models for a task under the current policy.
func (g *Gateway) Candidates(task Task) []Candidate {
	g.mu.RLock()
	configs, pol, mockOnly := g.configs, g.policy, g.mockOnly
	g.mu.RUnlock()
	return Route(task, configs, func(p string) bool {
		if mockOnly {
			return p == ProviderMock
		}
		return g.ProviderConfigured(p)
	}, pol)
}

// Call describes one routed model invocation.
type Call struct {
	Task           Task
	UserID         *uuid.UUID
	IdeaID         *uuid.UUID
	ConversationID *uuid.UUID
	AgentRunID     *uuid.UUID
	Request        Request
	// Pin forces "provider/model" (Model Lab, explicit user choice). No fallback when pinned.
	Pin string
	// OnDelta enables streaming when non-nil.
	OnDelta StreamHandler
	// MaxAttempts bounds fallbacks across candidates (default 3).
	MaxAttempts int
	// OnRetry is notified before the gateway waits out a provider rate limit.
	OnRetry  func(wait time.Duration, reason string)
	retries  int             // rate-limit retry rounds already used
	tooLarge map[string]bool // candidates that rejected this request as too large (never retried)
}

// maxRateLimitRetries bounds how often one call waits out per-minute rate limits.
const maxRateLimitRetries = 2

// Attempt records one try against one model.
type Attempt struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Error     string `json:"error,omitempty"`
	LatencyMS int    `json:"latency_ms"`
}

// Result is a completed, accounted model call.
type Result struct {
	Response
	Provider    string    `json:"provider"`
	ModelKey    string    `json:"model_key"`
	LatencyMS   int       `json:"latency_ms"`
	CostUSD     float64   `json:"cost_usd"`
	ModelCallID uuid.UUID `json:"model_call_id"`
	Attempts    []Attempt `json:"attempts"`
}

// ErrNoModel is returned when no configured model can serve a task.
var ErrNoModel = errors.New("no AI model is configured for this task")

// Do routes and executes a model call with fallback across ranked candidates.
func (g *Gateway) Do(ctx context.Context, c Call) (*Result, error) {
	var cands []Candidate
	if c.Pin != "" {
		for _, m := range g.Models() {
			if m.Provider+"/"+m.Model == c.Pin {
				if !m.Available && !(m.Provider == ProviderMock) {
					return nil, fmt.Errorf("%w: %s is not available (provider not configured or model disabled)", ErrNoModel, c.Pin)
				}
				cands = []Candidate{{Config: m}}
			}
		}
		if len(cands) == 0 {
			return nil, fmt.Errorf("%w: unknown model %s", ErrNoModel, c.Pin)
		}
	} else {
		cands = g.Candidates(c.Task)
	}
	if len(cands) == 0 {
		return nil, ErrNoModel
	}
	maxAttempts := c.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var attempts []Attempt
	var lastErr error
	var minWait time.Duration
	sawRateLimit := false
	tried := 0
	for _, cand := range cands {
		if tried >= maxAttempts {
			break
		}
		if c.tooLarge[cand.Key()] {
			continue
		}
		tried++
		p, ok := g.Provider(cand.Config.Provider)
		if !ok {
			continue
		}
		req := c.Request
		req.Model = cand.Config.Model
		emitted := false
		var onDelta StreamHandler
		if c.OnDelta != nil {
			onDelta = func(d string) { emitted = true; c.OnDelta(d) }
		}
		start := time.Now()
		var resp *Response
		var err error
		if onDelta != nil {
			resp, err = p.Stream(ctx, req, onDelta)
		} else {
			resp, err = p.Complete(ctx, req)
		}
		lat := int(time.Since(start).Milliseconds())
		attempt := Attempt{Provider: cand.Config.Provider, Model: cand.Config.Model, LatencyMS: lat}
		callID := uuid.New()
		if err != nil {
			attempt.Error = err.Error()
			attempts = append(attempts, attempt)
			g.record(ctx, c, cand.Config, callID, nil, lat, err)
			lastErr = err
			if w, ok := retryAfter(err); ok {
				if !sawRateLimit || w < minWait {
					minWait = w
				}
				sawRateLimit = true
			}
			var pe *ProviderError
			if errors.As(err, &pe) && pe.StatusCode == http.StatusRequestEntityTooLarge {
				if c.tooLarge == nil {
					c.tooLarge = map[string]bool{}
				}
				c.tooLarge[cand.Key()] = true
			}
			if ctx.Err() != nil || emitted {
				return nil, err
			}
			if g.log != nil {
				g.log.WarnContext(ctx, "model call failed; trying next candidate", "provider", cand.Config.Provider, "model", cand.Config.Model, "task", c.Task, "error", err)
			}
			continue
		}
		attempts = append(attempts, attempt)
		if resp.Usage.InputTokens == 0 && resp.Usage.OutputTokens == 0 {
			resp.Usage = Usage{InputTokens: EstimateRequestTokens(req), OutputTokens: EstimateTokens(resp.Content), Estimated: true}
		}
		cost := CostUSD(cand.Config, resp.Usage)
		g.record(ctx, c, cand.Config, callID, resp, lat, nil)
		if resp.Model == "" {
			resp.Model = cand.Config.Model
		}
		return &Result{Response: *resp, Provider: cand.Config.Provider, ModelKey: cand.Key(), LatencyMS: lat, CostUSD: cost, ModelCallID: callID, Attempts: attempts}, nil
	}
	if lastErr == nil {
		lastErr = ErrNoModel
	}
	// Every candidate failed. If a provider asked us to retry shortly (per-minute limits on
	// free tiers), wait the shortest suggested time and try the candidates again.
	if sawRateLimit && c.retries < maxRateLimitRetries && len(cands) > len(c.tooLarge) {
		if c.OnRetry != nil {
			c.OnRetry(minWait, "rate limit")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(minWait):
		}
		c.retries++
		return g.Do(ctx, c)
	}
	return nil, lastErr
}

var retryAfterRe = regexp.MustCompile(`(?i)try again in (?:(\d+)m)?([\d.]+)s`)

// retryAfter extracts a provider-suggested wait from a rate-limit error. Per-minute limits
// reset within a minute, so waits up to 60s are worth it; longer ones (daily quotas) are not.
func retryAfter(err error) (time.Duration, bool) {
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 429 {
		return 0, false
	}
	m := retryAfterRe.FindStringSubmatch(pe.Message)
	if m == nil {
		return 3 * time.Second, true
	}
	mins, _ := strconv.Atoi(m[1])
	secs, _ := strconv.ParseFloat(m[2], 64)
	d := time.Duration(mins)*time.Minute + time.Duration(secs*float64(time.Second)) + 500*time.Millisecond
	if d > 60*time.Second {
		return 0, false
	}
	return d, true
}

// DoJSON runs a structured-output call and decodes into out, validating against schema.
// On invalid output it retries once with the validation errors appended.
func (g *Gateway) DoJSON(ctx context.Context, c Call, schema json.RawMessage, schemaName string, out any) (*Result, error) {
	c.Request.JSONSchema = schema
	c.Request.SchemaName = schemaName
	c.OnDelta = nil
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		res, err := g.Do(ctx, c)
		if err != nil {
			return nil, err
		}
		raw, ok := ExtractJSON(res.Content)
		if !ok {
			lastErr = fmt.Errorf("model %s returned no JSON", res.ModelKey)
		} else {
			var generic any
			if err := json.Unmarshal(raw, &generic); err != nil {
				lastErr = err
			} else if errs := ValidateJSONSchema(schema, generic); len(errs) > 0 {
				lastErr = fmt.Errorf("schema validation failed: %s", strings.Join(errs, "; "))
			} else if err := json.Unmarshal(raw, out); err != nil {
				lastErr = err
			} else {
				return res, nil
			}
		}
		c.Request.Messages = append(c.Request.Messages,
			Message{Role: RoleAssistant, Content: res.Content},
			Message{Role: RoleUser, Content: "Your previous output was invalid: " + lastErr.Error() + ". Reply with ONLY valid JSON matching the schema."})
	}
	return nil, lastErr
}

// Embed embeds texts with the configured embedder and records usage.
func (g *Gateway) Embed(ctx context.Context, userID *uuid.UUID, texts []string) ([][]float32, error) {
	e := g.Embedder()
	if e == nil {
		return nil, ErrNotConfigured
	}
	start := time.Now()
	vecs, usage, err := e.Embed(ctx, texts)
	lat := int(time.Since(start).Milliseconds())
	if g.usage != nil && e.ProviderID() != "local" {
		ev := &domain.UsageEvent{Provider: e.ProviderID(), Model: e.Model(), Operation: "embed", Task: "embedding",
			InputTokens: usage.InputTokens, TotalTokens: usage.InputTokens, TokensEstimated: usage.Estimated, LatencyMS: lat, Success: err == nil}
		if err != nil {
			ev.Error = err.Error()
		}
		ev.EstimatedCostUSD = embeddingCost(e.ProviderID(), e.Model(), usage.InputTokens)
		_ = g.usage.InsertUsage(context.WithoutCancel(ctx), userID, ev)
	}
	return vecs, err
}

// EmbedQuery embeds one search query, reusing a cached vector for identical text with the
// same embedding model. Agents often search and then build context for the same query, and
// paid embedders charge per call; document embeddings go through Embed and are never cached.
func (g *Gateway) EmbedQuery(ctx context.Context, userID *uuid.UUID, text string) ([]float32, error) {
	e := g.Embedder()
	if e == nil {
		return nil, ErrNotConfigured
	}
	g.mu.RLock()
	cache := g.cache
	g.mu.RUnlock()
	sum := sha256.Sum256([]byte(text))
	key := "emb:q:" + e.ProviderID() + ":" + e.Model() + ":" + hex.EncodeToString(sum[:])
	if cache != nil {
		if b, ok := cache.Get(ctx, key); ok && len(b) == 4*EmbeddingDims {
			return decodeVector(b), nil
		}
	}
	vecs, err := g.Embed(ctx, userID, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embedder returned %d vectors for 1 input", len(vecs))
	}
	if cache != nil && len(vecs[0]) == EmbeddingDims {
		cache.Set(ctx, key, encodeVector(vecs[0]), queryEmbeddingTTL)
	}
	return vecs[0], nil
}

func encodeVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func decodeVector(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func (g *Gateway) record(ctx context.Context, c Call, m domain.ModelConfig, callID uuid.UUID, resp *Response, lat int, callErr error) {
	if g.usage == nil {
		return
	}
	op := "chat"
	if c.OnDelta != nil {
		op = "chat_stream"
	}
	if len(c.Request.JSONSchema) > 0 {
		op = "structured"
	}
	ev := &domain.UsageEvent{ModelCallID: callID, Provider: m.Provider, Model: m.Model, Operation: op, Task: string(c.Task), LatencyMS: lat,
		Success: callErr == nil, ConversationID: c.ConversationID, IdeaID: c.IdeaID, AgentRunID: c.AgentRunID}
	if resp != nil {
		ev.InputTokens = resp.Usage.InputTokens
		ev.OutputTokens = resp.Usage.OutputTokens
		ev.TotalTokens = resp.Usage.Total()
		ev.TokensEstimated = resp.Usage.Estimated
		ev.ToolCalls = len(resp.ToolCalls)
		ev.EstimatedCostUSD = CostUSD(m, resp.Usage)
	}
	if callErr != nil {
		ev.Error = callErr.Error()
	}
	if err := g.usage.InsertUsage(context.WithoutCancel(ctx), c.UserID, ev); err != nil && g.log != nil {
		g.log.WarnContext(ctx, "failed to record usage", "error", err)
	}
}

// CostUSD estimates the cost of a call from registry prices.
func CostUSD(m domain.ModelConfig, u Usage) float64 {
	return float64(u.InputTokens)*m.InputCostPerMTok/1e6 + float64(u.OutputTokens)*m.OutputCostPerMTok/1e6
}

func embeddingCost(provider, model string, tokens int) float64 {
	price := 0.0
	switch {
	case provider == ProviderOpenAI && strings.Contains(model, "small"):
		price = 0.02
	case provider == ProviderOpenAI:
		price = 0.13
	case provider == ProviderGemini:
		price = 0.15
	}
	return float64(tokens) * price / 1e6
}
