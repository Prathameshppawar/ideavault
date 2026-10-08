// Package observability provides structured logging with correlation IDs and
// secret redaction.
package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyAgentRunID
	keyToolCallID
	keyUserID
)

// WithRequestID stores the request id in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

// RequestID returns the request id stored in ctx, if any.
func RequestID(ctx context.Context) string { s, _ := ctx.Value(keyRequestID).(string); return s }

// WithAgentRunID stores the agent run id in ctx.
func WithAgentRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyAgentRunID, id)
}

// AgentRunID returns the agent run id stored in ctx, if any.
func AgentRunID(ctx context.Context) string { s, _ := ctx.Value(keyAgentRunID).(string); return s }

// WithToolCallID stores the tool call id in ctx.
func WithToolCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyToolCallID, id)
}

// ToolCallID returns the tool call id stored in ctx, if any.
func ToolCallID(ctx context.Context) string { s, _ := ctx.Value(keyToolCallID).(string); return s }

// WithUserID stores the authenticated user id in ctx for logging.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyUserID, id)
}

// NewLogger builds the process logger. Format is "json" or "text".
func NewLogger(w io.Writer, level, format string) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl, ReplaceAttr: redactAttr}
	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(&contextHandler{Handler: h})
}

// contextHandler injects correlation ids from the context into every record.
type contextHandler struct{ slog.Handler }

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if v := RequestID(ctx); v != "" {
		r.AddAttrs(slog.String("request_id", v))
	}
	if v := AgentRunID(ctx); v != "" {
		r.AddAttrs(slog.String("agent_run_id", v))
	}
	if v := ToolCallID(ctx); v != "" {
		r.AddAttrs(slog.String("tool_call_id", v))
	}
	if v, _ := ctx.Value(keyUserID).(string); v != "" {
		r.AddAttrs(slog.String("user_id", v))
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}

var sensitiveKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|authorization|cookie|credential|master[_-]?key|ciphertext)`)

// redactAttr masks values of attributes whose keys look sensitive, and scrubs
// secret-looking substrings from string values.
func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if sensitiveKey.MatchString(a.Key) && a.Key != "tokens" && !strings.HasSuffix(a.Key, "_tokens") {
		return slog.String(a.Key, "[REDACTED]")
	}
	if a.Value.Kind() == slog.KindString {
		if s := a.Value.String(); s != "" {
			if r := RedactString(s); r != s {
				return slog.String(a.Key, r)
			}
		}
	}
	return a
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-(?:ant-|proj-)?[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`gsk_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_\-]{30,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-._~+/]{16,}=*`),
	regexp.MustCompile(`(?i)([?&](?:key|api_key|token)=)[^&\s]+`),
}

// RedactString replaces secret-looking substrings (API keys, bearer tokens) with a placeholder.
func RedactString(s string) string {
	for _, p := range secretPatterns {
		s = p.ReplaceAllStringFunc(s, func(m string) string {
			sub := p.FindStringSubmatch(m)
			if len(sub) > 1 && sub[1] != "" {
				return sub[1] + "[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return s
}
