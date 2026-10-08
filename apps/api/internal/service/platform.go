package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/connectors"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/security"
)

// ---------- Credentials (encrypted at rest) ----------

func credAAD(userID uuid.UUID, ownerType, ownerKey, name string) string {
	return userID.String() + ":" + ownerType + ":" + ownerKey + ":" + name
}

func (s *Service) putSecret(ctx context.Context, userID uuid.UUID, ownerType, ownerKey, name, secret string) (*postgres.EncryptedCredential, error) {
	if s.sealer == nil {
		return nil, domain.Unavailable("credential encryption is not configured")
	}
	ct, nonce, ver, err := s.sealer.Seal([]byte(secret), credAAD(userID, ownerType, ownerKey, name))
	if err != nil {
		return nil, err
	}
	c := &postgres.EncryptedCredential{OwnerType: ownerType, OwnerKey: ownerKey, Name: name, Ciphertext: ct, Nonce: nonce, KeyVersion: ver, Hint: security.MaskSecret(secret)}
	if strings.HasSuffix(name, "url") {
		c.Hint = secret // URLs are not secret; show them
	}
	if err := s.store.PutCredential(ctx, userID, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) getSecret(ctx context.Context, userID uuid.UUID, ownerType, ownerKey, name string) (string, error) {
	c, err := s.store.GetCredential(ctx, userID, ownerType, ownerKey, name)
	if err != nil {
		return "", err
	}
	pt, err := s.sealer.Open(c.Ciphertext, c.Nonce, credAAD(userID, ownerType, ownerKey, name))
	if err != nil {
		return "", err
	}
	_ = s.store.TouchCredential(ctx, c.ID)
	return string(pt), nil
}

// ---------- Model providers ----------

// ProviderStatus describes an AI provider's configuration (never the secret).
type ProviderStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Configured  bool   `json:"configured"`
	Source      string `json:"source"` // env | vault | none
	Hint        string `json:"hint,omitempty"`
	BaseURL     string `json:"base_url,omitempty"`
	KeyRequired bool   `json:"key_required"`
	Models      int    `json:"models"`
	Notes       string `json:"notes"`
}

var providerNames = map[string]string{models.ProviderGroq: "Groq", models.ProviderOpenAI: "OpenAI", models.ProviderAnthropic: "Anthropic (Claude)",
	models.ProviderGemini: "Google Gemini", models.ProviderOllama: "Ollama / local (OpenAI-compatible)", models.ProviderMock: "Offline planner (no AI)"}

var providerNotes = map[string]string{
	models.ProviderGroq:      "Fast open-weight models (GPT-OSS, Qwen). Free tier available. No embeddings API — IdeaVault uses the local embedder.",
	models.ProviderOpenAI:    "GPT-5 family. Also provides embeddings (text-embedding-3-small at 768 dimensions).",
	models.ProviderAnthropic: "Claude models via the official Anthropic SDK.",
	models.ProviderGemini:    "Gemini models; free tier available; embeddings via gemini-embedding-001.",
	models.ProviderOllama:    "Run models locally for free. Set the base URL (e.g. http://localhost:11434/v1).",
	models.ProviderMock:      "Deterministic rule-based planner used when no AI provider is configured and in automated tests. It does not generate text with AI.",
}

// ProviderStatuses lists providers with their configuration state.
func (s *Service) ProviderStatuses(ctx context.Context, userID uuid.UUID) ([]ProviderStatus, error) {
	creds, err := s.store.ListCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	vault := map[string]postgres.EncryptedCredential{}
	for _, c := range creds {
		if c.OwnerType == "model_provider" {
			vault[c.OwnerKey+"/"+c.Name] = c
		}
	}
	envKey := map[string]bool{}
	if s.cfg != nil {
		envKey[models.ProviderGroq] = s.cfg.GroqKey != ""
		envKey[models.ProviderOpenAI] = s.cfg.OpenAIKey != ""
		envKey[models.ProviderAnthropic] = s.cfg.AnthropicKey != ""
		envKey[models.ProviderGemini] = s.cfg.GeminiKey != ""
		envKey[models.ProviderOllama] = s.cfg.OllamaBaseURL != ""
	}
	counts := map[string]int{}
	for _, m := range s.gw.Models() {
		counts[m.Provider]++
	}
	var out []ProviderStatus
	for _, id := range []string{models.ProviderGroq, models.ProviderOpenAI, models.ProviderAnthropic, models.ProviderGemini, models.ProviderOllama, models.ProviderMock} {
		ps := ProviderStatus{ID: id, Name: providerNames[id], Configured: s.gw.ProviderConfigured(id), Source: "none", KeyRequired: id != models.ProviderOllama && id != models.ProviderMock,
			Models: counts[id], Notes: providerNotes[id]}
		if c, ok := vault[id+"/api_key"]; ok {
			ps.Source, ps.Hint = "vault", c.Hint
		} else if envKey[id] {
			ps.Source, ps.Hint = "env", "set via environment"
		}
		if c, ok := vault[id+"/base_url"]; ok {
			ps.BaseURL = c.Hint
			if ps.Source == "none" {
				ps.Source = "vault"
			}
		}
		if id == models.ProviderMock {
			ps.Source = "builtin"
			ps.Configured = true
		}
		out = append(out, ps)
	}
	return out, nil
}

