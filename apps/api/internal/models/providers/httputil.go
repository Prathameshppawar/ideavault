package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

const (
	// maxResponseBytes caps any buffered (non-streaming) response body.
	maxResponseBytes = 32 << 20
	// maxErrorBodyBytes caps how much of an error response body is read.
	maxErrorBodyBytes = 64 << 10
	// maxSSEEventBytes caps a single SSE line / event payload.
	maxSSEEventBytes = 32 << 20
	// maxErrorMessageChars truncates raw (non-JSON) error bodies in messages.
	maxErrorMessageChars = 500
)

// hostedProviderIDs are OpenAI-compatible endpoints that always require an API
// key. Other IDs (ollama, lmstudio, custom local servers) may run keyless.
var hostedProviderIDs = map[string]bool{
	"openai":        true,
	"groq":          true,
	"gemini-openai": true,
}

// httpClientOrDefault returns c, or a fresh client with no overall timeout:
// request lifetime is governed by the caller's context so long generations and
// streams are not cut off, while cancellation still stops them promptly.
func httpClientOrDefault(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{}
}

// httpCaller performs JSON HTTP calls on behalf of one provider and converts
// failures into classified, secret-free errors.
type httpCaller struct {
	client   *http.Client
	provider string
	secrets  []string // literal secret values to scrub from error text
}

// do sends an HTTP request with an optional JSON body. On a 2xx response the
// caller owns resp.Body. Non-2xx responses are read (bounded), closed and
// returned as *models.ProviderError. Context cancellation is returned as an
// error wrapping ctx.Err().
func (h httpCaller) do(ctx context.Context, method, url string, headers map[string]string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, h.errorf(0, false, "encode request: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, h.errorf(0, false, "build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, h.transportError(ctx, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		data, _ := readBounded(resp.Body, maxErrorBodyBytes)
		return nil, h.statusError(resp.StatusCode, data)
	}
	return resp, nil
}

// decodeJSON reads a bounded response body and unmarshals it into v.
func (h httpCaller) decodeJSON(ctx context.Context, resp *http.Response, v any) error {
	defer resp.Body.Close()
	data, err := readBounded(resp.Body, maxResponseBytes)
	if err != nil {
		return h.readError(ctx, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return h.errorf(resp.StatusCode, false, "invalid JSON response: %v (body: %s)", err, truncate(string(data), 200))
	}
	return nil
}

// errorf builds a redacted ProviderError.
func (h httpCaller) errorf(status int, retryable bool, format string, args ...any) *models.ProviderError {
	return &models.ProviderError{
		Provider:   h.provider,
		StatusCode: status,
		Retryable:  retryable,
		Message:    redact(fmt.Sprintf(format, args...), h.secrets...),
	}
}

// statusError classifies a non-2xx response.
func (h httpCaller) statusError(status int, body []byte) *models.ProviderError {
	msg := errorMessageFromBody(body)
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &models.ProviderError{
		Provider:   h.provider,
		StatusCode: status,
		Retryable:  retryableStatus(status),
		Message:    redact(msg, h.secrets...),
	}
}

// transportError classifies a failure to obtain a response.
func (h httpCaller) transportError(ctx context.Context, err error) error {
	return transportError(ctx, h.provider, err, h.secrets...)
}

// readError classifies a failure while reading a response body or stream.
func (h httpCaller) readError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", h.provider, ctxErr)
	}
	return h.errorf(0, true, "read response: %v", err)
}

// transportError converts a network-level error into either a wrapped context
// error (when ctx is done) or a classified ProviderError.
func transportError(ctx context.Context, provider string, err error, secrets ...string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", provider, ctxErr)
	}
	return &models.ProviderError{
		Provider:  provider,
		Retryable: retryableNetError(err),
		Message:   redact(err.Error(), secrets...),
	}
}

// retryableStatus reports whether an HTTP status is worth retrying:
// timeouts, conflicts, rate limits and server errors (including Anthropic's 529).
func retryableStatus(status int) bool {
	switch {
	case status == http.StatusRequestTimeout, status == http.StatusConflict,
		status == http.StatusTooEarly, status == http.StatusTooManyRequests:
		return true
	case status >= 500:
		return true
	}
	return false
}

// retryableNetError reports whether a transport error is transient.
func retryableNetError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF)
}

