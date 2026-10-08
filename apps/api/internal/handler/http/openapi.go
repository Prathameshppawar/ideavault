package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/evals"
)

// BenchmarkTasks exposes the built-in benchmark suite.
var BenchmarkTasks = evals.Benchmarks

func (a *API) registerAll() {
	if len(a.routes) > 0 {
		return
	}
	a.registerCore()
	a.registerAI()
	a.registerPlatform()
}

// OpenAPI builds the OpenAPI 3.1 document from the registered routes and Go types,
// so documentation can never drift from the code.
func (a *API) OpenAPI() map[string]any {
	a.registerAll()
	g := &schemaGen{defs: map[string]any{}}
	paths := map[string]map[string]any{}
	tags := map[string]bool{}
	pathParam := regexp.MustCompile(`\{([a-z_]+)\}`)
	for _, rt := range a.routes {
		op := map[string]any{"summary": rt.Summary, "tags": []string{rt.Tag}, "operationId": opID(rt)}
		tags[rt.Tag] = true
		var params []map[string]any
		for _, m := range pathParam.FindAllStringSubmatch(rt.Path, -1) {
			typ := map[string]any{"type": "string"}
			if m[1] == "id" {
				typ["format"] = "uuid"
			}
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": typ})
		}
		for _, q := range rt.Query {
			s := map[string]any{"type": "string"}
			switch q.Type {
			case "integer":
				s = map[string]any{"type": "integer"}
			case "boolean":
				s = map[string]any{"type": "boolean"}
			case "uuid":
				s = map[string]any{"type": "string", "format": "uuid"}
			}
			p := map[string]any{"name": q.Name, "in": "query", "schema": s}
			if q.Desc != "" {
				p["description"] = q.Desc
			}
			params = append(params, p)
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if rt.Req != nil {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": g.schema(reflect.TypeOf(rt.Req))}}}
		}
		resp := map[string]any{"description": "OK"}
		switch {
		case rt.Stream:
			resp["content"] = map[string]any{"text/event-stream": map[string]any{"schema": map[string]any{"type": "string",
				"description": "SSE events: run_started, token, trace, confirmation_required, message, run_completed, error, done"}}}
		case rt.Raw != "":
			resp["content"] = map[string]any{rt.Raw: map[string]any{"schema": map[string]any{"type": "string"}}}
		case rt.Resp != nil:
			resp["content"] = map[string]any{"application/json": map[string]any{"schema": g.schema(reflect.TypeOf(rt.Resp))}}
		}
		op["responses"] = map[string]any{"200": resp, "default": map[string]any{"description": "Error",
			"content": map[string]any{"application/json": map[string]any{"schema": g.schema(reflect.TypeOf(ErrorBody{}))}}}}
		if rt.Auth {
			op["security"] = []map[string]any{{"session": []string{}}, {"bearer": []string{}}}
		}
		if paths[rt.Path] == nil {
			paths[rt.Path] = map[string]any{}
		}
		paths[rt.Path][strings.ToLower(rt.Method)] = op
	}
	var tagList []map[string]any
	var names []string
	for t := range tags {
		names = append(names, t)
	}
	sort.Strings(names)
	for _, t := range names {
		tagList = append(tagList, map[string]any{"name": t})
	}
	servers := []map[string]any{{"url": "/"}}
	if a.cfg != nil && a.cfg.PublicURL != "" {
		servers = []map[string]any{{"url": a.cfg.PublicURL}}
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "IdeaVault API", "version": Version,
			"description": "Personal AI thinking system: ideas, branches, immutable checkpoints, knowledge, artifacts, context packs and an agent supervisor. Cookie sessions require the X-IdeaVault-CSRF: 1 header on unsafe requests; API tokens use Authorization: Bearer."},
		"servers": servers,
		"tags":    tagList,
		"paths":   paths,
		"components": map[string]any{
			"schemas": g.defs,
			"securitySchemes": map[string]any{
				"session": map[string]any{"type": "apiKey", "in": "cookie", "name": sessionCookie},
				"bearer":  map[string]any{"type": "http", "scheme": "bearer"},
			},
		},
	}
}

func opID(rt Route) string {
	p := strings.NewReplacer("/v1/", "", "{", "", "}", "", "/", "_", "-", "_").Replace(rt.Path)
	return strings.ToLower(rt.Method) + "_" + p
}

func (a *API) openapi(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, a.OpenAPI())
}

func (a *API) swaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>IdeaVault API</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css"></head>
<body><div id="ui"></div><script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url:'v1/openapi.json',dom_id:'#ui'})</script></body></html>`))
}

// schemaGen converts Go types into JSON Schema (OpenAPI 3.1 dialect).
type schemaGen struct {
	defs map[string]any
}

var (
	timeType = reflect.TypeOf(time.Time{})
	uuidType = reflect.TypeOf(uuid.UUID{})
	rawType  = reflect.TypeOf(json.RawMessage{})
)

func (g *schemaGen) schema(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t {
	case timeType:
		return map[string]any{"type": "string", "format": "date-time"}
	case uuidType:
		return map[string]any{"type": "string", "format": "uuid"}
	case rawType:
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": g.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": g.schema(t.Elem())}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		name := t.Name()
		if name == "" {
			return g.structSchema(t)
		}
		key := schemaName(t)
		if _, ok := g.defs[key]; !ok {
			g.defs[key] = map[string]any{"type": "object"} // placeholder for recursion
			g.defs[key] = g.structSchema(t)
		}
		return map[string]any{"$ref": "#/components/schemas/" + key}
	}
	return map[string]any{}
}

func schemaName(t reflect.Type) string {
	pkg := t.PkgPath()
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	name := t.Name()
	if pkg == "domain" || pkg == "" {
		return name
	}
	if pkg == "httpapi" {
		return "Api" + strings.ToUpper(name[:1]) + name[1:]
	}
	return strings.ToUpper(pkg[:1]) + pkg[1:] + name
}

func (g *schemaGen) structSchema(t reflect.Type) map[string]any {
	props := map[string]any{}
	var required []string
	var embedded []any
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			embedded = append(embedded, g.schema(f.Type))
			continue
		}
		if name == "" {
			name = f.Name
		}
		props[name] = g.schema(f.Type)
		if !strings.Contains(opts, "omitempty") && f.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		s["required"] = required
	}
	if len(embedded) > 0 {
		return map[string]any{"allOf": append(embedded, s)}
	}
	return s
}
