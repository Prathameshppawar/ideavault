// Package app wires IdeaVault's components together. It is used by the API
// binary, the CLIs and the integration tests so they all run the same system.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/agent"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/config"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/connectors"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/db"
	httpapi "github.com/Prathameshppawar/ideavault/apps/api/internal/handler/http"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/adapters"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/jobs"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models/providers"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	rds "github.com/Prathameshppawar/ideavault/apps/api/internal/repository/redis"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/security"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

// App is a fully wired IdeaVault instance.
type App struct {
	Config     *config.Config
	Log        *slog.Logger
	Pool       *pgxpool.Pool
	Store      *postgres.Store
	Redis      *rds.Client
	Gateway    *models.Gateway
	Service    *service.Service
	Supervisor *agent.Supervisor
	Worker     *jobs.Worker
	API        *httpapi.API
	Fetcher    *security.SafeFetcher
}

// ProviderFactory builds chat providers from credentials.
func ProviderFactory(id string, c models.ProviderCredentials) (models.Provider, error) {
	switch id {
	case models.ProviderGroq:
		return providers.NewOpenAICompatible(providers.OpenAICompatConfig{ID: "groq", BaseURL: c.BaseURL, APIKey: c.APIKey}), nil
	case models.ProviderOpenAI:
		return providers.NewOpenAICompatible(providers.OpenAICompatConfig{ID: "openai", BaseURL: c.BaseURL, APIKey: c.APIKey}), nil
	case models.ProviderAnthropic:
		return providers.NewAnthropic(providers.AnthropicConfig{APIKey: c.APIKey, BaseURL: c.BaseURL}), nil
	case models.ProviderGemini:
		return providers.NewGemini(providers.GeminiConfig{APIKey: c.APIKey, BaseURL: c.BaseURL}), nil
	case models.ProviderOllama:
		base := c.BaseURL
		if base == "" {
			base = providers.DefaultOllamaBaseURL
		}
		return providers.NewOpenAICompatible(providers.OpenAICompatConfig{ID: "ollama", BaseURL: base, APIKey: c.APIKey}), nil
	}
	return nil, fmt.Errorf("unknown provider %q", id)
}

// chooseEmbedder picks the embedding backend: explicit setting, else the best configured provider, else local hashing.
func chooseEmbedder(cfg *config.Config) models.Embedder {
	p := strings.ToLower(cfg.EmbeddingProvider)
	if cfg.MockAI && (p == "auto" || p == "") {
		p = "local"
	}
	switch {
	case p == "openai" || (p == "auto" && cfg.OpenAIKey != ""):
		if cfg.OpenAIKey != "" {
			model := cfg.EmbeddingModel
			if model == "" {
				model = "text-embedding-3-small"
			}
			return providers.NewOpenAIEmbedder(providers.OpenAIEmbedConfig{ID: "openai", BaseURL: cfg.OpenAIBaseURL, APIKey: cfg.OpenAIKey, Model: model, Dimensions: models.EmbeddingDims})
		}
	case p == "gemini" || (p == "auto" && cfg.GeminiKey != ""):
		if cfg.GeminiKey != "" {
			return providers.NewGeminiEmbedder(providers.GeminiEmbedConfig{APIKey: cfg.GeminiKey, Model: cfg.EmbeddingModel})
		}
	case p == "ollama":
		if cfg.OllamaBaseURL != "" {
			model := cfg.EmbeddingModel
			if model == "" {
				model = "nomic-embed-text"
			}
			return providers.NewOpenAIEmbedder(providers.OpenAIEmbedConfig{ID: "ollama", BaseURL: cfg.OllamaBaseURL, Model: model})
		}
	}
	return providers.NewHashEmbedder()
}

