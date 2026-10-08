package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// fakeVector returns a dims-wide vector whose first element encodes seed.
func fakeVector(seed float32, dims int) []float32 {
	v := make([]float32, dims)
	v[0] = seed
	for i := 1; i < dims; i++ {
		v[i] = 0.001
	}
	return v
}

// openAIEmbedHandler answers /embeddings with one vector per input, in
// REVERSED index order (to verify reordering), encoding "text N" as N in
// element 0. dims sets the returned width.
func openAIEmbedHandler(t *testing.T, dims int) func(http.ResponseWriter, *http.Request, int) {
	return func(w http.ResponseWriter, r *http.Request, call int) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad body: %v", err)
		}
		var data []any
		for i := len(req.Input) - 1; i >= 0; i-- {
			var n float32
			fmt.Sscanf(req.Input[i], "text %g", &n)
			data = append(data, map[string]any{"index": i, "embedding": fakeVector(n, dims)})
		}
		writeJSON(w, 200, mustJSON(map[string]any{"data": data, "usage": map[string]any{"prompt_tokens": 2 * len(req.Input)}}))
	}
}

func TestOpenAIEmbedderBatchingOrderAndUsage(t *testing.T) {
	srv := newFakeServer(t, openAIEmbedHandler(t, models.EmbeddingDims))
	e := NewOpenAIEmbedder(OpenAIEmbedConfig{ID: "openai", BaseURL: srv.URL, APIKey: "sk-test-1234567890abcdef", Model: "text-embedding-3-small", Dimensions: 768})
	texts := make([]string, 200)
	for i := range texts {
		texts[i] = fmt.Sprintf("text %d", i)
	}
	texts[5] = "   " // blank -> zero vector, not sent

	vecs, usage, err := e.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 200 {
		t.Fatalf("got %d vectors", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != models.EmbeddingDims {
			t.Fatalf("vector %d has %d dims", i, len(v))
		}
		want := float32(i)
		if i == 5 {
			want = 0
		}
		if v[0] != want {
			t.Errorf("vector %d out of order: v[0]=%v", i, v[0])
		}
	}
	reqs := srv.requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3 (96+96+7)", len(reqs))
	}
	sizes := []int{96, 96, 7}
	for i, rec := range reqs {
		body := rec.JSON(t)
		if rec.Path != "/embeddings" || body["model"] != "text-embedding-3-small" || body["dimensions"] != float64(768) {
			t.Errorf("request %d = %s %v", i, rec.Path, body)
		}
		if n := len(body["input"].([]any)); n != sizes[i] {
			t.Errorf("batch %d size = %d, want %d", i, n, sizes[i])
		}
	}
	if usage.InputTokens != 2*199 || usage.Estimated {
		t.Errorf("usage = %+v", usage)
	}
	if e.ProviderID() != "openai" || e.Model() != "text-embedding-3-small" {
		t.Errorf("identity = %s/%s", e.ProviderID(), e.Model())
	}
}

func TestOpenAIEmbedderDimensionsOnlyWhenConfigured(t *testing.T) {
	srv := newFakeServer(t, openAIEmbedHandler(t, models.EmbeddingDims))
	e := NewOpenAIEmbedder(OpenAIEmbedConfig{ID: "ollama", BaseURL: srv.URL, Model: "nomic-embed-text"})
	if _, _, err := e.Embed(context.Background(), []string{"text 1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.last(t).JSON(t)["dimensions"]; ok {
		t.Error("dimensions sent although not configured")
	}
}

func TestOpenAIEmbedderRejectsWrongDimensions(t *testing.T) {
	srv := newFakeServer(t, openAIEmbedHandler(t, 1536))
	e := NewOpenAIEmbedder(OpenAIEmbedConfig{ID: "openai", BaseURL: srv.URL, APIKey: "sk-test-1234567890abcdef", Model: "text-embedding-3-small"})
	_, _, err := e.Embed(context.Background(), []string{"text 1"})
	pe := providerError(t, err)
	if pe.Retryable || pe.Message == "" {
		t.Errorf("err = %+v", pe)
	}
}

func TestOpenAIEmbedderErrorsAndConfig(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeJSON(w, 429, `{"error":{"message":"rate limited"}}`)
	})
	e := NewOpenAIEmbedder(OpenAIEmbedConfig{ID: "openai", BaseURL: srv.URL, APIKey: "sk-test-1234567890abcdef", Model: "m"})
	_, _, err := e.Embed(context.Background(), []string{"x"})
	if pe := providerError(t, err); !pe.Retryable || pe.StatusCode != 429 {
		t.Errorf("err = %+v", pe)
	}
	_, _, err = NewOpenAIEmbedder(OpenAIEmbedConfig{ID: "openai", Model: "m"}).Embed(context.Background(), []string{"x"})
	if !errors.Is(err, models.ErrNotConfigured) {
		t.Errorf("missing key: %v", err)
	}
	vecs, _, err := e.Embed(context.Background(), nil)
	if err != nil || vecs != nil {
		t.Errorf("empty input: %v %v", vecs, err)
	}
}

