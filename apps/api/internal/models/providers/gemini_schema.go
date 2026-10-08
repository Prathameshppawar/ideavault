package providers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// geminiSchemaMaxDepth bounds recursion (and $ref expansion) in the sanitizer.
const geminiSchemaMaxDepth = 24

// geminiAllowedKeys is the OpenAPI-subset of schema keywords Gemini accepts.
// Everything else ($schema, $id, additionalProperties, default, examples,
// patternProperties, if/then/else, not, ...) is dropped.
var geminiAllowedKeys = map[string]bool{
	"type": true, "format": true, "title": true, "description": true, "nullable": true,
	"enum": true, "items": true, "properties": true, "required": true,
	"minItems": true, "maxItems": true, "minProperties": true, "maxProperties": true,
	"minLength": true, "maxLength": true, "pattern": true, "minimum": true, "maximum": true,
	"propertyOrdering": true,
}

// geminiFormats lists the formats Gemini accepts per (upper-case) type.
var geminiFormats = map[string]map[string]bool{
	"STRING":  {"enum": true, "date-time": true},
	"INTEGER": {"int32": true, "int64": true},
	"NUMBER":  {"float": true, "double": true},
}

// sanitizeGeminiSchema converts a JSON Schema into the OpenAPI subset accepted
// by Gemini function declarations and responseSchema:
//
//   - drops unsupported keywords (additionalProperties, $schema, $id, default,
//     examples, ...), keeping only geminiAllowedKeys;
//   - resolves local $ref (#/$defs/X, #/definitions/X) up to a depth limit;
//   - collapses anyOf/oneOf/allOf to the first non-null branch (marking
//     nullable when a null branch was present) and type arrays likewise;
//   - converts const to a single-value enum; non-string enums are dropped
//     and listed in the description instead;
//   - upper-cases types, infers missing types, drops unsupported formats,
//     filters required to existing properties, and defaults array items.
//
// It returns nil when the schema is empty or describes an object with no
// properties (Gemini rejects OBJECT parameters with empty properties).
func sanitizeGeminiSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("schema must be a JSON object: %w", err)
	}
	s := geminiSanitizer{root: root}
	out := s.node(root, 0)
	if out == nil {
		return nil, nil
	}
	if out["type"] == "OBJECT" {
		if props, _ := out["properties"].(map[string]any); len(props) == 0 {
			return nil, nil
		}
	}
	return json.Marshal(out)
}

type geminiSanitizer struct {
	root map[string]any
}

func (s geminiSanitizer) resolveRef(ref string) (map[string]any, bool) {
	for _, prefix := range []string{"#/$defs/", "#/definitions/"} {
		if name, ok := strings.CutPrefix(ref, prefix); ok {
			defs, _ := s.root[strings.TrimSuffix(strings.TrimPrefix(prefix, "#/"), "/")].(map[string]any)
			def, ok := defs[name].(map[string]any)
			return def, ok
		}
	}
	return nil, false
}

