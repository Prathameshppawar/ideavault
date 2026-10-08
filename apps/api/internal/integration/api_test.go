package integration

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/testutil"
)

func TestAPIAuthFlow(t *testing.T) {
	e := requireEnv(t)
	e.TruncateAll(t) // first-run setup needs an empty vault
	c := e.NewClient(t)
	var st struct {
		SetupRequired bool `json:"setup_required"`
		Authenticated bool `json:"authenticated"`
	}
	c.JSON("GET", "/v1/auth/status", nil, 200, &st)
	if !st.SetupRequired || st.Authenticated {
		t.Fatalf("fresh vault status: %+v", st)
	}
	bad := []map[string]string{
		{"email": "not-an-email", "password": "long enough password"},
		{"email": "owner@ideavault.test", "password": "short"},
	}
	for _, b := range bad {
		if res := c.Do("POST", "/v1/auth/setup", b); res.Status != 400 {
			t.Errorf("setup with %v: %s", b, res)
		}
	}
	res := c.Do("POST", "/v1/auth/setup", map[string]string{"email": " Owner@IdeaVault.test ", "password": "correct horse battery", "display_name": "Owner"})
	if res.Status != 201 {
		t.Fatalf("setup: %s", res)
	}
	cookie := res.Header.Get("Set-Cookie")
	for _, want := range []string{"iv_session=ivs_", "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("session cookie %q missing %s", cookie, want)
		}
	}
	var me domain.User
	c.JSON("GET", "/v1/auth/me", nil, 200, &me)
	if me.Email != "owner@ideavault.test" || me.DisplayName != "Owner" {
		t.Errorf("me = %+v", me)
	}
	c.JSON("GET", "/v1/auth/status", nil, 200, &st)
	if st.SetupRequired || !st.Authenticated {
		t.Errorf("status after setup: %+v", st)
	}
	// A second setup is forbidden (single-owner vault).
	if res := e.NewClient(t).Do("POST", "/v1/auth/setup", map[string]string{"email": "intruder@ideavault.test", "password": "another long password"}); res.Status != 403 {
		t.Errorf("second setup must be forbidden: %s", res)
	}
	// Login.
	other := e.NewClient(t)
	if res := other.Do("POST", "/v1/auth/login", map[string]string{"email": "owner@ideavault.test", "password": "wrong password!"}); res.Status != 401 {
		t.Errorf("wrong password: %s", res)
	}
	if res := other.Do("POST", "/v1/auth/login", map[string]string{"email": "nobody@ideavault.test", "password": "correct horse battery"}); res.Status != 401 ||
		!strings.Contains(string(res.Body), "invalid email or password") {
		t.Errorf("unknown user must look like a wrong password: %s", res)
	}
	if res := other.Do("POST", "/v1/auth/login", map[string]string{"email": "OWNER@ideavault.test", "password": "correct horse battery"}); res.Status != 200 {
		t.Fatalf("login: %s", res)
	}
	other.JSON("GET", "/v1/auth/me", nil, 200, nil)
	var sessions []domain.Session
	other.JSON("GET", "/v1/auth/sessions", nil, 200, &sessions)
	if len(sessions) != 2 {
		t.Errorf("sessions = %d", len(sessions))
	}
	// Password change requires the current password.
	if res := other.Do("POST", "/v1/auth/password", map[string]string{"current": "nope nope nope", "next": "brand new password"}); res.Status != 401 {
		t.Errorf("password change with wrong current: %s", res)
	}
	other.JSON("POST", "/v1/auth/password", map[string]string{"current": "correct horse battery", "next": "brand new password"}, 200, nil)
	// Logout revokes the session server-side.
	cookieVal := ""
	for _, ck := range other.HTTP.Jar.Cookies(mustURL(t, e.Server.URL)) {
		if ck.Name == "iv_session" {
			cookieVal = ck.Value
		}
	}
	other.JSON("POST", "/v1/auth/logout", nil, 200, nil)
	if res := other.Do("GET", "/v1/auth/me", nil); res.Status != 401 {
		t.Errorf("after logout: %s", res)
	}
	replay := e.NewClient(t)
	if res := replay.Do("GET", "/v1/auth/me", nil, "Cookie", "iv_session="+cookieVal); res.Status != 401 {
		t.Errorf("a logged-out session token must be revoked, got %s", res)
	}
	if res := e.NewClient(t).Do("POST", "/v1/auth/login", map[string]string{"email": "owner@ideavault.test", "password": "brand new password"}); res.Status != 200 {
		t.Errorf("login with the new password: %s", res)
	}
}

