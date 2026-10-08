package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/config"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	rds "github.com/Prathameshppawar/ideavault/apps/api/internal/repository/redis"
)

func TestOpenAPIDocumentsEveryRoute(t *testing.T) {
	api := New(Options{})
	doc := api.OpenAPI()
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi version = %v", doc["openapi"])
	}
	paths := doc["paths"].(map[string]map[string]any)
	routes := api.Routes()
	if len(routes) < 80 {
		t.Fatalf("only %d routes registered", len(routes))
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		if seen[key] {
			t.Errorf("route %s registered twice", key)
		}
		seen[key] = true
		op, ok := paths[rt.Path][strings.ToLower(rt.Method)].(map[string]any)
		if !ok {
			t.Errorf("route %s is not documented", key)
			continue
		}
		if op["summary"] == "" || op["operationId"] == "" {
			t.Errorf("%s lacks summary/operationId", key)
		}
		id := op["operationId"].(string)
		if ids[id] {
			t.Errorf("duplicate operationId %s", id)
		}
		ids[id] = true
		if rt.Auth && op["security"] == nil {
			t.Errorf("%s requires auth but documents no security", key)
		}
		if rt.Handler == nil {
			t.Errorf("%s has no handler", key)
		}
		if !strings.HasPrefix(rt.Path, "/v1/") {
			t.Errorf("%s is not versioned", key)
		}
		// Path parameters must be declared.
		for _, m := range regexp.MustCompile(`\{([a-z_]+)\}`).FindAllStringSubmatch(rt.Path, -1) {
			found := false
			params, _ := op["parameters"].([]map[string]any)
			for _, p := range params {
				if p["name"] == m[1] && p["in"] == "path" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s does not declare path parameter %s", key, m[1])
			}
		}
		if (rt.Method == "POST" || rt.Method == "PUT" || rt.Method == "PATCH") && rt.Req == nil && !strings.HasSuffix(rt.Path, "/logout") &&
			!strings.HasSuffix(rt.Path, "/contradictions") && !strings.HasSuffix(rt.Path, "/backfill") && !strings.HasSuffix(rt.Path, "/discard") &&
			!strings.HasSuffix(rt.Path, "/disconnect") {
			t.Errorf("%s accepts a body but documents no request schema", key)
		}
	}
	// Every documented path is a registered route (no stale docs).
	for p, ops := range paths {
		for m := range ops {
			if !seen[strings.ToUpper(m)+" "+p] {
				t.Errorf("documented %s %s is not registered", m, p)
			}
		}
	}
	// Every $ref resolves.
	raw, _ := json.Marshal(doc)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, m := range regexp.MustCompile(`"#/components/schemas/([^"]+)"`).FindAllStringSubmatch(string(raw), -1) {
		if _, ok := schemas[m[1]]; !ok {
			t.Errorf("dangling $ref %s", m[1])
		}
	}
}

// The frontend is generated from packages/contracts/openapi.json; it must match the code.
func TestOpenAPIContractIsUpToDate(t *testing.T) {
	dir, _ := os.Getwd()
	var contract string
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "packages", "contracts", "openapi.json")
		if _, err := os.Stat(cand); err == nil {
			contract = cand
			break
		}
		dir = filepath.Dir(dir)
	}
	if contract == "" {
		t.Skip("packages/contracts/openapi.json not found")
	}
	committed, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(committed, &want); err != nil {
		t.Fatalf("committed contract is not JSON: %v", err)
	}
	b, _ := json.Marshal(New(Options{}).OpenAPI())
	_ = json.Unmarshal(b, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatal("packages/contracts/openapi.json is stale; regenerate with: cd apps/api && go run ./cmd/openapi > ../../packages/contracts/openapi.json")
	}
}

