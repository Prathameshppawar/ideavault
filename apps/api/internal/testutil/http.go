package testutil

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// Client is an HTTP client for the test server. Cookie sessions send the CSRF
// header on unsafe requests unless NoCSRF is set; Bearer clients send a token.
type Client struct {
	t      TB
	Base   string
	HTTP   *http.Client
	Bearer string
	NoCSRF bool
}

// Response is a fully read HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// String renders the response for failure messages.
func (r *Response) String() string {
	b := r.Body
	if len(b) > 600 {
		b = append(b[:600:600], "…"...)
	}
	return fmt.Sprintf("HTTP %d: %s", r.Status, b)
}

// Decode unmarshals the JSON body into v, failing the test on error.
func (r *Response) Decode(t TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode response (%s): %v", r.String(), err)
	}
}

// NewClient returns an anonymous client with a cookie jar.
func (e *Env) NewClient(t TB) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{t: t, Base: e.Server.URL, HTTP: &http.Client{Jar: jar, Timeout: 2 * time.Minute}}
}

// Login returns a cookie-session client for u (POST /v1/auth/login).
func (e *Env) Login(t TB, u *User) *Client {
	t.Helper()
	c := e.NewClient(t)
	res := c.Do("POST", "/v1/auth/login", map[string]string{"email": u.Email, "password": u.Password})
	if res.Status != http.StatusOK {
		t.Fatalf("login %s: %s", u.Email, res.String())
	}
	return c
}

// Do sends a request with an optional JSON body (or raw []byte / io.Reader body)
// and extra headers given as alternating key/value strings.
func (c *Client) Do(method, path string, body any, headers ...string) *Response {
	c.t.Helper()
	var rd io.Reader
	contentType := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case io.Reader:
		rd = b
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			c.t.Fatalf("encode body: %v", err)
		}
		rd = bytes.NewReader(raw)
		contentType = "application/json"
	}
	req, err := http.NewRequest(method, c.Base+path, rd)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	}
	if !c.NoCSRF && method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-IdeaVault-CSRF", "1")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body: %v", err)
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: data}
}

// JSON sends a request, asserts the status and decodes the body into out (if non-nil).
func (c *Client) JSON(method, path string, body any, wantStatus int, out any) *Response {
	c.t.Helper()
	res := c.Do(method, path, body)
	if res.Status != wantStatus {
		c.t.Fatalf("%s %s: want HTTP %d, got %s", method, path, wantStatus, res.String())
	}
	if out != nil {
		res.Decode(c.t, out)
	}
	return res
}

// Stream POSTs body to an SSE endpoint and returns the parsed events.
func (c *Client) Stream(path string, body any) []SSEEvent {
	c.t.Helper()
	res := c.Do("POST", path, body)
	if res.Status != http.StatusOK {
		c.t.Fatalf("POST %s: %s", path, res.String())
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		c.t.Fatalf("POST %s: content type %q is not text/event-stream", path, ct)
	}
	evs, err := ParseSSE(bytes.NewReader(res.Body))
	if err != nil {
		c.t.Fatalf("parse SSE: %v", err)
	}
	return evs
}

// SSEEvent is one server-sent event.
type SSEEvent struct {
	Event string
	Data  json.RawMessage
}

// Decode unmarshals the event data.
func (e SSEEvent) Decode(t TB, v any) {
	t.Helper()
	if err := json.Unmarshal(e.Data, v); err != nil {
		t.Fatalf("decode %s event %s: %v", e.Event, e.Data, err)
	}
}

// ParseSSE parses a text/event-stream body. Comment lines (": keep-alive") are ignored;
// multi-line data fields are joined with newlines per the SSE specification.
func ParseSSE(r io.Reader) ([]SSEEvent, error) {
	var out []SSEEvent
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	var ev string
	var data []string
	flush := func() {
		if ev == "" && len(data) == 0 {
			return
		}
		name := ev
		if name == "" {
			name = "message"
		}
		out = append(out, SSEEvent{Event: name, Data: json.RawMessage(strings.Join(data, "\n"))})
		ev, data = "", nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			ev = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
	return out, sc.Err()
}

// EventsNamed returns the events with the given name, in order.
func EventsNamed(evs []SSEEvent, name string) []SSEEvent {
	var out []SSEEvent
	for _, e := range evs {
		if e.Event == name {
			out = append(out, e)
		}
	}
	return out
}

// EventNames lists event names in order (for failure messages).
func EventNames(evs []SSEEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Event
	}
	return out
}