func TestAPIAuthenticationRequired(t *testing.T) {
	e := requireEnv(t)
	anon := e.NewClient(t)
	for _, path := range []string{"/v1/ideas", "/v1/auth/me", "/v1/dashboard/today", "/v1/knowledge", "/v1/agent/pending"} {
		res := anon.Do("GET", path, nil)
		var body struct {
			Kind      string `json:"kind"`
			RequestID string `json:"request_id"`
		}
		res.Decode(t, &body)
		if res.Status != 401 || body.Kind != "unauthorized" || body.RequestID == "" {
			t.Errorf("GET %s without auth: %s", path, res)
		}
	}
	for _, hdr := range [][]string{{"Authorization", "Bearer ivt_forged"}, {"Authorization", "Bearer garbage"}, {"Cookie", "iv_session=ivs_forged"}} {
		if res := anon.Do("GET", "/v1/ideas", nil, hdr...); res.Status != 401 {
			t.Errorf("forged credentials %v: %s", hdr, res)
		}
	}
	if res := anon.Do("POST", "/v1/ideas", map[string]string{"title": "x"}); res.Status != 401 {
		t.Errorf("anonymous write: %s", res)
	}
	if res := anon.Do("GET", "/v1/nope", nil); res.Status != 404 {
		t.Errorf("unknown endpoint: %s", res)
	}
}

func TestAPICSRFAndBearerTokens(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	c.NoCSRF = true
	res := c.Do("POST", "/v1/ideas", map[string]string{"title": "CSRF idea"})
	if res.Status != 403 || !strings.Contains(string(res.Body), "CSRF") {
		t.Fatalf("cookie POST without CSRF header must be rejected: %s", res)
	}
	if res := c.Do("POST", "/v1/ideas", map[string]string{"title": "CSRF idea"}, "X-IdeaVault-CSRF", "1", "Origin", "https://evil.example"); res.Status != 403 {
		t.Errorf("cross-site origin must be rejected: %s", res)
	}
	if res := c.Do("POST", "/v1/ideas", map[string]string{"title": "CSRF idea"}, "X-IdeaVault-CSRF", "1", "Origin", "http://localhost:3000"); res.Status != 201 {
		t.Errorf("allowed web origin: %s", res)
	}
	c.NoCSRF = false
	var tok struct {
		Token string `json:"token"`
	}
	c.JSON("POST", "/v1/auth/tokens", map[string]string{"label": "cli"}, 201, &tok)
	if !strings.HasPrefix(tok.Token, "ivt_") {
		t.Fatalf("token = %q", tok.Token)
	}
	bearer := e.NewClient(t)
	bearer.Bearer, bearer.NoCSRF = tok.Token, true
	var created service.IdeaCreated
	bearer.JSON("POST", "/v1/ideas", map[string]string{"title": "Bearer idea"}, 201, &created)
	if created.Branch.Name != "Main" {
		t.Errorf("bearer create: %+v", created)
	}
	var sessions []domain.Session
	c.JSON("GET", "/v1/auth/sessions", nil, 200, &sessions)
	var tokenID uuid.UUID
	for _, s := range sessions {
		if s.Kind == "api_token" {
			tokenID = s.ID
		}
	}
	c.JSON("DELETE", "/v1/auth/sessions/"+tokenID.String(), nil, 200, nil)
	if res := bearer.Do("GET", "/v1/ideas", nil); res.Status != 401 {
		t.Errorf("revoked token must stop working: %s", res)
	}
}