func TestSchemaGenerator(t *testing.T) {
	type inner struct {
		N int `json:"n"`
	}
	type sample struct {
		Name     string          `json:"name"`
		Optional string          `json:"optional,omitempty"`
		Ptr      *int            `json:"ptr"`
		Hidden   string          `json:"-"`
		List     []inner         `json:"list"`
		Map      map[string]bool `json:"map"`
		Raw      json.RawMessage `json:"raw"`
		Bytes    []byte          `json:"bytes"`
		domain.EntityRef
	}
	g := &schemaGen{defs: map[string]any{}}
	ref := g.schema(reflect.TypeOf(sample{}))
	key := strings.TrimPrefix(ref["$ref"].(string), "#/components/schemas/")
	s := g.defs[key].(map[string]any)
	all := s["allOf"].([]any)
	obj := all[1].(map[string]any)
	props := obj["properties"].(map[string]any)
	if _, ok := props["Hidden"]; ok {
		t.Error("json:\"-\" fields must be omitted")
	}
	if !reflect.DeepEqual(obj["required"], []string{"bytes", "list", "map", "name", "raw"}) {
		t.Errorf("required = %v (omitempty and pointer fields are optional)", obj["required"])
	}
	if props["bytes"].(map[string]any)["format"] != "byte" || props["list"].(map[string]any)["type"] != "array" {
		t.Errorf("props = %v", props)
	}
	if !strings.Contains(all[0].(map[string]any)["$ref"].(string), "EntityRef") {
		t.Error("embedded structs are composed with allOf")
	}
}

func TestWriteErrorStatusMapping(t *testing.T) {
	tests := []struct {
		err    error
		status int
		kind   string
	}{
		{domain.NotFound("idea"), 404, "not_found"},
		{domain.Invalid("title", "bad"), 400, "invalid"},
		{domain.Conflict("dup"), 409, "conflict"},
		{domain.Immutable("history"), 409, "immutable"},
		{domain.Forbidden("no"), 403, "forbidden"},
		{domain.Unauthorized("who"), 401, "unauthorized"},
		{domain.RateLimited("slow"), 429, "rate_limited"},
		{domain.Unavailable("down"), 503, "unavailable"},
		{errors.New("pq: secret sk-abcdefghijklmnopqrstuv leaked"), 500, "internal"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		writeError(rec, httptest.NewRequest("GET", "/x", nil), tt.err)
		var body ErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code != tt.status || body.Kind != tt.kind {
			t.Errorf("%v → %d %s, want %d %s", tt.err, rec.Code, body.Kind, tt.status, tt.kind)
		}
		if tt.status == 500 && strings.Contains(rec.Body.String(), "sk-") {
			t.Error("internal errors must not leak details to clients")
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Error("API responses must not be cached")
		}
	}
}

func TestCSRFMiddleware(t *testing.T) {
	a := &API{cfg: &config.Config{WebOrigins: []string{"http://localhost:3000"}}}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h := a.csrf(ok)
	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"GET is safe", "GET", nil, 204},
		{"HEAD is safe", "HEAD", nil, 204},
		{"POST without header", "POST", nil, 403},
		{"POST with wrong header value", "POST", map[string]string{"X-IdeaVault-CSRF": "yes"}, 403},
		{"POST with header", "POST", map[string]string{"X-IdeaVault-CSRF": "1"}, 204},
		{"DELETE without header", "DELETE", nil, 403},
		{"cross-site origin", "POST", map[string]string{"X-IdeaVault-CSRF": "1", "Origin": "https://evil.example"}, 403},
		{"allowed origin", "PATCH", map[string]string{"X-IdeaVault-CSRF": "1", "Origin": "http://localhost:3000"}, 204},
		{"same host origin", "POST", map[string]string{"X-IdeaVault-CSRF": "1", "Origin": "http://api.test"}, 204},
		{"bearer tokens are not CSRF-able", "POST", map[string]string{"Authorization": "Bearer ivt_x"}, 204},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, "http://api.test/v1/ideas", nil)
		for k, v := range tt.headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != tt.want {
			t.Errorf("%s: status %d, want %d", tt.name, rec.Code, tt.want)
		}
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	a := &API{cfg: &config.Config{RateLimitAPIPerMin: 2, RateLimitAuthPerMin: 1}, limiter: rds.NewMemoryLimiter()}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := a.rateLimit("api", ok)
	codes := []int{}
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/v1/ideas", nil)
		req.RemoteAddr = "203.0.113.7:5555"
		rec := httptest.NewRecorder()
		h(rec, req)
		codes = append(codes, rec.Code)
		if i == 2 && (rec.Header().Get("Retry-After") == "" || rec.Header().Get("X-RateLimit-Remaining") != "0") {
			t.Errorf("429 must carry Retry-After and remaining=0: %v", rec.Header())
		}
	}
	if !reflect.DeepEqual(codes, []int{200, 200, 429}) {
		t.Errorf("codes = %v", codes)
	}
	// Another client (different IP) has its own budget.
	req := httptest.NewRequest("GET", "/v1/ideas", nil)
	req.RemoteAddr = "198.51.100.9:1"
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 200 {
		t.Errorf("separate clients must not share a bucket: %d", rec.Code)
	}
}

