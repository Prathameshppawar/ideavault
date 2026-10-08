// Package config loads IdeaVault API configuration from the environment.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full runtime configuration. Secrets are never logged; use Redacted().
type Config struct {
	Env         string // development | test | production
	Port        int
	PublicURL   string // externally visible API URL (used in OpenAPI servers)
	WebOrigins  []string
	DatabaseURL string
	RedisURL    string // optional; empty disables Redis (in-memory fallbacks are used)

	MigrationsDir string
	AutoMigrate   bool

	// MasterKey encrypts credentials at rest (AES-256-GCM). 32 raw bytes.
	MasterKey []byte

	SessionTTL     time.Duration
	CookieSecure   bool
	CookieDomain   string
	AllowSignup    bool // first-run setup is always allowed when no user exists
	BootstrapEmail string
	BootstrapPass  string

	// Provider credentials from the environment (may also be stored encrypted in DB).
	OpenAIKey     string
	OpenAIBaseURL string
	GroqKey       string
	AnthropicKey  string
	GeminiKey     string
	OllamaBaseURL string

	EmbeddingProvider string // auto | openai | gemini | ollama | local
	EmbeddingModel    string

	// Mock forces the offline deterministic planner as the only model (tests/E2E).
	MockAI bool

	RateLimitAuthPerMin   int
	RateLimitAgentPerMin  int
	RateLimitImportPerMin int
	RateLimitAPIPerMin    int

	WorkerConcurrency int
	JobPollInterval   time.Duration

	MaxUploadBytes int64
	LogLevel       string
	LogFormat      string // json | text
}

// Load reads configuration from environment variables with sane defaults.
func Load() (*Config, error) {
	c := &Config{
		Env:                   env("APP_ENV", "development"),
		Port:                  envInt("PORT", 8080),
		PublicURL:             env("PUBLIC_API_URL", ""),
		WebOrigins:            envList("WEB_ORIGINS", "http://localhost:3000"),
		DatabaseURL:           env("DATABASE_URL", "postgres://ideavault:ideavault@localhost:5432/ideavault?sslmode=disable"),
		RedisURL:              env("REDIS_URL", ""),
		MigrationsDir:         env("MIGRATIONS_DIR", ""),
		AutoMigrate:           envBool("AUTO_MIGRATE", true),
		SessionTTL:            envDuration("SESSION_TTL", 30*24*time.Hour),
		CookieDomain:          env("COOKIE_DOMAIN", ""),
		AllowSignup:           envBool("ALLOW_SIGNUP", false),
		BootstrapEmail:        env("BOOTSTRAP_EMAIL", ""),
		BootstrapPass:         env("BOOTSTRAP_PASSWORD", ""),
		OpenAIKey:             env("OPENAI_API_KEY", ""),
		OpenAIBaseURL:         env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		GroqKey:               env("GROQ_API_KEY", ""),
		AnthropicKey:          env("ANTHROPIC_API_KEY", ""),
		GeminiKey:             firstNonEmpty(os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY")),
		OllamaBaseURL:         env("OLLAMA_BASE_URL", ""),
		EmbeddingProvider:     env("EMBEDDING_PROVIDER", "auto"),
		EmbeddingModel:        env("EMBEDDING_MODEL", ""),
		MockAI:                envBool("MOCK_AI", false),
		RateLimitAuthPerMin:   envInt("RATE_LIMIT_AUTH_PER_MIN", 10),
		RateLimitAgentPerMin:  envInt("RATE_LIMIT_AGENT_PER_MIN", 30),
		RateLimitImportPerMin: envInt("RATE_LIMIT_IMPORT_PER_MIN", 10),
		RateLimitAPIPerMin:    envInt("RATE_LIMIT_API_PER_MIN", 600),
		WorkerConcurrency:     envInt("WORKER_CONCURRENCY", 2),
		JobPollInterval:       envDuration("JOB_POLL_INTERVAL", 1500*time.Millisecond),
		MaxUploadBytes:        int64(envInt("MAX_UPLOAD_MB", 200)) << 20,
		LogLevel:              env("LOG_LEVEL", "info"),
		LogFormat:             env("LOG_FORMAT", "json"),
	}
	c.CookieSecure = envBool("COOKIE_SECURE", c.Env == "production")

	key := os.Getenv("MASTER_KEY")
	switch {
	case key != "":
		raw, err := base64.StdEncoding.DecodeString(key)
		if err != nil || len(raw) != 32 {
			return nil, errors.New("MASTER_KEY must be 32 bytes, base64-encoded (openssl rand -base64 32)")
		}
		c.MasterKey = raw
	case c.Env == "production":
		return nil, errors.New("MASTER_KEY is required in production")
	default:
		// Deterministic development key so local credentials survive restarts.
		// Never used in production (enforced above).
		c.MasterKey = []byte("ideavault-dev-master-key-32bytes")
	}
	if c.Env == "production" && c.MockAI {
		return nil, errors.New("MOCK_AI must not be enabled in production")
	}
	return c, nil
}

// IsProduction reports whether the API runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }

// Redacted returns a log-safe summary of the configuration.
func (c *Config) Redacted() map[string]any {
	return map[string]any{
		"env":                c.Env,
		"port":               c.Port,
		"web_origins":        c.WebOrigins,
		"redis":              c.RedisURL != "",
		"auto_migrate":       c.AutoMigrate,
		"mock_ai":            c.MockAI,
		"openai_key":         c.OpenAIKey != "",
		"groq_key":           c.GroqKey != "",
		"anthropic_key":      c.AnthropicKey != "",
		"gemini_key":         c.GeminiKey != "",
		"ollama":             c.OllamaBaseURL != "",
		"embedding_provider": c.EmbeddingProvider,
		"cookie_secure":      c.CookieSecure,
	}
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envList(k, def string) []string {
	raw := env(k, def)
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.TrimRight(p, "/"))
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// String implements fmt.Stringer without leaking secrets.
func (c *Config) String() string { return fmt.Sprintf("%v", c.Redacted()) }
