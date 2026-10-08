package adapters

import (
	"context"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

// FuzzParse feeds arbitrary bytes (seeded with every fixture and truncated
// variants) through detection and every adapter: nothing may panic, and any
// result must be sanitized. Run with: go test -fuzz FuzzParse ./internal/imports/adapters/
func FuzzParse(f *testing.F) {
	seeds := [][]string{
		{"chatgpt", "conversations.json"}, {"chatgpt", "share_page.html"}, {"claude", "conversations.json"},
		{"gemini", "MyActivity.json"}, {"json", "messages.json"}, {"markdown", "chatgpt_copy.md"},
		{"markdown", "notes.md"}, {"text", "transcript.txt"}, {"injection", "malicious_chatgpt.json"},
	}
	for _, s := range seeds {
		data := fixtures.Read(f, s...)
		f.Add(data)
		f.Add(data[:len(data)/2])
		f.Add(data[:len(data)/3])
	}
	f.Add([]byte(`[{"_1":0}]`))
	f.Add([]byte("PK\x03\x04"))
	r := NewRegistry()
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		check := func(res *imports.ParseResult) {
			if res == nil {
				return
			}
			for _, c := range res.Conversations {
				if len(c.Messages) == 0 {
					t.Fatalf("empty conversation returned")
				}
			}
		}
		res, _ := r.Parse(ctx, imports.Input{Data: data}, "")
		check(res)
		for _, a := range r.Adapters() {
			_ = a.Detect(imports.Input{Data: data})
			res, _ := a.Parse(ctx, imports.Input{Data: data})
			check(res)
		}
	})
}
