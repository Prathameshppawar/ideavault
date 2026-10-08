package chatgptv2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

// turboEncoder is a minimal test-only turbo-stream encoder: it flattens a
// JSON-like value into the index-addressed array React Router emits,
// deduplicating primitives. {"$date": ms} encodes a Date and {"$promise":
// true} a pending promise, mirroring what real share pages contain.
type turboEncoder struct {
	values []any
	prims  map[string]int
}

func encodeTurbo(v any) []any {
	e := &turboEncoder{prims: map[string]int{}}
	e.encode(v)
	return e.values
}

func (e *turboEncoder) reserve() int {
	e.values = append(e.values, nil)
	return len(e.values) - 1
}

func (e *turboEncoder) encode(v any) int {
	switch x := v.(type) {
	case nil:
		return tsNull
	case bool, float64, string:
		key := fmt.Sprintf("%T:%v", x, x)
		if i, ok := e.prims[key]; ok {
			return i
		}
		i := e.reserve()
		e.values[i] = x
		e.prims[key] = i
		return i
	case []any:
		i := e.reserve()
		idx := make([]any, len(x))
		for j, el := range x {
			idx[j] = e.encode(el)
		}
		e.values[i] = idx
		return i
	case map[string]any:
		if ms, ok := x["$date"]; ok && len(x) == 1 {
			i := e.reserve()
			e.values[i] = []any{"D", ms}
			return i
		}
		if _, ok := x["$promise"]; ok && len(x) == 1 {
			i := e.reserve()
			e.values[i] = []any{"P", float64(i)}
			return i
		}
		i := e.reserve()
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		obj := map[string]any{}
		for _, k := range keys {
			ki := e.encode(k)
			obj[fmt.Sprintf("_%d", ki)] = e.encode(x[k])
		}
		e.values[i] = obj
		return i
	}
	panic(fmt.Sprintf("unsupported type %T", v))
}

// buildSharePage renders a synthetic share page around the encoded source.
func buildSharePage(t *testing.T, source any) []byte {
	t.Helper()
	flat, err := json.Marshal(encodeTurbo(source))
	if err != nil {
		t.Fatal(err)
	}
	chunk1, _ := json.Marshal(string(flat) + "\n")
	chunk2, _ := json.Marshal("P9999:[{}]\n")
	var b bytes.Buffer
	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"/>
<title>ChatGPT - Weekend ride route voting</title>
<meta property="og:title" content="Check out this chat"/>
</head><body><div id="root"><main><p>Synthetic share page fixture for IdeaVault tests.</p></main></div>
<script nonce="fixture">window.__reactRouterContext = {"basename":"/","future":{},"isSpaMode":false};window.__reactRouterContext.stream = new ReadableStream({start(controller){window.__reactRouterContext.streamController = controller;}}).pipeThrough(new TextEncoderStream());</script>
<script nonce="fixture">window.__reactRouterContext.streamController.enqueue(`)
	b.Write(chunk1)
	b.WriteString(`);</script>
<script nonce="fixture">window.__reactRouterContext.streamController.enqueue(`)
	b.Write(chunk2)
	b.WriteString(`);</script>
<script nonce="fixture">window.__reactRouterContext.streamController.close();</script>
</body></html>
`)
	return b.Bytes()
}

func loadShareSource(t *testing.T) any {
	t.Helper()
	var source any
	if err := json.Unmarshal(fixtures.Read(t, "chatgpt", "share_page_source.json"), &source); err != nil {
		t.Fatal(err)
	}
	return source
}

// TestSharePageFixtureUpToDate guarantees share_page.html is exactly the
// encoding of share_page_source.json. Regenerate with
// IDEAVAULT_UPDATE_FIXTURES=1 go test ./internal/imports/chatgpt/v2/ -run TestSharePageFixtureUpToDate
func TestSharePageFixtureUpToDate(t *testing.T) {
	want := buildSharePage(t, loadShareSource(t))
	path := fixtures.Path(t, "chatgpt", "share_page.html")
	if os.Getenv("IDEAVAULT_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with IDEAVAULT_UPDATE_FIXTURES=1)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; regenerate with IDEAVAULT_UPDATE_FIXTURES=1", path)
	}
}

// TestSharePageFixtureRoundTrip decodes the committed page with the
// production decoder and compares it with the source data.
func TestSharePageFixtureRoundTrip(t *testing.T) {
	source := loadShareSource(t).(map[string]any)
	payloads := extractEnqueuePayloads(string(fixtures.Read(t, "chatgpt", "share_page.html")))
	if len(payloads) != 2 {
		t.Fatalf("got %d enqueue payloads, want 2", len(payloads))
	}
	ts, err := parseTurboStream(payloads[0] + payloads[1])
	if err != nil {
		t.Fatal(err)
	}
	data, shareID := findShareData(ts)
	wantRoute := source["loaderData"].(map[string]any)["routes/share.$shareId.($action)"].(map[string]any)
	wantData := wantRoute["serverResponse"].(map[string]any)["data"]
	if !reflect.DeepEqual(data, wantData) {
		t.Fatalf("decoded share data differs from source")
	}
	if shareID != wantRoute["sharedConversationId"] {
		t.Fatalf("shareID = %q", shareID)
	}
	root := ts.root().(map[string]any)
	dd := root["loaderData"].(map[string]any)["root"].(map[string]any)["dd"].(map[string]any)
	if dd["traceTime"] != "2026-01-03T00:00:50Z" {
		t.Errorf("Date typed value decoded to %v", dd["traceTime"])
	}
}
