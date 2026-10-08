package observability

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRedactString(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		secret string // must not survive
		keep   string // must survive
	}{
		{"openai key", "openai: invalid key sk-abcdefghijklmnopqrstuvwx", "sk-abcdefghijklmnopqrstuvwx", "openai: invalid key"},
		{"openai project key", "key=sk-proj-AbCdEfGhIjKlMnOpQrStUv_123", "sk-proj-AbCdEfGhIjKlMnOpQrStUv_123", ""},
		{"anthropic key", "x-api-key: sk-ant-api03-abcdefghijklmnopqrst", "abcdefghijklmnopqrst", "x-api-key"},
		{"groq key", "groq rejected gsk_ABCDEFGHIJKLMNOPQRST1234", "gsk_ABCDEFGHIJKLMNOPQRST1234", "groq rejected"},
		{"gemini key", "url ?alt=sse AIzaSyD-abcdefghijklmnopqrstuvwxyz0123 failed", "AIzaSyD-abcdefghijklmnopqrstuvwxyz0123", "failed"},
		{"github token", "token ghp_abcdefghijklmnopqrstuvwxyz0123 expired", "ghp_abcdefghijklmnopqrstuvwxyz0123", "expired"},
		{"github fine-grained", "github_pat_11ABCDEFG0123456789_abcdefghij", "github_pat_11ABCDEFG0123456789_abcdefghij", ""},
		{"bearer header", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz.123", "abcdefghijklmnopqrstuvwxyz.123", "Bearer"},
		{"lowercase bearer", "sent bearer ivt_ABCDEFGHIJKLMNOPQRSTUVWX", "ivt_ABCDEFGHIJKLMNOPQRSTUVWX", "bearer"},
		{"query key", "GET https://x.example/v1?key=SECRETVALUE123&alt=json", "SECRETVALUE123", "alt=json"},
		{"query token", "callback?token=abc123def456", "abc123def456", "callback?token="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactString(tt.in)
			if strings.Contains(got, tt.secret) {
				t.Errorf("RedactString(%q) = %q still contains the secret", tt.in, got)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("RedactString(%q) = %q has no redaction marker", tt.in, got)
			}
			if tt.keep != "" && !strings.Contains(got, tt.keep) {
				t.Errorf("RedactString(%q) = %q lost context %q", tt.in, got, tt.keep)
			}
		})
	}
	for _, safe := range []string{"", "nothing to see here", "sk-short", "the task finished in 42 tokens", "ask-anything works"} {
		if got := RedactString(safe); got != safe {
			t.Errorf("RedactString(%q) changed harmless text to %q", safe, got)
		}
	}
}

func TestLoggerRedactsAttributesAndAddsCorrelationIDs(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, "info", "json")
	ctx := WithRequestID(context.Background(), "req-1")
	ctx = WithAgentRunID(ctx, "run-2")
	ctx = WithToolCallID(ctx, "tool-3")
	ctx = WithUserID(ctx, "user-4")
	log.InfoContext(ctx, "provider failed", "api_key", "sk-abcdefghijklmnopqrstuvwx", "password", "hunter2hunter2",
		"error", "upstream said: invalid key gsk_ABCDEFGHIJKLMNOPQRST1234", "input_tokens", 42, "tokens", 7)
	out := buf.String()
	for _, secret := range []string{"sk-abcdefghijklmnopqrstuvwx", "hunter2hunter2", "gsk_ABCDEFGHIJKLMNOPQRST1234"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output leaks %q: %s", secret, out)
		}
	}
	for _, want := range []string{`"request_id":"req-1"`, `"agent_run_id":"run-2"`, `"tool_call_id":"tool-3"`, `"user_id":"user-4"`, `"input_tokens":42`, `"tokens":7`} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %s: %s", want, out)
		}
	}
	if RequestID(ctx) != "req-1" || AgentRunID(ctx) != "run-2" || ToolCallID(ctx) != "tool-3" {
		t.Error("context accessors broken")
	}
	buf.Reset()
	log.Debug("hidden")
	if buf.Len() != 0 {
		t.Error("debug must be filtered at info level")
	}
}