func TestGeminiEmbedder(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		var req struct {
			Requests []json.RawMessage `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var embs []any
		for i := range req.Requests {
			v := make([]float32, models.EmbeddingDims)
			v[0], v[1] = 3, float32(4+i) // unnormalized
			embs = append(embs, map[string]any{"values": v})
		}
		writeJSON(w, 200, mustJSON(map[string]any{"embeddings": embs}))
	})
	e := NewGeminiEmbedder(GeminiEmbedConfig{APIKey: testGeminiKey, BaseURL: srv.URL + "/v1beta"})
	vecs, usage, err := e.Embed(context.Background(), []string{"alpha note", "", "beta note"})
	if err != nil {
		t.Fatal(err)
	}
	rec := srv.last(t)
	if rec.Path != "/v1beta/models/gemini-embedding-001:batchEmbedContents" || rec.Header.Get("X-Goog-Api-Key") != testGeminiKey {
		t.Errorf("request = %s (key header %q)", rec.Path, rec.Header.Get("X-Goog-Api-Key"))
	}
	body := rec.JSON(t)
	items := body["requests"].([]any)
	if len(items) != 2 {
		t.Fatalf("sent %d items, want 2 (blank skipped)", len(items))
	}
	if path(t, items, 0, "model") != "models/gemini-embedding-001" ||
		path(t, items, 0, "outputDimensionality") != float64(768) ||
		path(t, items, 0, "taskType") != "RETRIEVAL_DOCUMENT" ||
		path(t, items, 0, "content", "parts", 0, "text") != "alpha note" {
		t.Errorf("item = %v", items[0])
	}
	if vecs[0][0] != 0.6 || math.Abs(float64(vecs[0][1])-0.8) > 1e-6 {
		t.Errorf("vector not L2-normalized: %v %v", vecs[0][0], vecs[0][1])
	}
	for _, x := range vecs[1] {
		if x != 0 {
			t.Fatal("blank text should embed to the zero vector")
		}
	}
	if !usage.Estimated || usage.InputTokens == 0 {
		t.Errorf("usage = %+v", usage)
	}
	if e.ProviderID() != "gemini" || e.Model() != DefaultGeminiEmbedModel {
		t.Errorf("identity = %s/%s", e.ProviderID(), e.Model())
	}
}

func TestGeminiEmbedderRejectsWrongDimensions(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, call int) {
		writeJSON(w, 200, mustJSON(map[string]any{"embeddings": []any{map[string]any{"values": make([]float32, 3072)}}}))
	})
	_, _, err := NewGeminiEmbedder(GeminiEmbedConfig{APIKey: testGeminiKey, BaseURL: srv.URL}).Embed(context.Background(), []string{"x"})
	providerError(t, err)
	if _, _, err := NewGeminiEmbedder(GeminiEmbedConfig{}).Embed(context.Background(), []string{"x"}); !errors.Is(err, models.ErrNotConfigured) {
		t.Errorf("missing key: %v", err)
	}
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

func TestHashEmbedder(t *testing.T) {
	e := NewHashEmbedder()
	if e.ProviderID() != "local" || e.Model() != "hash-768-v1" {
		t.Errorf("identity = %s/%s", e.ProviderID(), e.Model())
	}
	texts := []string{
		"Building a personal knowledge base with linked notes",
		"Notes linked together form a personal knowledge base I am building",
		"The recipe needs two cups of flour and an egg",
		"Building a personal knowledge base with linked notes",
		"",
		"東京の天気は晴れです",
		"東京の天気は雨です",
	}
	vecs, usage, err := e.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Estimated || usage.InputTokens == 0 {
		t.Errorf("usage = %+v", usage)
	}
	for i, v := range vecs {
		if len(v) != models.EmbeddingDims {
			t.Fatalf("vector %d has %d dims", i, len(v))
		}
		for _, x := range v {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				t.Fatalf("vector %d contains NaN/Inf", i)
			}
		}
	}

	similar, unrelated := cosine(vecs[0], vecs[1]), cosine(vecs[0], vecs[2])
	if similar <= unrelated || similar < 0.3 {
		t.Errorf("similar=%.3f unrelated=%.3f; want similar > unrelated", similar, unrelated)
	}
	if cjk, other := cosine(vecs[5], vecs[6]), cosine(vecs[5], vecs[2]); cjk <= other {
		t.Errorf("CJK similar=%.3f unrelated=%.3f", cjk, other)
	}

	// Identical text -> identical vector, unit length.
	for i := range vecs[0] {
		if vecs[0][i] != vecs[3][i] {
			t.Fatal("identical texts produced different vectors")
		}
	}
	if n := cosine(vecs[0], vecs[0]); math.Abs(n-1) > 1e-6 {
		t.Errorf("self-cosine = %v", n)
	}
	var norm float64
	for _, x := range vecs[0] {
		norm += float64(x) * float64(x)
	}
	if math.Abs(norm-1) > 1e-5 {
		t.Errorf("norm^2 = %v, want 1", norm)
	}

	// Empty text -> zero vector.
	for _, x := range vecs[4] {
		if x != 0 {
			t.Fatal("empty text should embed to the zero vector")
		}
	}

	// Deterministic across calls.
	again, _, _ := e.Embed(context.Background(), texts[:1])
	for i := range again[0] {
		if again[0][i] != vecs[0][i] {
			t.Fatal("embedding is not deterministic across calls")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := e.Embed(ctx, texts); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx: %v", err)
	}
}

func TestHashTokensAndStemming(t *testing.T) {
	got := hashTokens("The Ideas are RUNNING; walked through notes, studies & stopped!")
	// "The", "are" and "through" are stopwords; "&" is not a token.
	want := []string{"idea", "run", "walk", "note", "study", "stop"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("hashTokens = %v, want %v", got, want)
	}
	if got := hashTokens("the of and"); len(got) != 3 {
		t.Errorf("all-stopword text should keep tokens, got %v", got)
	}
}