// node sanitizes one schema node; nil means "drop this node".
func (s geminiSanitizer) node(in map[string]any, depth int) map[string]any {
	if in == nil || depth > geminiSchemaMaxDepth {
		return nil
	}
	if ref, ok := in["$ref"].(string); ok {
		def, found := s.resolveRef(ref)
		if !found {
			return map[string]any{"type": "STRING", "description": describe(in, "")}
		}
		merged := make(map[string]any, len(def)+1)
		for k, v := range def {
			merged[k] = v
		}
		if d, ok := in["description"].(string); ok && d != "" {
			merged["description"] = d
		}
		return s.node(merged, depth+1)
	}

	nullable := false
	// Collapse combinators to their first non-null branch.
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		branches, ok := in[key].([]any)
		if !ok {
			continue
		}
		var chosen map[string]any
		for _, b := range branches {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := bm["type"].(string); t == "null" {
				nullable = true
				continue
			}
			if chosen == nil {
				chosen = bm
			}
		}
		if chosen != nil {
			merged := make(map[string]any, len(in)+len(chosen))
			for k, v := range chosen {
				merged[k] = v
			}
			for k, v := range in {
				if k == "anyOf" || k == "oneOf" || k == "allOf" {
					continue
				}
				if _, exists := merged[k]; !exists || k == "description" {
					merged[k] = v
				}
			}
			if nullable {
				merged["nullable"] = true
			}
			return s.node(merged, depth+1)
		}
	}

	out := map[string]any{}
	typ := ""
	switch t := in["type"].(type) {
	case string:
		typ = t
	case []any:
		for _, v := range t {
			if ts, ok := v.(string); ok {
				if ts == "null" {
					nullable = true
				} else if typ == "" {
					typ = ts
				}
			}
		}
	}
	if typ == "null" {
		return nil
	}

	if c, ok := in["const"]; ok {
		in = shallowCopy(in)
		in["enum"] = []any{c}
	}
	if typ == "" {
		switch {
		case in["properties"] != nil:
			typ = "object"
		case in["items"] != nil:
			typ = "array"
		case in["enum"] != nil:
			typ = "string"
		default:
			typ = "string"
		}
	}
	typ = strings.ToUpper(typ)
	out["type"] = typ

	for k, v := range in {
		if !geminiAllowedKeys[k] {
			continue
		}
		switch k {
		case "type", "properties", "items", "required", "enum", "format":
			// handled below
		default:
			out[k] = v
		}
	}
	if nullable {
		out["nullable"] = true
	}

	if f, ok := in["format"].(string); ok && geminiFormats[typ][f] {
		out["format"] = f
	}

	if enum, ok := in["enum"].([]any); ok && len(enum) > 0 {
		strs := make([]any, 0, len(enum))
		allStrings := true
		for _, e := range enum {
			str, ok := e.(string)
			if !ok {
				allStrings = false
				break
			}
			strs = append(strs, str)
		}
		if allStrings && typ == "STRING" {
			out["enum"] = strs
		} else {
			b, _ := json.Marshal(enum)
			out["description"] = describe(out, "Allowed values: "+string(b))
		}
	}

	switch typ {
	case "OBJECT":
		props, _ := in["properties"].(map[string]any)
		cleaned := map[string]any{}
		names := make([]string, 0, len(props))
		for name := range props {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			pm, ok := props[name].(map[string]any)
			if !ok {
				continue
			}
			if sub := s.node(pm, depth+1); sub != nil {
				cleaned[name] = sub
			}
		}
		if len(cleaned) > 0 {
			out["properties"] = cleaned
			if req, ok := in["required"].([]any); ok {
				var kept []any
				for _, r := range req {
					if name, ok := r.(string); ok {
						if _, exists := cleaned[name]; exists {
							kept = append(kept, name)
						}
					}
				}
				if len(kept) > 0 {
					out["required"] = kept
				}
			}
		}
		if order, ok := out["propertyOrdering"].([]any); ok {
			var kept []any
			for _, o := range order {
				if name, ok := o.(string); ok {
					if _, exists := cleaned[name]; exists {
						kept = append(kept, name)
					}
				}
			}
			if len(kept) > 0 {
				out["propertyOrdering"] = kept
			} else {
				delete(out, "propertyOrdering")
			}
		}
	case "ARRAY":
		var items map[string]any
		switch it := in["items"].(type) {
		case map[string]any:
			items = s.node(it, depth+1)
		case []any: // tuple form: keep the first schema
			if len(it) > 0 {
				if m, ok := it[0].(map[string]any); ok {
					items = s.node(m, depth+1)
				}
			}
		}
		if items == nil {
			items = map[string]any{"type": "STRING"}
		}
		out["items"] = items
	}
	return out
}

func shallowCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// describe appends extra to the node's description.
func describe(node map[string]any, extra string) string {
	d, _ := node["description"].(string)
	switch {
	case extra == "":
		return d
	case d == "":
		return extra
	default:
		return d + " " + extra
	}
}
