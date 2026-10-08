package imports

import "testing"

func TestHasJSONKey(t *testing.T) {
	tests := []struct {
		data string
		key  string
		want bool
	}{
		{`{"mapping": {}}`, "mapping", true},
		{`[{"mapping"` + "\n\t:" + `{}}]`, "mapping", true},
		{`{"text": "the word \"mapping\": appears in a string"}`, "mapping", false},
		{`{"mappings": {}}`, "mapping", false},
		{`["mapping"]`, "mapping", false},
	}
	for _, tt := range tests {
		if got := HasJSONKey([]byte(tt.data), tt.key); got != tt.want {
			t.Errorf("HasJSONKey(%s, %q) = %v, want %v", tt.data, tt.key, got, tt.want)
		}
	}
}

func TestSniffHelpers(t *testing.T) {
	if !LooksLikeJSON([]byte("\xEF\xBB\xBF  [1]")) || LooksLikeJSON([]byte("hello")) {
		t.Error("LooksLikeJSON")
	}
	if !LooksLikeHTML([]byte("  <!DOCTYPE html><html>")) || !LooksLikeHTML([]byte("<html><body>x")) || LooksLikeHTML([]byte("<notes>")) {
		t.Error("LooksLikeHTML")
	}
	if !LooksLikeText([]byte("plain words\n")) || LooksLikeText([]byte("PK\x03\x04\x00\x00")) || LooksLikeText([]byte("bad \xff\xfe")) {
		t.Error("LooksLikeText")
	}
	if !HasExt("Export/Conversations.JSON", ".json") || HasExt("notes.md", ".json") || !HasExt(`dir\file.TXT`, ".txt") {
		t.Error("HasExt")
	}
	if !MediaTypeIs("text/html; charset=utf-8", "text/html") || MediaTypeIs("", "text/html") || !MediaTypeIs("TEXT/PLAIN", "text/plain") {
		t.Error("MediaTypeIs")
	}
}
