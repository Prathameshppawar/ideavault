// Package httpapi exposes IdeaVault's versioned REST API (/v1), SSE streams and
// the generated OpenAPI document.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/agent"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/config"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/observability"
	rl "github.com/Prathameshppawar/ideavault/apps/api/internal/repository/redis"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

// Version is the API build version (set via -ldflags).
var Version = "dev"

const sessionCookie = "iv_session"

// API holds handler dependencies.
type API struct {
	svc     *service.Service
	sup     *agent.Supervisor
	cfg     *config.Config
	log     *slog.Logger
	limiter rl.Limiter
	routes  []Route
	health  func(ctx context.Context) map[string]any
	notify  func()
}

// Options configures the API.
type Options struct {
	Service    *service.Service
	Supervisor *agent.Supervisor
	Config     *config.Config
	Logger     *slog.Logger
	Limiter    rl.Limiter
	// Health returns dependency status for /readyz and /v1/system/status.
	Health func(ctx context.Context) map[string]any
	// NotifyJobs wakes the background worker after enqueueing.
	NotifyJobs func()
}

// Route describes an endpoint for routing and OpenAPI generation.
type Route struct {
	Method  string
	Path    string
	Summary string
	Tag     string
	Auth    bool
	Req     any // request body type (nil = none)
	Resp    any // response type
	Query   []Param
	Stream  bool   // text/event-stream
	Raw     string // raw content type (downloads)
	Limit   string // rate-limit bucket: auth | agent | import | api
	Handler http.HandlerFunc
}

// Param is a documented query parameter.
type Param struct {
	Name, Type, Desc string
}

type ctxKey int

const userKey ctxKey = 1

// New builds the API handler.
func New(o Options) *API {
	return &API{svc: o.Service, sup: o.Supervisor, cfg: o.Config, log: o.Logger, limiter: o.Limiter, health: o.Health, notify: o.NotifyJobs}
}

// Routes returns the registered routes (for docs and tests).
func (a *API) Routes() []Route { return a.routes }

// Handler returns the root HTTP handler.
func (a *API) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(a.requestID, a.recoverer, a.securityHeaders, a.cors, a.logRequests)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "version": Version})
	})
	r.Get("/readyz", a.readyz)
	r.Get("/v1/openapi.json", a.openapi)
	r.Get("/docs", a.swaggerUI)
	a.registerAll()
	for _, rt := range a.routes {
		rt := rt
		h := rt.Handler
		if rt.Auth {
			h = a.requireAuth(a.csrf(h))
		}
		h = a.rateLimit(rt.Limit, h)
		r.Method(rt.Method, rt.Path, h)
	}
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, domain.NotFound("endpoint"))
	})
	return r
}

func (a *API) add(rt Route) {
	if rt.Limit == "" {
		rt.Limit = "api"
	}
	a.routes = append(a.routes, rt)
}

// ---------- middleware ----------

func (a *API) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(observability.WithRequestID(r.Context(), id)))
	})
}

func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				a.log.ErrorContext(r.Context(), "panic", "panic", fmt.Sprint(p), "path", r.URL.Path)
				writeError(w, r, errors.New("internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		if r.URL.Path == "/docs" {
			h.Set("Content-Security-Policy", "default-src 'none'; script-src https://cdn.jsdelivr.net 'unsafe-inline'; style-src https://cdn.jsdelivr.net 'unsafe-inline'; img-src data: https:; connect-src 'self'")
		} else {
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		}
		if a.cfg != nil && a.cfg.IsProduction() {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) originAllowed(origin string) bool {
	if origin == "" || a.cfg == nil {
		return false
	}
	for _, o := range a.cfg.WebOrigins {
		if strings.EqualFold(strings.TrimRight(origin, "/"), o) {
			return true
		}
	}
	return false
}

func (a *API) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if a.originAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", "X-Request-ID, Content-Disposition")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-IdeaVault-CSRF, X-Confirm, X-Request-ID")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = 200
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (a *API) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" {
			return
		}
		lvl := slog.LevelInfo
		if rec.status >= 500 {
			lvl = slog.LevelError
		}
		a.log.Log(r.Context(), lvl, "http request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(), "bytes", rec.bytes)
	})
}

// requireAuth resolves the session from the cookie or bearer token.
func (a *API) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		if tok == "" {
			if c, err := r.Cookie(sessionCookie); err == nil {
				tok = c.Value
			}
		}
		if tok == "" {
			writeError(w, r, domain.Unauthorized("sign in required"))
			return
		}
		u, _, err := a.svc.Authenticate(r.Context(), tok)
		if err != nil {
			writeError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = observability.WithUserID(ctx, u.ID.String())
		next(w, r.WithContext(ctx))
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// csrf protects cookie-authenticated unsafe requests: they must carry the custom
// X-IdeaVault-CSRF header (not settable cross-site without a CORS preflight we
// only grant to configured origins) and, when present, an allowed Origin.
func (a *API) csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || bearer(r) != "" {
			next(w, r)
			return
		}
		if r.Header.Get("X-IdeaVault-CSRF") != "1" {
			writeError(w, r, domain.Forbidden("missing CSRF header"))
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !a.originAllowed(o) && !sameHost(o, r) {
			writeError(w, r, domain.Forbidden("cross-site request blocked"))
			return
		}
		next(w, r)
	}
}

func sameHost(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = fh
	}
	return strings.EqualFold(u.Host, host)
}