func TestAPIDestructiveRequiresConfirmation(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	var created service.IdeaCreated
	c.JSON("POST", "/v1/ideas", map[string]string{"title": "Delete me", "origin_text": "temp"}, 201, &created)
	var art service.ArtifactResult
	c.JSON("POST", "/v1/artifacts", map[string]any{"idea_id": created.Idea.ID, "type": "CUSTOM", "title": "Doc", "content_markdown": "# Doc"}, 201, &art)
	for _, path := range []string{"/v1/artifacts/" + art.Artifact.ID.String(), "/v1/ideas/" + created.Idea.ID.String()} {
		res := c.Do("DELETE", path, nil)
		if res.Status != 400 || !strings.Contains(string(res.Body), "X-Confirm") {
			t.Errorf("DELETE %s without X-Confirm: %s", path, res)
		}
		if res := c.Do("DELETE", path, nil, "X-Confirm", "yes"); res.Status != 400 {
			t.Errorf("X-Confirm must be exactly 'delete': %s", res)
		}
	}
	c.JSON("GET", "/v1/ideas/"+created.Idea.ID.String(), nil, 200, nil)
	if res := c.Do("DELETE", "/v1/artifacts/"+art.Artifact.ID.String(), nil, "X-Confirm", "delete"); res.Status != 200 {
		t.Fatalf("confirmed artifact delete: %s", res)
	}
	if res := c.Do("DELETE", "/v1/ideas/"+created.Idea.ID.String(), nil, "X-Confirm", "delete"); res.Status != 200 {
		t.Fatalf("confirmed idea delete: %s", res)
	}
	if res := c.Do("GET", "/v1/ideas/"+created.Idea.ID.String(), nil); res.Status != 404 {
		t.Errorf("deleted idea: %s", res)
	}
}

func TestAPIUserIsolation(t *testing.T) {
	e := requireEnv(t)
	alice, bob := e.NewUser(t), e.NewUser(t)
	idea := e.MustIdea(t, alice, "Alice private idea", "secret")
	d1 := e.MustRecord(t, alice, decision(idea.Idea.ID, "Alice decision"))
	b := e.Login(t, bob)
	for _, path := range []string{"/v1/ideas/" + idea.Idea.ID.String(), "/v1/branches/" + idea.Branch.ID.String(), "/v1/knowledge/" + d1.ID.String(),
		"/v1/ideas/" + idea.Idea.ID.String() + "/journey"} {
		if res := b.Do("GET", path, nil); res.Status != 404 {
			t.Errorf("bob GET %s: %s", path, res)
		}
	}
	if res := b.Do("PATCH", "/v1/ideas/"+idea.Idea.ID.String(), map[string]string{"title": "pwned"}); res.Status != 404 {
		t.Errorf("bob PATCH: %s", res)
	}
	if res := b.Do("DELETE", "/v1/ideas/"+idea.Idea.ID.String(), nil, "X-Confirm", "delete"); res.Status != 404 {
		t.Errorf("bob DELETE: %s", res)
	}
	var list struct {
		Ideas []domain.Idea `json:"ideas"`
		Total int           `json:"total"`
	}
	b.JSON("GET", "/v1/ideas", nil, 200, &list)
	if list.Total != 0 || list.Ideas == nil {
		t.Errorf("bob sees %d ideas (ideas=%v)", list.Total, list.Ideas)
	}
	var sr service.SearchResponse
	b.JSON("GET", "/v1/search?q=secret", nil, 200, &sr)
	if len(sr.Results) != 0 {
		t.Errorf("search leaked %d results", len(sr.Results))
	}
	if res := b.Do("PATCH", "/v1/knowledge/"+d1.ID.String()+"/status", map[string]string{"status": "REJECTED"}); res.Status != 404 {
		t.Errorf("bob status change: %s", res)
	}
}

