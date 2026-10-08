package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

func TestSSEReader(t *testing.T) {
	long := strings.Repeat("x", 300<<10) // 300 KiB line, far beyond Scanner's 64 KiB default
	tests := []struct {
		name  string
		input string
		want  []sseEvent
	}{
		{
			name:  "basic data events",
			input: "data: {\"a\":1}\n\ndata: [DONE]\n\n",
			want:  []sseEvent{{Data: `{"a":1}`}, {Data: "[DONE]"}},
		},
		{
			name:  "CRLF line endings",
			input: "data: one\r\n\r\ndata: two\r\n\r\n",
			want:  []sseEvent{{Data: "one"}, {Data: "two"}},
		},
		{
			name:  "comments and keepalives are skipped",
			input: ": keepalive\n\n: another\ndata: x\n\n",
			want:  []sseEvent{{Data: "x"}},
		},
		{
			name:  "multi-line data joined with newline",
			input: "data: line1\ndata: line2\n\n",
			want:  []sseEvent{{Data: "line1\nline2"}},
		},
		{
			name:  "event and id fields",
			input: "event: message_start\nid: 7\ndata: {}\n\nevent: ping\ndata: {}\n\n",
			want:  []sseEvent{{Event: "message_start", ID: "7", Data: "{}"}, {Event: "ping", Data: "{}"}},
		},
		{
			name:  "no space after colon",
			input: "data:tight\n\n",
			want:  []sseEvent{{Data: "tight"}},
		},
		{
			name:  "trailing event without blank line",
			input: "data: a\n\ndata: tail",
			want:  []sseEvent{{Data: "a"}, {Data: "tail"}},
		},
		{
			name:  "very long line",
			input: "data: " + long + "\n\n",
			want:  []sseEvent{{Data: long}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newSSEReader(strings.NewReader(tc.input))
			var got []sseEvent
			for {
				ev, err := r.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("Next: %v", err)
				}
				got = append(got, ev)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d events, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("event %d = %+v, want %+v", i, truncate(got[i].Data, 40), truncate(tc.want[i].Data, 40))
				}
			}
		})
	}
}

func TestRedact(t *testing.T) {
	const secret = "sk-live-SUPERSECRETVALUE123"
	tests := []struct {
		name, in string
		leak     []string
	}{
		{"literal secret", "request failed for key " + secret, []string{secret}},
		{"authorization header", "Authorization: Bearer abc.def.ghi", []string{"abc.def.ghi"}},
		{"x-api-key header", `x-api-key: "sk-ant-api03-abcdefghijkl"`, []string{"sk-ant-api03-abcdefghijkl"}},
		{"x-goog-api-key header", "x-goog-api-key=AIzaSyA1234567890abcdefghijklmnopqrstu", []string{"AIzaSyA1234567890abcdefghijklmnopqrstu"}},
		{"key query param", `Post "https://host/v1/models/x:generateContent?key=AIzaSECRET&alt=sse": EOF`, []string{"AIzaSECRET"}},
		{"bare openai key", "invalid key sk-proj-abcdefghijklmnopqrstuvwxyz", []string{"sk-proj-abcdefghijklmnopqrstuvwxyz"}},
		{"bare groq key", "bad gsk_ABCDEFGHIJKLMNOPQRSTUV", []string{"gsk_ABCDEFGHIJKLMNOPQRSTUV"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := redact(tc.in, secret)
			for _, l := range tc.leak {
				if strings.Contains(out, l) {
					t.Errorf("redact(%q) = %q still contains %q", tc.in, out, l)
				}
			}
			if !strings.Contains(out, "[REDACTED]") {
				t.Errorf("redact(%q) = %q has no redaction marker", tc.in, out)
			}
		})
	}
	if got := redact("plain message", "abc"); got != "plain message" {
		t.Errorf("redact changed benign text: %q", got)
	}
}