// isRejection reports whether err is a 400/422 ProviderError whose message
// mentions any of the (lower-case) needles. Used to detect unsupported
// optional parameters so a request can be retried without them.
func isRejection(err error, needles ...string) bool {
	var pe *models.ProviderError
	if !errors.As(err, &pe) || (pe.StatusCode != http.StatusBadRequest && pe.StatusCode != http.StatusUnprocessableEntity) {
		return false
	}
	msg := strings.ToLower(pe.Message)
	for _, n := range needles {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

// readBounded reads at most limit bytes, failing if the body is larger.
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return data, err
	}
	if int64(len(data)) > limit {
		return data[:limit], fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return data, nil
}

// errorMessageFromBody extracts a human-readable message from common provider
// error envelopes: {"error":{"message":..}}, {"error":".."}, {"message":..},
// and Gemini's array form [{"error":{..}}]. Falls back to the truncated body.
func errorMessageFromBody(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] == '[' {
		var arr []json.RawMessage
		if json.Unmarshal(trimmed, &arr) == nil && len(arr) > 0 {
			trimmed = arr[0]
		}
	}
	var env struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  string          `json:"detail"`
	}
	if json.Unmarshal(trimmed, &env) == nil {
		if len(env.Error) > 0 {
			var s string
			if json.Unmarshal(env.Error, &s) == nil && s != "" {
				return s
			}
			var obj struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Status  string `json:"status"`
			}
			if json.Unmarshal(env.Error, &obj) == nil && obj.Message != "" {
				return obj.Message
			}
		}
		if env.Message != "" {
			return env.Message
		}
		if env.Detail != "" {
			return env.Detail
		}
	}
	return truncate(string(trimmed), maxErrorMessageChars)
}

// embeddedErrorMessage returns the message of an "error" member found inside
// an otherwise successful (2xx) payload, or "" when raw is absent/null.
func embeddedErrorMessage(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	env, err := json.Marshal(map[string]json.RawMessage{"error": raw})
	if err != nil {
		return truncate(string(raw), maxErrorMessageChars)
	}
	if msg := errorMessageFromBody(env); msg != "" {
		return msg
	}
	return "unknown error"
}

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// Redaction patterns for credentials that might appear in error text (echoed
// headers, URLs with ?key=, or raw key strings).
var redactPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)(authorization|x-api-key|x-goog-api-key|api[-_]?key)("?\s*[:=]\s*"?)(bearer\s+)?[^\s",;&]+`), "${1}${2}[REDACTED]"},
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`), "Bearer [REDACTED]"},
	{regexp.MustCompile(`(?i)([?&](?:key|api_key|apikey|access_token|token)=)[^&\s"']+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`\b(?:sk-ant-[A-Za-z0-9_-]{8,}|sk-[A-Za-z0-9_-]{16,}|gsk_[A-Za-z0-9]{16,}|AIza[0-9A-Za-z_-]{30,})`), "[REDACTED]"},
}

// redact scrubs credentials from s: each literal secret value plus common
// header / query / key-shaped patterns.
func redact(s string, secrets ...string) string {
	for _, sec := range secrets {
		if len(sec) >= 4 {
			s = strings.ReplaceAll(s, sec, "[REDACTED]")
		}
	}
	for _, p := range redactPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// ---------------------------------------------------------------------------
// Server-Sent Events

// sseEvent is one dispatched Server-Sent Event.
type sseEvent struct {
	Event string
	Data  string
	ID    string
}

// sseReader parses a text/event-stream body. It uses bufio.Reader (not
// bufio.Scanner) so lines longer than 64 KiB are handled, up to maxSSEEventBytes.
// It supports comments, multi-line data, event/id fields, and LF or CRLF line
// endings.
type sseReader struct {
	r *bufio.Reader
}

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// Next returns the next event. It returns io.EOF when the stream ends; a
// trailing event without a terminating blank line is still dispatched.
func (s *sseReader) Next() (sseEvent, error) {
	var (
		ev      sseEvent
		data    strings.Builder
		hasData bool
		hasAny  bool
	)
	for {
		line, err := s.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) && hasAny {
				ev.Data = data.String()
				return ev, nil
			}
			return sseEvent{}, err
		}
		if line == "" {
			if hasData || ev.Event != "" {
				ev.Data = data.String()
				return ev, nil
			}
			continue
		}
		if line[0] == ':' {
			continue // comment / keep-alive
		}
		field, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			if data.Len()+len(value) > maxSSEEventBytes {
				return sseEvent{}, fmt.Errorf("sse event exceeds %d bytes", maxSSEEventBytes)
			}
			data.WriteString(value)
			hasData = true
			hasAny = true
		case "event":
			ev.Event = value
			hasAny = true
		case "id":
			ev.ID = value
		}
	}
}