func TestAPIRateLimiting(t *testing.T) {
	requireEnv(t)
	cfg := testutil.Config(env.URL)
	cfg.RateLimitAPIPerMin, cfg.RateLimitAuthPerMin = 3, 2
	a := try(testutil.NewApp(ctx(), cfg)).must(t)
	defer a.Close()
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	get := func(path string) *http.Response {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	var codes []int
	for i := 0; i < 4; i++ {
		codes = append(codes, get("/v1/auth/status").StatusCode)
	}
	if fmt.Sprint(codes) != "[200 200 200 429]" {
		t.Errorf("api bucket codes = %v", codes)
	}
	last := get("/v1/auth/status")
	if last.StatusCode != 429 || last.Header.Get("Retry-After") == "" || last.Header.Get("X-RateLimit-Limit") != "3" {
		t.Errorf("429 headers: %v", last.Header)
	}
	login := func() int {
		resp, err := http.Post(srv.URL+"/v1/auth/login", "application/json", strings.NewReader(`{"email":"x@ideavault.test","password":"whatever123"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	codes = []int{login(), login(), login()}
	if fmt.Sprint(codes) != "[401 401 429]" {
		t.Errorf("auth bucket codes = %v (brute-force protection)", codes)
	}
	if get("/healthz").StatusCode != 200 {
		t.Error("health checks are not rate limited")
	}
}

func TestAPISecurityHeadersCORSAndHealth(t *testing.T) {
	e := requireEnv(t)
	c := e.NewClient(t)
	res := c.Do("GET", "/v1/auth/status", nil, "X-Request-ID", "req-abc", "Origin", "http://localhost:3000")
	h := res.Header
	for k, v := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'", "Cross-Origin-Resource-Policy": "same-site", "Cache-Control": "no-store",
		"X-Request-Id": "req-abc", "Access-Control-Allow-Origin": "http://localhost:3000", "Access-Control-Allow-Credentials": "true"} {
		if got := h.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	if h.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS is production-only")
	}
	evil := c.Do("GET", "/v1/auth/status", nil, "Origin", "https://evil.example")
	if evil.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS must not allow arbitrary origins")
	}
	if gen := c.Do("GET", "/healthz", nil).Header.Get("X-Request-Id"); len(gen) != 36 {
		t.Errorf("request ids are generated: %q", gen)
	}
	pre := c.Do("OPTIONS", "/v1/ideas", nil, "Origin", "http://localhost:3000", "Access-Control-Request-Method", "POST")
	if pre.Status != 204 || !strings.Contains(pre.Header.Get("Access-Control-Allow-Headers"), "X-IdeaVault-CSRF") || !strings.Contains(pre.Header.Get("Access-Control-Allow-Headers"), "X-Confirm") {
		t.Errorf("preflight: %d %v", pre.Status, pre.Header)
	}
	var health map[string]any
	c.JSON("GET", "/readyz", nil, 200, &health)
	if health["postgres"] != "up" || health["ok"] != true || health["ai"] != "offline planner only" {
		t.Errorf("readyz = %v", health)
	}
	docs := c.Do("GET", "/docs", nil)
	if docs.Status != 200 || !strings.Contains(docs.Header.Get("Content-Security-Policy"), "cdn.jsdelivr.net") || !strings.Contains(string(docs.Body), "swagger-ui") {
		t.Errorf("docs: %d %v", docs.Status, docs.Header.Get("Content-Security-Policy"))
	}
}

func TestAPIOpenAPIServedAndComplete(t *testing.T) {
	e := requireEnv(t)
	var doc map[string]any
	e.NewClient(t).JSON("GET", "/v1/openapi.json", nil, 200, &doc)
	paths := doc["paths"].(map[string]any)
	routes := e.App.API.Routes()
	if len(routes) < 80 {
		t.Fatalf("only %d routes", len(routes))
	}
	for _, rt := range routes {
		ops, ok := paths[rt.Path].(map[string]any)
		if !ok || ops[strings.ToLower(rt.Method)] == nil {
			t.Errorf("%s %s is not documented", rt.Method, rt.Path)
		}
	}
	// Every documented operation is actually routed (no 404/405 from the router).
	c := e.Login(t, e.NewUser(t))
	for p, ops := range paths {
		for m := range ops.(map[string]any) {
			path := regexp.MustCompile(`\{[a-z_]+\}`).ReplaceAllString(p, uuid.NewString())
			var res *testutil.Response
			if strings.ToUpper(m) == "GET" {
				res = c.Do("GET", path, nil)
			} else {
				res = c.Do(strings.ToUpper(m), path, []byte(`{`), "Content-Type", "application/json")
			}
			if res.Status == 405 || (res.Status == 404 && strings.Contains(string(res.Body), "endpoint not found")) {
				t.Errorf("documented %s %s is not routed: %s", strings.ToUpper(m), p, res)
			}
			if res.Status >= 500 {
				t.Errorf("%s %s with a random id/bad body crashed: %s", strings.ToUpper(m), p, res)
			}
		}
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestAPIArtifactDownloadAndContextPackExport(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	tl := seedTimeline(t, e, u, "Download Idea")
	var gen service.ArtifactResult
	c.JSON("POST", "/v1/artifacts/generate", map[string]any{"idea_id": tl.idea.Idea.ID, "type": "ACTION_PLAN"}, 201, &gen)
	id := gen.Artifact.ID.String()
	dl := c.Do("GET", "/v1/artifacts/"+id+"/download", nil)
	if dl.Status != 200 || !strings.HasPrefix(dl.Header.Get("Content-Type"), "text/markdown") ||
		dl.Header.Get("Content-Disposition") != `attachment; filename="Download-Idea-Action-Plan.md"` || !bytes.HasPrefix(dl.Body, []byte("# ")) {
		t.Fatalf("download: %d %v %q", dl.Status, dl.Header, firstLineOf(string(dl.Body)))
	}
	var upd service.ArtifactResult
	c.JSON("PATCH", "/v1/artifacts/"+id, map[string]any{"content_markdown": "# Edited plan\n\n- [ ] Step [D2]"}, 200, &upd)
	if upd.Artifact.CurrentVersion != 2 {
		t.Fatalf("PATCH must create v2: %+v", upd.Artifact)
	}
	v1 := c.Do("GET", "/v1/artifacts/"+id+"/download?version=1", nil)
	if !bytes.Equal(v1.Body, []byte(gen.Artifact.ContentMarkdown)) || !strings.Contains(v1.Header.Get("Content-Disposition"), "v1.md") {
		t.Errorf("v1 download must return the original content: %q", v1.Header.Get("Content-Disposition"))
	}
	cur := c.Do("GET", "/v1/artifacts/"+id+"/download", nil)
	if !strings.HasPrefix(string(cur.Body), "# Edited plan") {
		t.Errorf("current download: %q", cur.Body)
	}
	if res := c.Do("GET", "/v1/artifacts/"+id+"/download?version=9", nil); res.Status != 404 {
		t.Errorf("missing version: %s", res)
	}
	var versions []domain.ArtifactVersion
	c.JSON("GET", "/v1/artifacts/"+id+"/versions", nil, 200, &versions)
	var prov []domain.ProvenanceEntry
	c.JSON("GET", "/v1/artifacts/"+id+"/provenance?version=1", nil, 200, &prov)
	if len(versions) != 2 || len(prov) == 0 {
		t.Errorf("versions=%d provenance=%d", len(versions), len(prov))
	}
	var pack domain.ContextPack
	c.JSON("POST", "/v1/context-packs", map[string]any{"idea_id": tl.idea.Idea.ID, "objective": "Pricing"}, 201, &pack)
	md := c.Do("GET", "/v1/context-packs/"+pack.ID.String()+"/export", nil)
	js := c.Do("GET", "/v1/context-packs/"+pack.ID.String()+"/export?format=json", nil)
	if md.Status != 200 || !strings.HasPrefix(string(md.Body), "# IdeaVault Context Pack: Download Idea") || !strings.HasSuffix(md.Header.Get("Content-Disposition"), `.md"`) {
		t.Errorf("md export: %d %q", md.Status, md.Header.Get("Content-Disposition"))
	}
	var pj map[string]any
	js.Decode(t, &pj)
	if pj["format"] != "ideavault.context_pack.v1" || !strings.HasSuffix(js.Header.Get("Content-Disposition"), `.json"`) {
		t.Errorf("json export: %v", js.Header)
	}
}

func TestAPIImportUploadFlow(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "conversations.json")
	_, _ = fw.Write(testutil.Fixture(t, "imports", "chatgpt", "conversations.json"))
	_ = mw.Close()
	res := c.Do("POST", "/v1/imports", buf.Bytes(), "Content-Type", mw.FormDataContentType())
	if res.Status != 202 {
		t.Fatalf("upload: %s", res)
	}
	var im domain.Import
	res.Decode(t, &im)
	e.DrainJobs(t)
	var d service.ImportDetail
	c.JSON("GET", "/v1/imports/"+im.ID.String(), nil, 200, &d)
	if d.Import.Status != domain.ImportPreview || len(d.Items) != 2 || d.Items[0].Payload != nil {
		t.Fatalf("preview over HTTP: %+v (payloads must not be sent)", d.Import)
	}
	c.JSON("POST", "/v1/imports/"+im.ID.String()+"/commit", map[string]any{"extract": true,
		"items": []map[string]any{{"item_id": d.Items[0].ID, "include": true, "new_idea": true}, {"item_id": d.Items[1].ID, "include": false}}}, 202, nil)
	e.DrainJobs(t)
	c.JSON("GET", "/v1/imports/"+im.ID.String(), nil, 200, &d)
	if d.Import.Status != domain.ImportCompleted {
		t.Errorf("commit over HTTP: %+v", d.Import)
	}
	var sync domain.Import
	c.JSON("POST", "/v1/imports", map[string]any{"source_kind": "paste", "text": "User: Should riders rate trips?\n\nAssistant: Yes, keep it to three questions.", "sync": true, "new_idea": true}, 201, &sync)
	if sync.Status != domain.ImportCompleted {
		t.Errorf("sync paste import: %+v", sync)
	}
	if res := c.Do("POST", "/v1/imports", map[string]any{"url": "https://chatgpt.com/c/abc", "sync": true}); res.Status != 400 || !strings.Contains(string(res.Body), "private conversation") {
		t.Errorf("private link over HTTP: %s", res)
	}
	var list []domain.Import
	c.JSON("GET", "/v1/imports", nil, 200, &list)
	if len(list) != 3 {
		t.Errorf("imports listed = %d", len(list))
	}
}

func TestAPICoreResourceRoundTrip(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	var created service.IdeaCreated
	c.JSON("POST", "/v1/ideas", map[string]any{"title": "HTTP Idea", "origin_text": "riders", "tags": []string{"Mobility"}}, 201, &created)
	ideaID := created.Idea.ID.String()
	var d1 domain.KnowledgeItem
	c.JSON("POST", "/v1/knowledge", map[string]any{"idea_id": ideaID, "kind": "decision", "statement": "No marketplace", "rationale": "moderation"}, 201, &d1)
	var q1 domain.KnowledgeItem
	c.JSON("POST", "/v1/knowledge", map[string]any{"idea_id": ideaID, "kind": "question", "statement": "Public trips?"}, 201, &q1)
	if res := c.Do("POST", "/v1/knowledge", map[string]any{"idea_id": ideaID, "kind": "rumour", "statement": "x"}); res.Status != 400 {
		t.Errorf("invalid kind: %s", res)
	}
	if res := c.Do("POST", "/v1/knowledge", []byte(`{"idea_id": `), "Content-Type", "application/json"); res.Status != 400 {
		t.Errorf("malformed JSON: %s", res)
	}
	var cp domain.Checkpoint
	c.JSON("POST", "/v1/ideas/"+ideaID+"/checkpoints", map[string]any{"title": "HTTP CP", "kind": "fork_base"}, 201, &cp)
	if cp.Kind != domain.CheckpointManual {
		t.Errorf("clients may only create manual checkpoints, got %s", cp.Kind)
	}
	var d2 domain.KnowledgeItem
	c.JSON("POST", "/v1/knowledge", map[string]any{"idea_id": ideaID, "kind": "decision", "statement": "Marketplace later", "supersedes": d1.ID}, 201, &d2)
	var ex service.KnowledgeExplanation
	c.JSON("GET", "/v1/knowledge/"+d1.ID.String(), nil, 200, &ex)
	if ex.Item.Status != "SUPERSEDED" || len(ex.Chain) != 2 || len(ex.Checkpoints) != 1 {
		t.Errorf("explain over HTTP: %+v", ex.Item)
	}
	var items struct {
		Items []domain.KnowledgeItem `json:"items"`
	}
	c.JSON("GET", "/v1/knowledge?idea_id="+ideaID+"&kind=decision", nil, 200, &items)
	if len(items.Items) != 1 || items.Items[0].ID != d2.ID {
		t.Errorf("live decisions: %+v", items.Items)
	}
	c.JSON("GET", "/v1/knowledge?idea_id="+ideaID+"&kind=decision&include_history=true", nil, 200, &items)
	if len(items.Items) != 2 {
		t.Errorf("with history: %d", len(items.Items))
	}
	c.JSON("PATCH", "/v1/knowledge/"+q1.ID.String()+"/status", map[string]any{"status": "ANSWERED", "answer": "Private"}, 200, nil)
	var fork service.ForkResult
	c.JSON("POST", "/v1/checkpoints/"+cp.ID.String()+"/fork", map[string]any{"name": "HTTP fork"}, 201, &fork)
	var cmp service.BranchComparison
	c.JSON("GET", fmt.Sprintf("/v1/branches/compare?a=%s&b=%s", created.Branch.ID, fork.Branch.ID), nil, 200, &cmp)
	if cmp.CommonAncestor == nil || cmp.CommonAncestor.Label != cp.Label {
		t.Errorf("compare over HTTP: %+v", cmp.CommonAncestor)
	}
	if res := c.Do("GET", "/v1/branches/compare?a=bad", nil); res.Status != 400 {
		t.Errorf("compare validation: %s", res)
	}
	var ov service.IdeaOverview
	c.JSON("GET", "/v1/ideas/"+ideaID+"?branch_id="+fork.Branch.ID.String(), nil, 200, &ov)
	if ov.Branch.ID != fork.Branch.ID || len(ov.Branches) != 2 || len(ov.Knowledge["decision"]) != 1 || ov.Knowledge["decision"][0].Label != "D1" {
		t.Errorf("overview of the fork (forked before D2): %+v", ov.Knowledge["decision"])
	}
	var concl service.ConclusionResult
	c.JSON("POST", "/v1/ideas/"+ideaID+"/conclude", map[string]any{"outcome": "PARK", "note": "later"}, 200, &concl)
	if concl.Idea.Status != domain.StatusParked {
		t.Errorf("conclude over HTTP: %s", concl.Idea.Status)
	}
	if res := c.Do("GET", "/v1/ideas/not-a-uuid", nil); res.Status != 400 {
		t.Errorf("bad uuid path: %s", res)
	}
}

// ---------- JSON contract: no null where the OpenAPI schema promises an array ----------

type nullChecker struct {
	schemas  map[string]any
	problems []string
}

func (n *nullChecker) resolve(s map[string]any) map[string]any {
	for i := 0; i < 10; i++ {
		ref, ok := s["$ref"].(string)
		if !ok {
			return s
		}
		next, _ := n.schemas[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
		if next == nil {
			return s
		}
		s = next
	}
	return s
}

func (n *nullChecker) check(schema map[string]any, v any, path string) {
	s := n.resolve(schema)
	if all, ok := s["allOf"].([]any); ok {
		for _, part := range all {
			if m, ok := part.(map[string]any); ok {
				n.check(m, v, path)
			}
		}
		return
	}
	switch s["type"] {
	case "array":
		if v == nil {
			n.problems = append(n.problems, path)
			return
		}
		arr, _ := v.([]any)
		items, _ := s["items"].(map[string]any)
		for i, el := range arr {
			if items != nil && i < 3 {
				n.check(items, el, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return
		}
		if props, ok := s["properties"].(map[string]any); ok {
			for k, ps := range props {
				if pv, present := obj[k]; present {
					if pm, ok := ps.(map[string]any); ok {
						n.check(pm, pv, path+"."+k)
					}
				}
			}
		}
		if ap, ok := s["additionalProperties"].(map[string]any); ok {
			for k, pv := range obj {
				n.check(ap, pv, path+"."+k)
			}
		}
	}
}

func TestAPIListsAreArraysNotNull(t *testing.T) {
	e := requireEnv(t)
	var doc map[string]any
	e.NewClient(t).JSON("GET", "/v1/openapi.json", nil, 200, &doc)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	paths := doc["paths"].(map[string]any)

	run := func(t *testing.T, c *testutil.Client, ids map[string]string) {
		nc := &nullChecker{schemas: schemas}
		for _, rt := range e.App.API.Routes() {
			if rt.Method != "GET" || rt.Stream || rt.Raw != "" || rt.Resp == nil || strings.Contains(rt.Path, "/available") {
				continue
			}
			path, ok := fillPath(rt.Path, ids)
			if !ok {
				continue
			}
			res := c.Do("GET", path, nil)
			if res.Status != 200 {
				t.Errorf("GET %s: %s", path, res)
				continue
			}
			var v any
			res.Decode(t, &v)
			op := paths[rt.Path].(map[string]any)["get"].(map[string]any)
			schema := op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
			nc.check(schema, v, rt.Path)
		}
		if len(nc.problems) > 0 {
			t.Errorf("null returned where the OpenAPI contract promises an array (%d):\n  %s", len(nc.problems), strings.Join(nc.problems, "\n  "))
		}
	}
	t.Run("empty vault", func(t *testing.T) {
		u := e.NewUser(t)
		run(t, e.Login(t, u), map[string]string{"q": "nothing"})
	})
	t.Run("seeded vault", func(t *testing.T) {
		u := e.NewUser(t)
		c := e.Login(t, u)
		ids := seedHTTPVault(t, e, u, c)
		run(t, c, ids)
	})
}

// fillPath substitutes path parameters by resource kind and adds required query params.
func fillPath(p string, ids map[string]string) (string, bool) {
	prefixes := []struct{ prefix, key string }{
		{"/v1/ideas/{id}", "idea"}, {"/v1/branches/{id}", "branch"}, {"/v1/checkpoints/{id}", "checkpoint"}, {"/v1/knowledge/{id}", "knowledge"},
		{"/v1/conversations/{id}", "conversation"}, {"/v1/artifacts/{id}", "artifact"}, {"/v1/context-packs/{id}", "pack"}, {"/v1/deltas/{id}", "delta"},
		{"/v1/imports/{id}", "import"}, {"/v1/agent/runs/{id}", "run"}, {"/v1/models/lab/{id}", "lab"},
	}
	for _, pr := range prefixes {
		if strings.HasPrefix(p, pr.prefix) {
			id, ok := ids[pr.key]
			if !ok {
				return "", false
			}
			p = strings.Replace(p, "{id}", id, 1)
		}
	}
	if strings.Contains(p, "{") {
		return "", false
	}
	switch p {
	case "/v1/branches/compare":
		if ids["branch"] == "" || ids["branch2"] == "" {
			return "", false
		}
		return "/v1/branches/compare?a=" + ids["branch"] + "&b=" + ids["branch2"], true
	case "/v1/checkpoints/compare":
		if ids["checkpoint"] == "" || ids["checkpoint2"] == "" {
			return "", false
		}
		return "/v1/checkpoints/compare?from=" + ids["checkpoint"] + "&to=" + ids["checkpoint2"], true
	case "/v1/search":
		return "/v1/search?q=" + ids["q"], true
	}
	return p, true
}

// seedHTTPVault fills a vault with one of everything and returns ids by kind.
func seedHTTPVault(t *testing.T, e *testutil.Env, u *testutil.User, c *testutil.Client) map[string]string {
	t.Helper()
	idea, k := seedViewsIdea(t, u)
	svc := e.Svc()
	ids := map[string]string{"q": "marketplace", "idea": idea.Idea.ID.String(), "branch": idea.Branch.ID.String(), "knowledge": k["D1"].ID.String()}
	brs := try(e.Store().ListBranches(ctx(), u.ID, idea.Idea.ID)).must(t)
	for _, b := range brs {
		if !b.IsDefault {
			ids["branch2"] = b.ID.String()
		}
	}
	cps := try(e.Store().ListCheckpoints(ctx(), u.ID, idea.Idea.ID, nil)).must(t)
	ids["checkpoint"], ids["checkpoint2"] = cps[0].ID.String(), cps[1].ID.String()
	arts := try(e.Store().ListArtifacts(ctx(), postgres.ArtifactFilter{UserID: u.ID, IdeaID: &idea.Idea.ID})).must(t)
	ids["artifact"] = arts[0].ID.String()
	pack := try(svc.BuildContextPack(ctx(), u.Actor(), service.BuildContextPackInput{IdeaID: idea.Idea.ID})).must(t)
	ids["pack"] = pack.ID.String()
	d := try(svc.AnalyzeDelta(ctx(), u.Actor(), service.AnalyzeDeltaInput{IdeaID: idea.Idea.ID, Text: externalTranscript})).must(t)
	ids["delta"] = d.ID.String()
	ids["conversation"] = d.ConversationID.String()
	im := try(svc.ImportNow(ctx(), u.Actor(), service.ImportNowInput{CreateImportInput: service.CreateImportInput{SourceKind: "paste", Text: "User: hello there, should we add ratings?\n\nAssistant: yes."}, IdeaID: &idea.Idea.ID})).must(t)
	ids["import"] = im.Import.ID.String()
	try(svc.AnalyzePrompt(ctx(), u.Actor(), "make it better", nil)).must(t)
	evs := c.Stream("/v1/agent/chat", map[string]any{"message": "Continue the biker community platform from checkpoint 1.", "idea_id": idea.Idea.ID})
	var started struct {
		RunID uuid.UUID `json:"run_id"`
	}
	testutil.EventsNamed(evs, "run_started")[0].Decode(t, &started)
	ids["run"] = started.RunID.String()
	var lab struct {
		ID uuid.UUID `json:"id"`
	}
	c.JSON("POST", "/v1/models/lab", map[string]any{"prompt": "Say hi", "models": []string{"mock/offline-planner"}}, 201, &lab)
	ids["lab"] = lab.ID.String()
	e.DrainJobs(t)
	return ids
}