func TestClientIPAndAttachmentNames(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	if clientIP(r) != "192.0.2.1" {
		t.Errorf("clientIP = %s", clientIP(r))
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	if clientIP(r) != "203.0.113.5" {
		t.Errorf("clientIP with XFF = %s", clientIP(r))
	}
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	if clientIP(r) != "192.0.2.1" {
		t.Error("garbage X-Forwarded-For must be ignored")
	}
	tests := []struct{ title, ext, want string }{
		{"Biker Platform — Action Plan", ".md", "Biker-Platform-Action-Plan.md"},
		{`../../etc/passwd"; evil=1`, ".md", "etc-passwd-evil-1.md"},
		{"", ".json", "ideavault.json"},
		{"***", ".md", "ideavault.md"},
	}
	for _, tt := range tests {
		if got := attachmentName(tt.title, tt.ext); got != tt.want {
			t.Errorf("attachmentName(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
	if n := attachmentName(strings.Repeat("a", 200), ".md"); len(n) != 83 {
		t.Errorf("attachment names are capped: %d", len(n))
	}
	if !reflect.DeepEqual(nonNilSlice[int](nil), []int{}) {
		t.Error("nonNilSlice(nil) must be an empty slice")
	}
}

func TestWithEmptySlices(t *testing.T) {
	type inner struct {
		Tags []string `json:"tags"`
	}
	type payload struct {
		List    []int            `json:"list"`
		Omitted []int            `json:"omitted,omitempty"`
		Raw     json.RawMessage  `json:"raw"`
		Bytes   []byte           `json:"bytes"`
		Ptr     *inner           `json:"ptr"`
		Nested  []inner          `json:"nested"`
		Map     map[string][]int `json:"map"`
		Any     any              `json:"any"`
		NilPtr  *inner           `json:"nil_ptr"`
		Fixed   [2]inner         `json:"fixed"`
		hidden  []int
	}
	shared := []inner{{}}
	in := &payload{Ptr: &inner{}, Nested: shared, Map: map[string][]int{"a": nil}, Any: inner{}, Raw: json.RawMessage(`{"x":1}`)}
	b, err := json.Marshal(withEmptySlices(in))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"list":[],"raw":{"x":1},"bytes":null,"ptr":{"tags":[]},"nested":[{"tags":[]}],"map":{"a":[]},"any":{"tags":[]},"nil_ptr":null,"fixed":[{"tags":[]},{"tags":[]}]}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	if in.List != nil || in.Ptr.Tags != nil || shared[0].Tags != nil || in.Map["a"] != nil {
		t.Error("the input (possibly shared data) must never be modified")
	}
	if withEmptySlices(nil) != nil {
		t.Error("nil stays nil")
	}
	var nilSlice []int
	if b, _ := json.Marshal(withEmptySlices(nilSlice)); string(b) != "[]" {
		t.Errorf("top-level nil slice = %s", b)
	}
	if b, _ := json.Marshal(withEmptySlices(map[string]any{"k": []any(nil)})); string(b) != `{"k":[]}` {
		t.Errorf("interface map values = %s", b)
	}
	rec := httptest.NewRecorder()
	writeJSON(rec, 200, struct {
		Items []string `json:"items"`
	}{})
	if strings.TrimSpace(rec.Body.String()) != `{"items":[]}` {
		t.Errorf("writeJSON must emit [] for nil slices, got %s", rec.Body.String())
	}
	_ = in.hidden
}