// SetProviderCredentials verifies and stores a provider key (encrypted), then activates the provider.
func (s *Service) SetProviderCredentials(ctx context.Context, userID uuid.UUID, provider, apiKey, baseURL string) (*ProviderStatus, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if _, ok := providerNames[provider]; !ok || provider == models.ProviderMock {
		return nil, domain.Invalid("provider", "unknown provider")
	}
	apiKey, baseURL = strings.TrimSpace(apiKey), strings.TrimSpace(baseURL)
	if apiKey == "" && !(provider == models.ProviderOllama && baseURL != "") {
		return nil, domain.Invalid("api_key", "API key is required")
	}
	if s.factory == nil {
		return nil, domain.Unavailable("provider factory not configured")
	}
	p, err := s.factory(provider, models.ProviderCredentials{APIKey: apiKey, BaseURL: baseURL})
	if err != nil {
		return nil, domain.Invalid("api_key", err.Error())
	}
	if err := verifyProvider(ctx, p, provider, s.gw); err != nil {
		return nil, domain.Invalid("api_key", "verification failed: "+security.RedactSecretError(err))
	}
	if apiKey != "" {
		if _, err := s.putSecret(ctx, userID, "model_provider", provider, "api_key", apiKey); err != nil {
			return nil, err
		}
	}
	if baseURL != "" {
		if _, err := s.putSecret(ctx, userID, "model_provider", provider, "base_url", baseURL); err != nil {
			return nil, err
		}
	}
	if err := s.gw.ConfigureProvider(provider, models.ProviderCredentials{APIKey: apiKey, BaseURL: baseURL}); err != nil {
		return nil, err
	}
	sts, _ := s.ProviderStatuses(ctx, userID)
	for _, st := range sts {
		if st.ID == provider {
			return &st, nil
		}
	}
	return nil, nil
}

