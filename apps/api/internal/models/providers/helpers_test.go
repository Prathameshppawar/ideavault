package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// fakeServer is an httptest server that records every request and answers
// with a scripted handler.
type fakeServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

// JSON decodes the recorded body into a generic map.
func (r recordedRequest) JSON(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("request body is not JSON: %v\n%s", err, r.Body)
	}
	return m
}

// newFakeServer starts a server whose handler receives the call index (0-based).
func newFakeServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, call int)) *fakeServer {
	t.Helper()
	fs := &fakeServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body)) // handlers may read it again
		fs.mu.Lock()
		call := len(fs.reqs)
		fs.reqs = append(fs.reqs, recordedRequest{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: body,
		})
		fs.mu.Unlock()
		handler(w, r, call)
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fakeServer) requests() []recordedRequest {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]recordedRequest(nil), fs.reqs...)
}

func (fs *fakeServer) last(t *testing.T) recordedRequest {
	t.Helper()
	reqs := fs.requests()
	if len(reqs) == 0 {
		t.Fatal("no requests recorded")
	}
	return reqs[len(reqs)-1]
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// writeSSE writes pre-formatted SSE frames, flushing after each one.
func writeSSE(w http.ResponseWriter, frames ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	for _, f := range frames {
		_, _ = io.WriteString(w, f)
		if fl != nil {
			fl.Flush()
		}
	}
}

// sseData formats a data-only SSE frame.
func sseData(data string) string { return "data: " + data + "\n\n" }

// sseEventFrame formats an SSE frame with an event name.
func sseEventFrame(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

// path walks a decoded JSON value by map keys and slice indexes.
func path(t *testing.T, v any, keys ...any) any {
	t.Helper()
	cur := v
	for _, k := range keys {
		switch key := k.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				t.Fatalf("path %v: expected object at %q, got %T", keys, key, cur)
			}
			cur = m[key]
		case int:
			s, ok := cur.([]any)
			if !ok || key >= len(s) {
				t.Fatalf("path %v: expected array with index %d, got %T (%v)", keys, key, cur, cur)
			}
			cur = s[key]
		}
	}
	return cur
}

// jsonEqual reports whether a and b are semantically equal JSON documents.
func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(x, y)
}

// assertSameResponse checks that two responses carry the same semantics
// (content, tool calls with semantically equal args, usage, finish reason,
// model, and replay presence / content).
func assertSameResponse(t *testing.T, a, b *models.Response) {
	t.Helper()
	if a.Content != b.Content {
		t.Errorf("content differs:\n  %q\n  %q", a.Content, b.Content)
	}
	if a.FinishReason != b.FinishReason {
		t.Errorf("finish reason differs: %q vs %q", a.FinishReason, b.FinishReason)
	}
	if a.Model != b.Model {
		t.Errorf("model differs: %q vs %q", a.Model, b.Model)
	}
	if a.Usage != b.Usage {
		t.Errorf("usage differs: %+v vs %+v", a.Usage, b.Usage)
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		t.Fatalf("tool call count differs: %d vs %d", len(a.ToolCalls), len(b.ToolCalls))
	}
	for i := range a.ToolCalls {
		x, y := a.ToolCalls[i], b.ToolCalls[i]
		if x.ID != y.ID || x.Name != y.Name || !jsonEqual(x.Arguments, y.Arguments) {
			t.Errorf("tool call %d differs: %+v vs %+v (%s vs %s)", i, x, y, x.Arguments, y.Arguments)
		}
	}
	if (a.Replay == nil) != (b.Replay == nil) {
		t.Fatalf("replay presence differs: %v vs %v", a.Replay != nil, b.Replay != nil)
	}
	if a.Replay != nil {
		if a.Replay.Provider != b.Replay.Provider || a.Replay.Model != b.Replay.Model || !jsonEqual(a.Replay.Content, b.Replay.Content) {
			t.Errorf("replay differs:\n  %s\n  %s", a.Replay.Content, b.Replay.Content)
		}
	}
}

// providerError asserts err is a *models.ProviderError and returns it.
func providerError(t *testing.T, err error) *models.ProviderError {
	t.Helper()
	pe, ok := err.(*models.ProviderError)
	if !ok {
		t.Fatalf("expected *models.ProviderError, got %T: %v", err, err)
	}
	return pe
}

// within fails the test if fn does not return within d.
func within(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("operation did not return within %v", d)
	}
}

// deltaRecorder collects stream deltas.
type deltaRecorder struct {
	mu     sync.Mutex
	deltas []string
}

func (d *deltaRecorder) handle(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deltas = append(d.deltas, s)
}

func (d *deltaRecorder) joined() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Join(d.deltas, "")
}

func (d *deltaRecorder) list() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.deltas...)
}

func ptrFloat(f float64) *float64 { return &f }

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("marshal: %v", err))
	}
	return string(b)
}