func TestErrorMessageFromBody(t *testing.T) {
	tests := []struct{ body, want string }{
		{`{"error":{"message":"Rate limit reached","type":"rate_limit_error"}}`, "Rate limit reached"},
		{`{"error":"model not found"}`, "model not found"},
		{`{"message":"bad things"}`, "bad things"},
		{`[{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}]`, "Resource exhausted"},
		{`<html>502 Bad Gateway</html>`, "<html>502 Bad Gateway</html>"},
		{``, ""},
	}
	for _, tc := range tests {
		if got := errorMessageFromBody([]byte(tc.body)); got != tc.want {
			t.Errorf("errorMessageFromBody(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}

func TestRetryableStatus(t *testing.T) {
	for status, want := range map[int]bool{
		400: false, 401: false, 403: false, 404: false, 422: false,
		408: true, 409: true, 429: true, 500: true, 502: true, 503: true, 504: true, 529: true,
	} {
		if got := retryableStatus(status); got != want {
			t.Errorf("retryableStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestHTTPCallerStatusErrors(t *testing.T) {
	const key = "sk-test-SECRETSECRETSECRET"
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		switch call {
		case 0:
			writeJSON(w, 429, `{"error":{"message":"slow down"}}`)
		case 1:
			writeJSON(w, 400, `{"error":{"message":"Incorrect API key provided: `+key+`"}}`)
		default:
			writeJSON(w, 503, `upstream unavailable`)
		}
	})
	h := httpCaller{client: http.DefaultClient, provider: "test", secrets: []string{key}}
	ctx := context.Background()

	_, err := h.do(ctx, http.MethodGet, srv.URL, nil, nil)
	pe := providerError(t, err)
	if pe.StatusCode != 429 || !pe.Retryable || pe.Message != "slow down" {
		t.Errorf("429: got %+v", pe)
	}
	if !models.IsRetryable(err) {
		t.Error("IsRetryable(429) = false")
	}

	_, err = h.do(ctx, http.MethodGet, srv.URL, nil, nil)
	pe = providerError(t, err)
	if pe.StatusCode != 400 || pe.Retryable {
		t.Errorf("400: got %+v", pe)
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("error leaks API key: %v", err)
	}

	_, err = h.do(ctx, http.MethodGet, srv.URL, nil, nil)
	pe = providerError(t, err)
	if pe.StatusCode != 503 || !pe.Retryable || pe.Message != "upstream unavailable" {
		t.Errorf("503: got %+v", pe)
	}
}

func TestTransportErrorClassification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := transportError(ctx, "p", errors.New("boom"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx: got %v, want context.Canceled", err)
	}
	err = transportError(context.Background(), "p", io.ErrUnexpectedEOF)
	if pe := providerError(t, err); !pe.Retryable {
		t.Errorf("unexpected EOF should be retryable: %+v", pe)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"  [1,2]  ", `[1,2]`},
		{"```json\n{\"a\": \"b\"}\n```", `{"a": "b"}`},
		{"Here you go:\n```\n{\"x\":[1]}\n```\nThanks", `{"x":[1]}`},
		{`Sure! {"title":"A {curly} thing","n":2} hope that helps`, `{"title":"A {curly} thing","n":2}`},
		{`no json here`, `no json here`},
	}
	for _, tc := range tests {
		if got := extractJSON(tc.in); got != tc.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestThinkStripper(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"no think block", []string{"Hel", "lo"}, "Hello"},
		{"think block removed", []string{"<thi", "nk>secret plan", "</think>\n\n", "Answer"}, "Answer"},
		{"leading whitespace before think", []string{"\n<think>x</think> Done"}, "Done"},
		{"unterminated think dropped", []string{"<think>never closes"}, ""},
		{"angle bracket text is not think", []string{"<b>bold</b>"}, "<b>bold</b>"},
		{"short partial prefix flushed", []string{"<th"}, "<th"},
		{"thinking tag variant", []string{"<thinking>plan</thinking>Result"}, "Result"},
		{"block in the middle", []string{"Step one. <think>hmm</think> Step two."}, "Step one. Step two."},
		{"multiple blocks", []string{"<think>a</think>A <thinking>b</thinking>B<think>c</think>"}, "A B"},
		{"close tag split across chunks", []string{"<think>x</thi", "nk>ok"}, "ok"},
		{"whitespace-only output kept", []string{"  "}, "  "},
		{"near-miss tag kept", []string{"<thinker> is a word"}, "<thinker> is a word"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s thinkStripper
			var got strings.Builder
			for _, c := range tc.chunks {
				got.WriteString(s.Write(c))
			}
			got.WriteString(s.Flush())
			if got.String() != tc.want {
				t.Errorf("incremental = %q, want %q", got.String(), tc.want)
			}
			whole := strings.Join(tc.chunks, "")
			if out := stripThink(whole); out != tc.want {
				t.Errorf("whole = %q, want %q", out, tc.want)
			}
			// Any two-way split must give the same result as the whole string.
			for i := 0; i <= len(whole); i++ {
				var s thinkStripper
				out := s.Write(whole[:i]) + s.Write(whole[i:]) + s.Flush()
				if out != tc.want {
					t.Fatalf("split at %d: %q, want %q", i, out, tc.want)
				}
			}
		})
	}
}

func TestEstimateUsage(t *testing.T) {
	req := models.Request{System: "abcd", Messages: []models.Message{{Role: models.RoleUser, Content: "12345678"}}}
	u := estimateUsage(req, &models.Response{Content: "abcdefgh"})
	if !u.Estimated || u.InputTokens <= 0 || u.OutputTokens != 2 {
		t.Errorf("estimateUsage = %+v", u)
	}
}
