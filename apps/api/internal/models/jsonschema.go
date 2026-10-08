package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExtractJSON finds the first complete JSON object or array in text (handles code fences
// and leading prose). Returns the raw JSON and true on success.
func ExtractJSON(text string) (json.RawMessage, bool) {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "```") {
		if i := strings.Index(t, "\n"); i >= 0 {
			t = t[i+1:]
		}
		if j := strings.LastIndex(t, "```"); j >= 0 {
			t = t[:j]
		}
		t = strings.TrimSpace(t)
	}
	if json.Valid([]byte(t)) && (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) {
		return json.RawMessage(t), true
	}
	for start := 0; start < len(t); start++ {
		if t[start] != '{' && t[start] != '[' {
			continue
		}
		depth, inStr, esc := 0, false, false
		for i := start; i < len(t); i++ {
			c := t[i]
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
					cand := t[start : i+1]
					if json.Valid([]byte(cand)) {
						return json.RawMessage(cand), true
					}
					i = len(t)
				}
			}
		}
	}
	return nil, false
}

// ValidateJSONSchema validates a decoded JSON value against a (subset of) JSON Schema:
// type, properties, required, items, enum, minItems, maxItems, minLength, minimum, maximum.
// It returns human-readable errors (empty when valid).
func ValidateJSONSchema(schema json.RawMessage, value any) []string {
	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		return []string{"invalid schema: " + err.Error()}
	}
	var errs []string
	validateNode(s, value, "$", &errs)
	return errs
}

func validateNode(s map[string]any, v any, path string, errs *[]string) {
	if len(*errs) > 50 {
		return
	}
	if t, ok := s["type"]; ok {
		types := []string{}
		switch tt := t.(type) {
		case string:
			types = append(types, tt)
		case []any:
			for _, x := range tt {
				if xs, ok := x.(string); ok {
					types = append(types, xs)
				}
			}
		}
		if len(types) > 0 && !matchesAnyType(v, types) {
			*errs = append(*errs, fmt.Sprintf("%s: expected %s, got %s", path, strings.Join(types, "|"), jsonType(v)))
			return
		}
	}
	if enum, ok := s["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				found = true
				break
			}
		}
		if !found {
			*errs = append(*errs, fmt.Sprintf("%s: value %v not in enum", path, v))
		}
	}
	switch val := v.(type) {
	case map[string]any:
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if rs, ok := r.(string); ok {
					if _, present := val[rs]; !present {
						*errs = append(*errs, fmt.Sprintf("%s: missing required property %q", path, rs))
					}
				}
			}
		}
		if props, ok := s["properties"].(map[string]any); ok {
			for k, sub := range props {
				if pv, present := val[k]; present {
					if subm, ok := sub.(map[string]any); ok {
						validateNode(subm, pv, path+"."+k, errs)
					}
				}
			}
		}
	case []any:
		if mi, ok := s["minItems"].(float64); ok && float64(len(val)) < mi {
			*errs = append(*errs, fmt.Sprintf("%s: expected at least %v items", path, mi))
		}
		if ma, ok := s["maxItems"].(float64); ok && float64(len(val)) > ma {
			*errs = append(*errs, fmt.Sprintf("%s: expected at most %v items", path, ma))
		}
		if items, ok := s["items"].(map[string]any); ok {
			for i, it := range val {
				validateNode(items, it, fmt.Sprintf("%s[%d]", path, i), errs)
			}
		}
	case string:
		if ml, ok := s["minLength"].(float64); ok && float64(len([]rune(val))) < ml {
			*errs = append(*errs, fmt.Sprintf("%s: shorter than %v", path, ml))
		}
	case float64:
		if mn, ok := s["minimum"].(float64); ok && val < mn {
			*errs = append(*errs, fmt.Sprintf("%s: below minimum %v", path, mn))
		}
		if mx, ok := s["maximum"].(float64); ok && val > mx {
			*errs = append(*errs, fmt.Sprintf("%s: above maximum %v", path, mx))
		}
	}
}

func matchesAnyType(v any, types []string) bool {
	jt := jsonType(v)
	for _, t := range types {
		if t == jt || (t == "number" && jt == "integer") {
			return true
		}
	}
	return false
}

func jsonType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if x == float64(int64(x)) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// EstimateTokens approximates token count for text (≈4 chars/token for English).
func EstimateTokens(s string) int {
	n := len(s) / 4
	if n == 0 && s != "" {
		n = 1
	}
	return n
}

// EstimateRequestTokens approximates input tokens of a request.
func EstimateRequestTokens(r Request) int {
	n := EstimateTokens(r.System)
	for _, m := range r.Messages {
		n += EstimateTokens(m.Content) + 4
		for _, tc := range m.ToolCalls {
			n += EstimateTokens(string(tc.Arguments)) + 8
		}
	}
	for _, t := range r.Tools {
		n += EstimateTokens(t.Description) + EstimateTokens(string(t.Parameters))
	}
	return n
}