// Build wires the application. It connects to PostgreSQL (and Redis when configured),
// applies migrations when enabled and seeds the model registry.
func Build(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	a := &App{Config: cfg, Log: log}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	a.Pool = pool
	if cfg.AutoMigrate {
		dir, err := db.FindMigrationsDir(cfg.MigrationsDir)
		if err != nil {
			return nil, err
		}
		migs, err := db.LoadMigrations(dir)
		if err != nil {
			return nil, err
		}
		n, err := db.MigrateUp(ctx, pool, migs, log)
		if err != nil {
			return nil, fmt.Errorf("migrate: %w", err)
		}
		if n > 0 {
			log.Info("migrations applied", "count", n)
		}
	}
	a.Store = postgres.New(pool)

	var cache service.Cache = rds.NewMemoryCache()
	var limiter rds.Limiter = rds.NewMemoryLimiter()
	if cfg.RedisURL != "" {
		if rc, err := rds.Connect(ctx, cfg.RedisURL); err != nil {
			log.Warn("redis unavailable; using in-memory cache and rate limiter", "error", err)
		} else {
			a.Redis = rc
			cache, limiter = rc, rc
		}
	}

	sealer, err := security.NewSealer(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	a.Fetcher = security.NewSafeFetcher(25<<20, 45*time.Second)

	gw := models.NewGateway(ProviderFactory, a.Store, log)
	gw.RegisterProvider(agent.NewOfflinePlanner())
	gw.SetEmbedder(chooseEmbedder(cfg))
	gw.SetCache(cache)
	if cfg.MockAI {
		gw.SetMockOnly(true)
	} else {
		for id, creds := range map[string]models.ProviderCredentials{
			models.ProviderGroq:      {APIKey: cfg.GroqKey},
			models.ProviderOpenAI:    {APIKey: cfg.OpenAIKey, BaseURL: cfg.OpenAIBaseURL},
			models.ProviderAnthropic: {APIKey: cfg.AnthropicKey},
			models.ProviderGemini:    {APIKey: cfg.GeminiKey},
			models.ProviderOllama:    {BaseURL: cfg.OllamaBaseURL},
		} {
			if creds.APIKey == "" && creds.BaseURL == "" || (id == models.ProviderOpenAI && creds.APIKey == "") {
				continue
			}
			if err := gw.ConfigureProvider(id, creds); err != nil {
				log.Warn("provider not configured", "provider", id, "error", err)
			}
		}
	}
	a.Gateway = gw

	conns := connectors.NewRegistry(a.Fetcher, nil)
	a.Service = service.New(service.Deps{Store: a.Store, Gateway: gw, Log: log, Config: cfg, Sealer: sealer, Fetcher: a.Fetcher, Cache: cache,
		Parser: adapters.NewRegistry(), Connectors: conns, ProviderFactory: ProviderFactory})
	if err := a.Service.SeedModels(ctx); err != nil {
		return nil, fmt.Errorf("seed models: %w", err)
	}
	if !cfg.MockAI {
		if err := a.Service.LoadVaultProviders(ctx); err != nil {
			log.Warn("loading stored provider keys failed", "error", err)
		}
	}
	if err := a.Service.EnsureBootstrapUser(ctx); err != nil {
		log.Warn("bootstrap user not created", "error", err)
	}
	if owner, err := a.Service.FirstUserID(ctx); err == nil {
		a.Service.LoadRoutingPolicy(ctx, owner)
	}
	a.Supervisor = agent.NewSupervisor(a.Service, log)

	handlers := map[string]jobs.Handler{}
	for k, h := range a.Service.JobHandlers() {
		handlers[k] = jobs.Handler(h)
	}
	a.Worker = jobs.NewWorker(a.Store, handlers, log, cfg.WorkerConcurrency, cfg.JobPollInterval)
	a.API = httpapi.New(httpapi.Options{Service: a.Service, Supervisor: a.Supervisor, Config: cfg, Logger: log, Limiter: limiter,
		Health: a.Health, NotifyJobs: a.Worker.Notify})
	return a, nil
}

// Health reports dependency health.
func (a *App) Health(ctx context.Context) map[string]any {
	out := map[string]any{"ok": true}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := a.Pool.Ping(cctx); err != nil {
		out["ok"] = false
		out["postgres"] = "down"
	} else {
		out["postgres"] = "up"
	}
	switch {
	case a.Redis == nil:
		out["redis"] = "disabled (in-memory fallback)"
	case a.Redis.Ping(cctx) != nil:
		out["redis"] = "down (in-memory fallback)"
	default:
		out["redis"] = "up"
	}
	out["ai"] = "offline planner only"
	if a.Gateway.HasRealModel(models.TaskSupervisor) {
		out["ai"] = "configured"
	}
	return out
}

// Handler returns the HTTP handler.
func (a *App) Handler() http.Handler { return a.API.Handler() }

// Close releases resources.
func (a *App) Close() {
	if a.Redis != nil {
		_ = a.Redis.Close()
	}
	if a.Pool != nil {
		a.Pool.Close()
	}
}