// readLine reads one line without its terminator. A final line without a
// newline is returned before io.EOF.
func (s *sseReader) readLine() (string, error) {
	var buf []byte
	for {
		chunk, err := s.r.ReadSlice('\n')
		if len(buf)+len(chunk) > maxSSEEventBytes {
			return "", fmt.Errorf("sse line exceeds %d bytes", maxSSEEventBytes)
		}
		buf = append(buf, chunk...)
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(buf) > 0 {
			break
		}
		return "", err
	}
	buf = bytes.TrimSuffix(buf, []byte("\n"))
	buf = bytes.TrimSuffix(buf, []byte("\r"))
	return string(buf), nil
}

// ---------------------------------------------------------------------------
// Token estimation

// estimateTokens approximates the token count of s as ≈ runes/4.
func estimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

// estimateRequestTokens approximates the prompt size of req.
func estimateRequestTokens(req models.Request) int {
	total := estimateTokens(req.System)
	for _, m := range req.Messages {
		total += 4 + estimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			total += estimateTokens(tc.Name) + estimateTokens(string(tc.Arguments))
		}
	}
	for _, t := range req.Tools {
		total += estimateTokens(t.Name) + estimateTokens(t.Description) + estimateTokens(string(t.Parameters))
	}
	total += estimateTokens(string(req.JSONSchema))
	return total
}

// estimateUsage approximates usage for a request/response pair.
func estimateUsage(req models.Request, resp *models.Response) models.Usage {
	out := estimateTokens(resp.Content)
	for _, tc := range resp.ToolCalls {
		out += estimateTokens(tc.Name) + estimateTokens(string(tc.Arguments))
	}
	return models.Usage{InputTokens: estimateRequestTokens(req), OutputTokens: out, Estimated: true}
}

// ---------------------------------------------------------------------------
// Shared response helpers

// refusalMessage is the visible content of a response the model declined.
const refusalMessage = "The model declined to respond to this request."

// Finish reasons (see models.Response.FinishReason).
const (
	finishStop      = "stop"
	finishToolCalls = "tool_calls"
	finishLength    = "length"
	finishRefusal   = "refusal"
	finishError     = "error"
)

// normalizeArgs returns tool-call arguments as valid JSON. Empty input becomes
// {}; invalid JSON is preserved as a JSON string so downstream validation can
// report it instead of the response failing to serialize.
func normalizeArgs(raw []byte) json.RawMessage {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return json.RawMessage(`{}`)
	}
	if json.Valid(t) {
		return append(json.RawMessage(nil), t...)
	}
	b, _ := json.Marshal(string(t))
	return b
}

// objectArgs returns raw if it is a JSON object, otherwise {}. Used for
// providers that require tool-call inputs to be objects.
func objectArgs(raw json.RawMessage) json.RawMessage {
	t := bytes.TrimSpace(raw)
	if len(t) > 0 && t[0] == '{' && json.Valid(t) {
		return append(json.RawMessage(nil), t...)
	}
	return json.RawMessage(`{}`)
}

// jsonInstruction is the prompt used when native structured output is not
// available and JSON must be requested via instructions.
func jsonInstruction(schema json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, schema); err != nil {
		buf.Reset()
		buf.Write(schema)
	}
	return "Respond with a single JSON value only (no prose, no markdown code fences) that conforms to this JSON Schema:\n" + buf.String()
}

// joinNonEmpty joins the non-empty parts with a blank line.
func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

// extractJSON returns the JSON document contained in s: s itself when it is
// valid JSON, else the contents of a ``` fence, else the first balanced
// {...} / [...] that parses. When nothing parses, s is returned unchanged.
func extractJSON(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return s
	}
	if json.Valid([]byte(t)) {
		return t
	}
	if i := strings.Index(t, "```"); i >= 0 {
		rest := t[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 && !strings.ContainsAny(rest[:nl], "{[") {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			if cand := strings.TrimSpace(rest[:j]); json.Valid([]byte(cand)) {
				return cand
			}
		}
	}
	tries := 0
	for i := 0; i < len(t) && tries < 16; i++ {
		if t[i] != '{' && t[i] != '[' {
			continue
		}
		tries++
		if end := matchJSONEnd(t, i); end > 0 {
			if cand := t[i:end]; json.Valid([]byte(cand)) {
				return cand
			}
		}
	}
	return s
}