// verifyProvider makes a minimal real call to prove credentials work.
func verifyProvider(ctx context.Context, p models.Provider, provider string, gw *models.Gateway) error {
	type lister interface {
		ListModels(ctx context.Context) ([]string, error)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if l, ok := p.(lister); ok {
		_, err := l.ListModels(cctx)
		return err
	}
	model := ""
	for _, m := range gw.Models() {
		if m.Provider == provider && m.Enabled {
			model = m.Model
			break
		}
	}
	if model == "" {
		return nil
	}
	_, err := p.Complete(cctx, models.Request{Model: model, Messages: []models.Message{{Role: models.RoleUser, Content: "Reply with OK."}}, MaxTokens: 400})
	return err
}

// RemoveProviderCredentials deletes stored provider credentials (env-configured providers stay active).
func (s *Service) RemoveProviderCredentials(ctx context.Context, userID uuid.UUID, provider string) error {
	_ = s.store.DeleteCredential(ctx, userID, "model_provider", provider, "api_key")
	_ = s.store.DeleteCredential(ctx, userID, "model_provider", provider, "base_url")
	creds := s.envProviderCreds(provider)
	return s.gw.ConfigureProvider(provider, creds)
}

func (s *Service) envProviderCreds(provider string) models.ProviderCredentials {
	if s.cfg == nil {
		return models.ProviderCredentials{}
	}
	switch provider {
	case models.ProviderGroq:
		return models.ProviderCredentials{APIKey: s.cfg.GroqKey}
	case models.ProviderOpenAI:
		return models.ProviderCredentials{APIKey: s.cfg.OpenAIKey, BaseURL: s.cfg.OpenAIBaseURL}
	case models.ProviderAnthropic:
		return models.ProviderCredentials{APIKey: s.cfg.AnthropicKey}
	case models.ProviderGemini:
		return models.ProviderCredentials{APIKey: s.cfg.GeminiKey}
	case models.ProviderOllama:
		return models.ProviderCredentials{BaseURL: s.cfg.OllamaBaseURL}
	}
	return models.ProviderCredentials{}
}

// LoadVaultProviders activates providers whose keys are stored in the vault (startup).
func (s *Service) LoadVaultProviders(ctx context.Context) error {
	all, err := s.store.AllModelProviderCredentials(ctx)
	if err != nil {
		return err
	}
	for uid, creds := range all {
		byProvider := map[string]models.ProviderCredentials{}
		for _, c := range creds {
			pt, err := s.sealer.Open(c.Ciphertext, c.Nonce, credAAD(uid, c.OwnerType, c.OwnerKey, c.Name))
			if err != nil {
				s.log.Warn("cannot decrypt stored provider credential (MASTER_KEY changed?)", "provider", c.OwnerKey)
				continue
			}
			pc := byProvider[c.OwnerKey]
			if c.Name == "api_key" {
				pc.APIKey = string(pt)
			} else if c.Name == "base_url" {
				pc.BaseURL = string(pt)
			}
			byProvider[c.OwnerKey] = pc
		}
		for id, pc := range byProvider {
			if err := s.gw.ConfigureProvider(id, pc); err != nil {
				s.log.Warn("failed to configure provider from vault", "provider", id, "error", err)
			}
		}
	}
	return nil
}

// SyncProviderModels lists models available from a configured provider (OpenAI-compatible providers).
func (s *Service) SyncProviderModels(ctx context.Context, provider string) ([]string, error) {
	p, ok := s.gw.Provider(provider)
	if !ok {
		return nil, domain.Invalid("provider", "provider is not configured")
	}
	l, ok := p.(interface {
		ListModels(ctx context.Context) ([]string, error)
	})
	if !ok {
		return nil, domain.Invalid("provider", "this provider does not support listing models")
	}
	ids, err := l.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

// RefreshModels reloads the registry snapshot into the gateway.
func (s *Service) RefreshModels(ctx context.Context) error {
	ms, err := s.store.ListModels(ctx)
	if err != nil {
		return err
	}
	s.gw.SetModels(ms)
	return nil
}

// SeedModels installs the built-in catalog.
func (s *Service) SeedModels(ctx context.Context) error {
	for _, m := range models.BuiltinModels {
		if err := s.store.SeedModel(ctx, m); err != nil {
			return err
		}
	}
	return s.RefreshModels(ctx)
}

// RoutingPreview shows which model each task would use and why.
type RoutingPreview struct {
	Task       models.Task        `json:"task"`
	Spec       models.TaskSpec    `json:"spec"`
	Candidates []models.Candidate `json:"candidates"`
}

// Routing returns the routing table for all tasks.
func (s *Service) Routing() []RoutingPreview {
	var tasks []models.Task
	for t := range models.TaskSpecs {
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i] < tasks[j] })
	var out []RoutingPreview
	for _, t := range tasks {
		c := s.gw.Candidates(t)
		if len(c) > 5 {
			c = c[:5]
		}
		out = append(out, RoutingPreview{Task: t, Spec: models.SpecFor(t), Candidates: c})
	}
	return out
}

// SetRoutingPolicy persists and applies routing preferences.
func (s *Service) SetRoutingPolicy(ctx context.Context, userID uuid.UUID, pol models.RoutingPolicy) (models.RoutingPolicy, error) {
	for t, key := range pol.Pins {
		if key == "" {
			delete(pol.Pins, t)
			continue
		}
		found := false
		for _, m := range s.gw.Models() {
			if m.Provider+"/"+m.Model == key {
				found = true
			}
		}
		if !found {
			return pol, domain.Invalid("pins", "unknown model "+key)
		}
	}
	if _, err := s.store.MergeSettings(ctx, userID, map[string]any{"routing": pol}); err != nil {
		return pol, err
	}
	s.gw.SetPolicy(pol)
	return pol, nil
}

// LoadRoutingPolicy applies the stored policy (startup).
func (s *Service) LoadRoutingPolicy(ctx context.Context, userID uuid.UUID) {
	st, err := s.store.GetSettings(ctx, userID)
	if err != nil {
		return
	}
	raw, ok := st["routing"]
	if !ok {
		return
	}
	b, _ := json.Marshal(raw)
	var pol models.RoutingPolicy
	if json.Unmarshal(b, &pol) == nil {
		s.gw.SetPolicy(pol)
	}
}

