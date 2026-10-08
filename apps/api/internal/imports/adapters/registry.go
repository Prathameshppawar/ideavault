// Package adapters wires every import adapter into a Registry that detects
// formats, unpacks export archives and routes URL imports.
package adapters

import (
	"bytes"
	"context"
	"fmt"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	chatgptv1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/chatgpt/v1"
	chatgptv2 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/chatgpt/v2"
	claudev1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/claude/v1"
	geminiv1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/gemini/v1"
	jsonconvv1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/jsonconv/v1"
	markdownv1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/markdown/v1"
	webv1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/web/v1"
)

// Input limits and detection threshold.
const (
	// MaxInputBytes caps any single import input (upload, paste or fetched body).
	MaxInputBytes = 200 << 20
	// MinConfidence is the lowest Detect score accepted.
	MinConfidence = 0.5
)

// Registry holds the adapters in priority order (most specific first); the
// order breaks ties between equal Detect scores.
type Registry struct {
	adapters []imports.Adapter
	byName   map[string]imports.Adapter
}

// NewRegistry returns a registry with every built-in adapter.
func NewRegistry() *Registry {
	return New(
		chatgptv2.New(),
		chatgptv1.New(),
		claudev1.New(),
		geminiv1.New(),
		jsonconvv1.New(),
		markdownv1.New(),
		markdownv1.NewText(),
		webv1.New(),
	)
}

// New returns a registry of the given adapters, in priority order.
func New(list ...imports.Adapter) *Registry {
	r := &Registry{byName: make(map[string]imports.Adapter, len(list))}
	for _, a := range list {
		r.adapters = append(r.adapters, a)
		r.byName[a.Name()] = a
	}
	return r
}

// Adapters returns the registered adapters in priority order.
func (r *Registry) Adapters() []imports.Adapter {
	return append([]imports.Adapter(nil), r.adapters...)
}

// Get returns the adapter with the given versioned name ("chatgpt/v1").
func (r *Registry) Get(name string) (imports.Adapter, bool) {
	a, ok := r.byName[name]
	return a, ok
}

// Detect returns the adapter with the highest confidence for in, and that
// confidence. Ties go to the more specific (earlier registered) adapter. When
// no adapter reaches MinConfidence it returns a nil adapter and the best
// score seen. ZIP archives are not inspected here; Parse unpacks them.
func (r *Registry) Detect(in imports.Input) (imports.Adapter, float64) {
	in.Data = imports.NormalizeEncoding(in.Data)
	var best imports.Adapter
	bestScore := 0.0
	for _, a := range r.adapters {
		if s := a.Detect(in); s > bestScore {
			best, bestScore = a, s
		}
	}
	if bestScore < MinConfidence {
		return nil, bestScore
	}
	return best, bestScore
}

// Parse parses in with the named adapter, or with the detected one when
// adapterName is empty. ZIP archives (official ChatGPT/Claude exports,
// Google Takeout) are unpacked in memory transparently. Malformed items of a
// valid export are reported in ParseResult.Failures, so a nil error may come
// with zero conversations when every item failed.
func (r *Registry) Parse(ctx context.Context, in imports.Input, adapterName string) (*imports.ParseResult, error) {
	if len(in.Data) > MaxInputBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds the %d MiB limit", imports.ErrTooLarge, len(in.Data), MaxInputBytes>>20)
	}
	if len(bytes.TrimSpace(in.Data)) == 0 {
		return nil, imports.ErrEmptyInput
	}
	if isZip(in.Data) {
		return r.parseZip(ctx, in, adapterName)
	}
	return r.parsePlain(ctx, in, adapterName)
}

// parsePlain parses a non-archive input.
func (r *Registry) parsePlain(ctx context.Context, in imports.Input, adapterName string) (*imports.ParseResult, error) {
	in.Data = imports.NormalizeEncoding(in.Data)
	a, err := r.pick(in, adapterName)
	if err != nil {
		return nil, err
	}
	res, err := a.Parse(ctx, in)
	if err != nil {
		return nil, err
	}
	return finish(res, a, in), nil
}

func (r *Registry) pick(in imports.Input, adapterName string) (imports.Adapter, error) {
	if adapterName != "" {
		a, ok := r.Get(adapterName)
		if !ok {
			return nil, fmt.Errorf("%w: unknown adapter %q", imports.ErrUnsupportedFormat, adapterName)
		}
		return a, nil
	}
	a, _ := r.Detect(in)
	if a == nil {
		return nil, fmt.Errorf("%w: could not recognize the input; supported: ChatGPT/Claude exports (zip or conversations.json), Gemini Takeout MyActivity.json, ChatGPT share links, JSON messages, Markdown, plain text and web pages", imports.ErrUnsupportedFormat)
	}
	return a, nil
}

// finish applies registry-wide post-processing: labels, URL and a final
// sanitization pass (adapters already sanitize; this is defense in depth).
func finish(res *imports.ParseResult, a imports.Adapter, in imports.Input) *imports.ParseResult {
	if res.Adapter == "" {
		res.Adapter = a.Name()
	}
	if res.Provider == "" {
		res.Provider = a.Provider()
	}
	for i := range res.Conversations {
		c := &res.Conversations[i]
		if c.Adapter == "" {
			c.Adapter = res.Adapter
		}
		if c.Provider == "" {
			c.Provider = res.Provider
		}
		if c.URL == "" && in.URL != "" && len(res.Conversations) == 1 {
			c.URL = in.URL
		}
	}
	imports.SanitizeResult(res)
	return res
}
