package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	for _, k := range []string{"APP_ENV", "PORT", "WEB_ORIGINS", "DATABASE_URL", "REDIS_URL", "AUTO_MIGRATE", "MASTER_KEY", "MOCK_AI", "COOKIE_SECURE",
		"OPENAI_API_KEY", "GROQ_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "SESSION_TTL", "RATE_LIMIT_API_PER_MIN", "MAX_UPLOAD_MB"} {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != "development" || c.Port != 8080 || !c.AutoMigrate || c.MockAI || c.CookieSecure || c.IsProduction() {
		t.Errorf("unexpected defaults: %+v", c.Redacted())
	}
	if len(c.MasterKey) != 32 {
		t.Errorf("development key must be 32 bytes, got %d", len(c.MasterKey))
	}
	if c.SessionTTL != 30*24*time.Hour || c.MaxUploadBytes != 200<<20 || c.RateLimitAPIPerMin != 600 {
		t.Errorf("defaults changed: ttl=%v upload=%d api=%d", c.SessionTTL, c.MaxUploadBytes, c.RateLimitAPIPerMin)
	}
	if len(c.WebOrigins) != 1 || c.WebOrigins[0] != "http://localhost:3000" {
		t.Errorf("origins = %v", c.WebOrigins)
	}
}

func TestLoadOverridesAndValidation(t *testing.T) {
	clearEnv(t)
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv("MASTER_KEY", key)
	t.Setenv("WEB_ORIGINS", "https://app.example.com/, http://localhost:3000 ,")
	t.Setenv("PORT", "9000")
	t.Setenv("SESSION_TTL", "2h")
	t.Setenv("MOCK_AI", "true")
	t.Setenv("GOOGLE_API_KEY", "  AIza-from-google  ")
	t.Setenv("RATE_LIMIT_API_PER_MIN", "not-a-number")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(c.MasterKey) != "0123456789abcdef0123456789abcdef" || c.Port != 9000 || c.SessionTTL != 2*time.Hour || !c.MockAI {
		t.Errorf("overrides not applied: %+v", c.Redacted())
	}
	if len(c.WebOrigins) != 2 || c.WebOrigins[0] != "https://app.example.com" {
		t.Errorf("origins must be trimmed of slashes/blanks: %v", c.WebOrigins)
	}
	if c.GeminiKey != "AIza-from-google" || c.RateLimitAPIPerMin != 600 {
		t.Errorf("gemini key fallback / bad ints: %q %d", c.GeminiKey, c.RateLimitAPIPerMin)
	}

	t.Setenv("MASTER_KEY", base64.StdEncoding.EncodeToString([]byte("too short")))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("short MASTER_KEY must be rejected, got %v", err)
	}
	t.Setenv("MASTER_KEY", "%%%not-base64")
	if _, err := Load(); err == nil {
		t.Error("invalid base64 MASTER_KEY must be rejected")
	}

	clearEnv(t)
	t.Setenv("APP_ENV", "production")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MASTER_KEY is required") {
		t.Errorf("production without MASTER_KEY must fail, got %v", err)
	}
	t.Setenv("MASTER_KEY", key)
	t.Setenv("MOCK_AI", "true")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MOCK_AI") {
		t.Errorf("production with MOCK_AI must fail, got %v", err)
	}
	t.Setenv("MOCK_AI", "false")
	c, err = Load()
	if err != nil || !c.CookieSecure || !c.IsProduction() {
		t.Errorf("production defaults to secure cookies: %v %+v", err, c)
	}
}

func TestRedactedNeverContainsSecrets(t *testing.T) {
	c := &Config{OpenAIKey: "sk-secret-value-123456", GroqKey: "gsk_secret", MasterKey: []byte("0123456789abcdef0123456789abcdef"), DatabaseURL: "postgres://u:pw@h/db"}
	s := c.String()
	for _, secret := range []string{"sk-secret", "gsk_secret", "0123456789abcdef", "pw@"} {
		if strings.Contains(s, secret) {
			t.Errorf("String() leaks %q: %s", secret, s)
		}
	}
	if c.Redacted()["openai_key"] != true || c.Redacted()["groq_key"] != true || c.Redacted()["anthropic_key"] != false {
		t.Error("Redacted should report which keys are configured")
	}
}