// ---------- Model Lab ----------

// LabInput runs one task across several models.
type LabInput struct {
	Task       string          `json:"task"`
	Title      string          `json:"title"`
	System     string          `json:"system"`
	Prompt     string          `json:"prompt"`
	Models     []string        `json:"models"` // provider/model keys
	ExpectJSON bool            `json:"expect_json"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	Reference  string          `json:"reference,omitempty"` // expected answer for correctness scoring
	MaxTokens  int             `json:"max_tokens,omitempty"`
}

// RunLab executes the prompt on each model in parallel and stores comparable metrics.
func (s *Service) RunLab(ctx context.Context, userID uuid.UUID, in LabInput) (*postgres.LabRun, error) {
	if strings.TrimSpace(in.Prompt) == "" {
		return nil, domain.Invalid("prompt", "prompt is required")
	}
	if len(in.Models) == 0 || len(in.Models) > 8 {
		return nil, domain.Invalid("models", "choose 1–8 models")
	}
	if in.ExpectJSON && len(in.JSONSchema) > 0 && !json.Valid(in.JSONSchema) {
		return nil, domain.Invalid("json_schema", "invalid JSON schema")
	}
	if in.Task == "" {
		in.Task = "custom"
	}
	run := &postgres.LabRun{Task: in.Task, Title: firstNonEmptyStr(in.Title, trimTo(in.Prompt, 60)), System: in.System, Prompt: in.Prompt,
		ExpectJSON: in.ExpectJSON, JSONSchema: in.JSONSchema, Reference: in.Reference}
	if err := s.store.CreateLabRun(ctx, userID, run); err != nil {
		return nil, err
	}
	maxTok := in.MaxTokens
	if maxTok <= 0 {
		maxTok = 4000
	}
	var wg sync.WaitGroup
	for _, key := range in.Models {
		key := key
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := &postgres.LabResult{RunID: run.ID}
			if i := strings.Index(key, "/"); i > 0 {
				res.Provider, res.Model = key[:i], key[i+1:]
			}
			req := models.Request{System: in.System, Messages: []models.Message{{Role: models.RoleUser, Content: in.Prompt}}, MaxTokens: maxTok}
			if in.ExpectJSON && len(in.JSONSchema) > 0 {
				req.JSONSchema, req.SchemaName = in.JSONSchema, "lab_output"
			}
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
			defer cancel()
			start := time.Now()
			r, err := s.gw.Do(cctx, models.Call{Task: models.TaskLab, UserID: &userID, Pin: key, Request: req})
			res.LatencyMS = int(time.Since(start).Milliseconds())
			if err != nil {
				res.Error = security.RedactSecretError(err)
			} else {
				res.Output = r.Content
				res.LatencyMS = r.LatencyMS
				res.InputTokens, res.OutputTokens, res.TotalTokens = r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.Total()
				res.EstimatedCostUSD = r.CostUSD
				if in.ExpectJSON {
					valid := false
					var errs []string
					if raw, ok := models.ExtractJSON(r.Content); ok {
						var v any
						if json.Unmarshal(raw, &v) == nil {
							valid = true
							if len(in.JSONSchema) > 0 {
								errs = models.ValidateJSONSchema(in.JSONSchema, v)
								valid = len(errs) == 0
							}
						}
					} else {
						errs = []string{"no JSON found in output"}
					}
					res.ValidJSON = &valid
					b, _ := json.Marshal(nonNil(errs))
					res.SchemaErrors = b
				}
				if strings.TrimSpace(in.Reference) != "" {
					c := float32(tokenF1(in.Reference, r.Content))
					res.Correctness = &c
				}
			}
			if err := s.store.InsertLabResult(context.WithoutCancel(ctx), res); err != nil {
				s.log.Warn("store lab result failed", "error", err)
			}
		}()
	}
	wg.Wait()
	_ = s.store.CompleteLabRun(ctx, run.ID, "COMPLETED")
	return s.store.GetLabRun(ctx, userID, run.ID)
}

// tokenF1 is a simple lexical overlap score against a reference answer (0..1).
func tokenF1(ref, out string) float64 {
	rt := strings.Fields(normalizeStatement(ref))
	ot := strings.Fields(normalizeStatement(out))
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

// ---------- Usage ----------

// UsageSummary aggregates model and connector usage.
type UsageSummary struct {
	Since           time.Time                    `json:"since"`
	Totals          postgres.UsageGroup          `json:"totals"`
	ByProvider      []postgres.UsageGroup        `json:"by_provider"`
	ByModel         []postgres.UsageGroup        `json:"by_model"`
	ByTask          []postgres.UsageGroup        `json:"by_task"`
	ByDay           []postgres.UsageGroup        `json:"by_day"`
	ByIdea          []postgres.UsageGroup        `json:"by_idea"`
	Recent          []domain.UsageEvent          `json:"recent"`
	Connectors      []postgres.ConnectorUsageRow `json:"connectors"`
	ConnectorEvents []domain.ConnectorEvent      `json:"connector_events"`
}

// Usage returns usage analytics for a filter.
func (s *Service) Usage(ctx context.Context, f postgres.UsageFilter) (*UsageSummary, error) {
	out := &UsageSummary{Since: f.Since}
	var err error
	if out.ByProvider, err = s.store.UsageBreakdown(ctx, f, "provider"); err != nil {
		return nil, err
	}
	out.ByModel, _ = s.store.UsageBreakdown(ctx, f, "model")
	out.ByTask, _ = s.store.UsageBreakdown(ctx, f, "task")
	out.ByDay, _ = s.store.UsageBreakdown(ctx, f, "day")
	out.ByIdea, _ = s.store.UsageBreakdown(ctx, f, "idea")
	out.Recent, _ = s.store.ListUsage(ctx, f)
	for _, g := range out.ByProvider {
		out.Totals.Calls += g.Calls
		out.Totals.Failures += g.Failures
		out.Totals.InputTokens += g.InputTokens
		out.Totals.OutputTokens += g.OutputTokens
		out.Totals.TotalTokens += g.TotalTokens
		out.Totals.CostUSD += g.CostUSD
		out.Totals.ToolCalls += g.ToolCalls
		out.Totals.Estimated += g.Estimated
		out.Totals.AvgLatencyMS += g.AvgLatencyMS * float64(g.Calls)
	}
	if out.Totals.Calls > 0 {
		out.Totals.AvgLatencyMS /= float64(out.Totals.Calls)
	}
	out.Totals.Key = "total"
	out.Connectors, _ = s.store.ConnectorUsage(ctx, f.UserID, f.Since)
	out.ConnectorEvents, _ = s.store.ListConnectorEvents(ctx, f.UserID, "", f.Since, 100)
	return out, nil
}

// ---------- Connectors ----------

// ConnectorView is a connector definition with its honest status.
type ConnectorView struct {
	connectors.Definition
	Status             domain.ConnectorStatus `json:"status"`
	LastError          string                 `json:"last_error,omitempty"`
	LastCheckedAt      *time.Time             `json:"last_checked_at,omitempty"`
	ConnectedAt        *time.Time             `json:"connected_at,omitempty"`
	GrantedPermissions []string               `json:"granted_permissions"`
	CredentialHints    map[string]string      `json:"credential_hints,omitempty"`
}

// ListConnectors returns all connectors with status.
func (s *Service) ListConnectors(ctx context.Context, userID uuid.UUID) ([]ConnectorView, error) {
	states, err := s.store.ListConnectorStates(ctx, userID)
	if err != nil {
		return nil, err
	}
	creds, _ := s.store.ListCredentials(ctx, userID)
	var out []ConnectorView
	for _, d := range s.conns.All() {
		v := ConnectorView{Definition: d, Status: domain.ConnectorNotConnected, GrantedPermissions: []string{}}
		if st, ok := states[d.Key]; ok {
			v.Status, v.LastError, v.LastCheckedAt, v.ConnectedAt, v.GrantedPermissions = st.Status, st.LastError, st.LastCheckedAt, st.ConnectedAt, st.GrantedPermissions
		}
		for _, c := range creds {
			if c.OwnerType == "connector" && c.OwnerKey == d.Key {
				if v.CredentialHints == nil {
					v.CredentialHints = map[string]string{}
				}
				v.CredentialHints[c.Name] = c.Hint
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// ConnectInput connects a connector with credentials and granted permissions.
type ConnectInput struct {
	Credentials map[string]string `json:"credentials"`
	Permissions []string          `json:"permissions"`
}

// ConnectConnector verifies credentials, stores them encrypted and records status.
func (s *Service) ConnectConnector(ctx context.Context, userID uuid.UUID, key string, in ConnectInput) (*ConnectorView, error) {
	c, ok := s.conns.Get(key)
	if !ok {
		return nil, domain.NotFound("connector")
	}
	def := c.Definition()
	perms := in.Permissions
	if len(perms) == 0 {
		perms = def.Permissions
	}
	for _, p := range perms {
		if !contains(def.Permissions, p) {
			return nil, domain.Invalid("permissions", "unknown permission "+p)
		}
	}
	for _, f := range def.CredentialFields {
		if f.Required && strings.TrimSpace(in.Credentials[f.Name]) == "" {
			return nil, domain.Invalid("credentials", f.Label+" is required")
		}
	}
	start := time.Now()
	verr := c.Verify(ctx, in.Credentials)
	ev := &domain.ConnectorEvent{ConnectorKey: key, Operation: "connect", Success: verr == nil, LatencyMS: int(time.Since(start).Milliseconds())}
	if verr != nil {
		ev.Error = security.RedactSecretError(verr)
	}
	_ = s.store.InsertConnectorEvent(ctx, userID, ev)
	state := postgres.ConnectorState{ConnectorKey: key, Status: domain.ConnectorConnected, GrantedPermissions: perms, Config: map[string]any{}}
	if verr != nil {
		state.Status = domain.ConnectorError
		state.LastError = security.RedactSecretError(verr)
		if errors.Is(verr, connectors.ErrNotImplemented) || def.AuthType == "export" {
			state.Status = domain.ConnectorNotConnected
		}
	} else {
		for _, f := range def.CredentialFields {
			if v := strings.TrimSpace(in.Credentials[f.Name]); v != "" {
				if _, err := s.putSecret(ctx, userID, "connector", key, f.Name, v); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := s.store.UpsertConnectorState(ctx, userID, state); err != nil {
		return nil, err
	}
	views, err := s.ListConnectors(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, v := range views {
		if v.Key == key {
			if verr != nil {
				return &v, domain.Invalid("credentials", state.LastError)
			}
			return &v, nil
		}
	}
	return nil, domain.NotFound("connector")
}

// DisconnectConnector removes credentials and marks the connector NOT_CONNECTED.
func (s *Service) DisconnectConnector(ctx context.Context, userID uuid.UUID, key string) error {
	c, ok := s.conns.Get(key)
	if !ok {
		return domain.NotFound("connector")
	}
	for _, f := range c.Definition().CredentialFields {
		_ = s.store.DeleteCredential(ctx, userID, "connector", key, f.Name)
	}
	return s.store.UpsertConnectorState(ctx, userID, postgres.ConnectorState{ConnectorKey: key, Status: domain.ConnectorNotConnected, Config: map[string]any{}})
}

// InvokeConnectorTool runs a connector tool after status and permission checks, recording usage.
func (s *Service) InvokeConnectorTool(ctx context.Context, userID uuid.UUID, tool string, args json.RawMessage, runID *uuid.UUID) (any, error) {
	c, t, ok := s.conns.FindTool(tool)
	if !ok {
		return nil, domain.NotFound("connector tool")
	}
	def := c.Definition()
	states, err := s.store.ListConnectorStates(ctx, userID)
	if err != nil {
		return nil, err
	}
	st, ok := states[def.Key]
	if !ok || st.Status != domain.ConnectorConnected {
		return nil, domain.Forbidden(def.Name + " is not connected. Connect it under Connectors first.")
	}
	if !contains(st.GrantedPermissions, t.Permission) {
		return nil, domain.Forbidden(fmt.Sprintf("permission %q was not granted to %s", t.Permission, def.Name))
	}
	creds := map[string]string{}
	for _, f := range def.CredentialFields {
		if v, err := s.getSecret(ctx, userID, "connector", def.Key, f.Name); err == nil {
			creds[f.Name] = v
		}
	}
	start := time.Now()
	res, err := c.Invoke(ctx, tool, args, creds)
	ev := &domain.ConnectorEvent{ConnectorKey: def.Key, Tool: tool, Operation: "invoke", Success: err == nil, LatencyMS: int(time.Since(start).Milliseconds()), AgentRunID: runID}
	if err != nil {
		ev.Error = security.RedactSecretError(err)
	}
	_ = s.store.InsertConnectorEvent(ctx, userID, ev)
	if err != nil {
		return nil, errors.New(security.RedactSecretError(err))
	}
	return res, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