// matchJSONEnd returns the index just past the bracket matching s[start], or
// -1. It is string- and escape-aware.
func matchJSONEnd(s string, start int) int {
	depth := 0
	inStr, esc := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// thinkTags maps each inline reasoning open tag to its close tag.
var thinkTags = []struct{ open, close string }{
	{"<think>", "</think>"},
	{"<thinking>", "</thinking>"},
}

// thinkStripper removes inline reasoning blocks (<think>…</think> and
// <thinking>…</thinking>, anywhere in the text) that reasoning models such as
// Qwen or DeepSeek emit in content, so chain-of-thought is never surfaced.
//
// It works incrementally: text inside a block, and any trailing fragment that
// could be the start of an open tag, is withheld until it can be classified.
// Whitespace right after a removed block, and whitespace-only output before
// one, is dropped. An unterminated block is discarded at Flush. Applying it to
// a whole string or to any split of that string yields the same output.
type thinkStripper struct {
	pending   string // unclassified text (partial tag or block body)
	inside    bool   // within a reasoning block
	close     string // close tag of the current block
	trimLead  bool   // drop leading whitespace (just after a block)
	lead      string // whitespace-only output held back before any visible text
	shownText bool   // non-whitespace text has been emitted
}

// Write consumes a delta and returns the text that is safe to show.
func (t *thinkStripper) Write(delta string) string {
	t.pending += delta
	var out strings.Builder
	for {
		if t.inside {
			i := strings.Index(t.pending, t.close)
			if i < 0 {
				// Keep only a suffix that could begin the close tag.
				t.pending = t.pending[len(t.pending)-partialSuffix(t.pending, t.close):]
				break
			}
			t.pending = t.pending[i+len(t.close):]
			t.inside = false
			t.trimLead = true
			continue
		}
		at, tag := -1, -1
		for k, tg := range thinkTags {
			if i := strings.Index(t.pending, tg.open); i >= 0 && (at < 0 || i < at) {
				at, tag = i, k
			}
		}
		if at >= 0 {
			t.emit(&out, t.pending[:at])
			if !t.shownText {
				t.lead = "" // whitespace before a leading block is not content
			}
			t.pending = t.pending[at+len(thinkTags[tag].open):]
			t.inside, t.close = true, thinkTags[tag].close
			continue
		}
		hold := 0
		for _, tg := range thinkTags {
			hold = max(hold, partialSuffix(t.pending, tg.open))
		}
		t.emit(&out, t.pending[:len(t.pending)-hold])
		t.pending = t.pending[len(t.pending)-hold:]
		break
	}
	return out.String()
}

// emit appends visible text, applying the whitespace rules.
func (t *thinkStripper) emit(out *strings.Builder, s string) {
	if t.trimLead {
		s = strings.TrimLeft(s, " \t\r\n")
		if s == "" {
			return
		}
		t.trimLead = false
	}
	if s == "" {
		return
	}
	if !t.shownText {
		if strings.TrimLeft(s, " \t\r\n") == "" {
			t.lead += s
			return
		}
		s = t.lead + s
		t.lead = ""
		t.shownText = true
	}
	out.WriteString(s)
}

// Flush returns any withheld visible text at end of stream. An unterminated
// reasoning block is dropped entirely.
func (t *thinkStripper) Flush() string {
	var out strings.Builder
	if !t.inside {
		t.emit(&out, t.pending)
	}
	t.pending, t.inside = "", false
	if !t.shownText && t.lead != "" {
		out.WriteString(t.lead)
		t.lead = ""
	}
	return out.String()
}

// partialSuffix returns the length of the longest proper suffix of s that is
// a prefix of tag (i.e. text that might still become tag).
func partialSuffix(s, tag string) int {
	for n := min(len(tag)-1, len(s)); n > 0; n-- {
		if strings.HasSuffix(s, tag[:n]) {
			return n
		}
	}
	return 0
}

// stripThink applies thinkStripper to a complete string.
func stripThink(s string) string {
	var t thinkStripper
	return t.Write(s) + t.Flush()
}