func (a *API) rateLimit(bucket string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.limiter == nil || a.cfg == nil {
			next(w, r)
			return
		}
		limit := a.cfg.RateLimitAPIPerMin
		switch bucket {
		case "auth":
			limit = a.cfg.RateLimitAuthPerMin
		case "agent":
			limit = a.cfg.RateLimitAgentPerMin
		case "import":
			limit = a.cfg.RateLimitImportPerMin
		}
		if limit <= 0 {
			next(w, r)
			return
		}
		key := bucket + ":ip:" + clientIP(r)
		if c, err := r.Cookie(sessionCookie); err == nil && bucket != "auth" {
			key = bucket + ":s:" + shortHash(c.Value)
		} else if t := bearer(r); t != "" && bucket != "auth" {
			key = bucket + ":s:" + shortHash(t)
		}
		ok, remaining, reset := a.limiter.Allow(r.Context(), key, limit, time.Minute)
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(reset.Seconds())+1))
			writeError(w, r, domain.RateLimited("too many requests; slow down"))
			return
		}
		next(w, r)
	}
}

func shortHash(s string) string {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return strconv.FormatUint(h, 36)
}

// clientIP uses the left-most X-Forwarded-For entry only behind a proxy, otherwise RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			xff = xff[:i]
		}
		if ip := net.ParseIP(strings.TrimSpace(xff)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- helpers ----------

func userFrom(r *http.Request) *domain.User {
	u, _ := r.Context().Value(userKey).(*domain.User)
	return u
}

func uid(r *http.Request) uuid.UUID { return userFrom(r).ID }

func actor(r *http.Request) service.Actor { return service.UserActor(uid(r)) }

func pathID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, domain.Invalid(name, "must be a UUID")
	}
	return id, nil
}

func queryUUID(r *http.Request, name string) (*uuid.UUID, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, domain.Invalid(name, "must be a UUID")
	}
	return &id, nil
}

func queryInt(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func queryBool(r *http.Request, name string) bool {
	b, _ := strconv.ParseBool(r.URL.Query().Get(name))
	return b
}

const maxJSONBody = 4 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.Invalid("body", "request body is required")
		}
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return domain.Invalid("body", "request body too large")
		}
		return domain.Invalid("body", "invalid JSON: "+err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(withEmptySlices(v))
}

// ErrorBody is the JSON error envelope.
type ErrorBody struct {
	Error     string `json:"error"`
	Kind      string `json:"kind"`
	Field     string `json:"field,omitempty"`
	RequestID string `json:"request_id"`
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	body := ErrorBody{Error: "Something went wrong. Please try again.", Kind: "internal", RequestID: observability.RequestID(r.Context())}
	var de *domain.Error
	if errors.As(err, &de) {
		body.Error, body.Kind, body.Field = de.Message, string(de.Kind), de.Field
		switch de.Kind {
		case domain.KindNotFound:
			status = http.StatusNotFound
		case domain.KindInvalid:
			status = http.StatusBadRequest
		case domain.KindConflict, domain.KindImmutable:
			status = http.StatusConflict
		case domain.KindForbidden:
			status = http.StatusForbidden
		case domain.KindUnauthorized:
			status = http.StatusUnauthorized
		case domain.KindRateLimited:
			status = http.StatusTooManyRequests
		case domain.KindUnavailable:
			status = http.StatusServiceUnavailable
		}
	} else if errors.Is(err, context.Canceled) {
		status, body.Kind, body.Error = 499, "cancelled", "request cancelled"
	} else {
		slog.Default().ErrorContext(r.Context(), "request failed", "error", observability.RedactString(err.Error()), "path", r.URL.Path)
	}
	writeJSON(w, status, body)
}

func (a *API) setSessionCookie(w http.ResponseWriter, token string, exp time.Time) {
	c := &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: exp, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	if a.cfg != nil {
		c.Secure = a.cfg.CookieSecure
		c.Domain = a.cfg.CookieDomain
	}
	http.SetCookie(w, c)
}

func (a *API) clearSessionCookie(w http.ResponseWriter) {
	c := &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	if a.cfg != nil {
		c.Secure = a.cfg.CookieSecure
		c.Domain = a.cfg.CookieDomain
	}
	http.SetCookie(w, c)
}

func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	st := map[string]any{"ok": true}
	if a.health != nil {
		st = a.health(r.Context())
	}
	code := 200
	if ok, _ := st["ok"].(bool); !ok {
		code = 503
	}
	writeJSON(w, code, st)
}

// ---------- SSE ----------

type sseWriter struct {
	w      http.ResponseWriter
	f      http.Flusher
	mu     chan struct{}
	closed bool
}

func newSSE(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	s := &sseWriter{w: w, f: f, mu: make(chan struct{}, 1)}
	s.mu <- struct{}{}
	return s, true
}

func (s *sseWriter) send(event string, data any) {
	<-s.mu
	defer func() { s.mu <- struct{}{} }()
	if s.closed {
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		s.closed = true
		return
	}
	s.f.Flush()
}

func (s *sseWriter) comment(text string) {
	<-s.mu
	defer func() { s.mu <- struct{}{} }()
	if s.closed {
		return
	}
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		s.closed = true
		return
	}
	s.f.Flush()
}

// stream runs fn while emitting agent events as SSE with periodic heartbeats.
func (a *API) stream(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, emit agent.Emitter) error) {
	sse, ok := newSSE(w)
	if !ok {
		writeError(w, r, errors.New("streaming unsupported"))
		return
	}
	ctx := r.Context()
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				sse.comment("keep-alive")
			}
		}
	}()
	err := fn(ctx, func(ev agent.Event) { sse.send(ev.Type, ev.Data) })
	close(done)
	if err != nil {
		var de *domain.Error
		msg := "Something went wrong."
		if errors.As(err, &de) {
			msg = de.Message
		}
		sse.send("error", map[string]any{"message": msg, "request_id": observability.RequestID(ctx)})
	}
	sse.send("done", map[string]any{})
}
