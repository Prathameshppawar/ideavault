package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // compact JSON; "" means not found
	}{
		{"plain object", `{"a":1}`, `{"a":1}`},
		{"plain array", ` [1, 2] `, `[1,2]`},
		{"json fence", "```json\n{\"intent\": \"fork\"}\n```", `{"intent":"fork"}`},
		{"bare fence", "```\n[{\"x\":true}]\n```", `[{"x":true}]`},
		{"prose before", "Sure! Here is the result:\n{\"ok\": true, \"n\": [1,2]}\nLet me know.", `{"ok":true,"n":[1,2]}`},
		{"prose with fence", "Here you go:\n```json\n{\"k\": \"v\"}\n```\nThanks", `{"k":"v"}`},
		{"braces in strings", `Result: {"text": "use {braces} and [brackets]", "q": "\"quoted\""} done`, `{"text":"use {braces} and [brackets]","q":"\"quoted\""}`},
		{"citation brackets before json", `Per [D3] and [A1]: {"verdict": "contradiction"}`, `{"verdict":"contradiction"}`},
		{"nested", `x {"a": {"b": [{"c": 1}]}} y`, `{"a":{"b":[{"c":1}]}}`},
		{"truncated", `{"a": [1, 2`, ""},
		{"no json", "I cannot help with that.", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, ok := ExtractJSON(tt.in)
			if tt.want == "" {
				if ok {
					t.Errorf("expected no JSON, got %s", raw)
				}
				return
			}
			if !ok {
				t.Fatalf("no JSON found in %q", tt.in)
			}
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				t.Fatalf("extracted invalid JSON %s: %v", raw, err)
			}
			got, _ := json.Marshal(v)
			var w any
			_ = json.Unmarshal([]byte(tt.want), &w)
			want, _ := json.Marshal(w)
			if string(got) != string(want) {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

func TestValidateJSONSchema(t *testing.T) {
	schema := json.RawMessage(`{
	  "type": "object",
	  "required": ["summary", "items"],
	  "properties": {
	    "summary": {"type": "string", "minLength": 3},
	    "count": {"type": "integer", "minimum": 0, "maximum": 10},
	    "score": {"type": "number"},
	    "tags": {"type": ["array", "null"], "items": {"type": "string"}, "maxItems": 2},
	    "items": {"type": "array", "minItems": 1, "items": {"type": "object", "required": ["kind"], "properties": {
	      "kind": {"type": "string", "enum": ["decision", "question"]},
	      "confidence": {"type": "number", "minimum": 0, "maximum": 1}
	    }}}
	  }
	}`)
	decode := func(s string) any {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatalf("bad test JSON %s: %v", s, err)
		}
		return v
	}
	valid := []string{
		`{"summary": "abc", "items": [{"kind": "decision"}]}`,
		`{"summary": "abcd", "count": 3, "score": 1.5, "tags": null, "items": [{"kind": "question", "confidence": 0.4}]}`,
		`{"summary": "abc", "items": [{"kind": "decision"}], "score": 2, "extra": "allowed"}`,
	}
	for _, v := range valid {
		if errs := ValidateJSONSchema(schema, decode(v)); len(errs) != 0 {
			t.Errorf("%s should be valid: %v", v, errs)
		}
	}
	invalid := []struct {
		doc  string
		want string
	}{
		{`[]`, "expected object"},
		{`{"items": [{"kind": "decision"}]}`, `missing required property "summary"`},
		{`{"summary": "ab", "items": [{"kind": "decision"}]}`, "shorter than 3"},
		{`{"summary": "abc", "items": []}`, "at least 1 items"},
		{`{"summary": "abc", "items": [{"kind": "idea"}]}`, "not in enum"},
		{`{"summary": "abc", "items": [{}]}`, `$.items[0]: missing required property "kind"`},
		{`{"summary": "abc", "items": [{"kind": "decision", "confidence": 1.5}]}`, "above maximum"},
		{`{"summary": "abc", "count": 2.5, "items": [{"kind": "decision"}]}`, "expected integer"},
		{`{"summary": "abc", "count": -1, "items": [{"kind": "decision"}]}`, "below minimum"},
		{`{"summary": "abc", "tags": ["a","b","c"], "items": [{"kind": "decision"}]}`, "at most 2 items"},
		{`{"summary": 5, "items": [{"kind": "decision"}]}`, "expected string, got integer"},
	}
	for _, tt := range invalid {
		errs := ValidateJSONSchema(schema, decode(tt.doc))
		if len(errs) == 0 || !strings.Contains(strings.Join(errs, "; "), tt.want) {
			t.Errorf("%s: want error containing %q, got %v", tt.doc, tt.want, errs)
		}
	}
	if errs := ValidateJSONSchema(json.RawMessage(`{not json`), map[string]any{}); len(errs) != 1 || !strings.Contains(errs[0], "invalid schema") {
		t.Errorf("invalid schema should be reported, got %v", errs)
	}
}
