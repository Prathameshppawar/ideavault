package models

import (
	"context"
	"testing"

	rds "github.com/Prathameshppawar/ideavault/apps/api/internal/repository/redis"
)

type countingEmbedder struct {
	model string
	calls int
}

func (e *countingEmbedder) ProviderID() string { return "test" }
func (e *countingEmbedder) Model() string      { return e.model }
func (e *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, Usage, error) {
	e.calls++
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, EmbeddingDims)
		v[0], v[1] = float32(len(t)), -0.5
		v[EmbeddingDims-1] = 0.125
		out[i] = v
	}
	return out, Usage{InputTokens: 3}, nil
}

func TestEmbedQueryReusesCachedVector(t *testing.T) {
	ctx := context.Background()
	emb := &countingEmbedder{model: "m1"}
	g := NewGateway(nil, nil, nil)
	g.SetEmbedder(emb)
	g.SetCache(rds.NewMemoryCache())

	first, err := g.EmbedQuery(ctx, nil, "biker trips")
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.EmbedQuery(ctx, nil, "biker trips")
	if err != nil {
		t.Fatal(err)
	}
	if emb.calls != 1 {
		t.Fatalf("embedder called %d times, want 1 (second query should hit the cache)", emb.calls)
	}
	if len(second) != EmbeddingDims || second[0] != first[0] || second[1] != -0.5 || second[EmbeddingDims-1] != 0.125 {
		t.Fatalf("cached vector does not round-trip: %v… vs %v…", second[:2], first[:2])
	}

	// Different text, or a different embedding model, must not reuse the vector.
	if _, err := g.EmbedQuery(ctx, nil, "clinic scheduling"); err != nil {
		t.Fatal(err)
	}
	g.SetEmbedder(&countingEmbedder{model: "m2"})
	if _, err := g.EmbedQuery(ctx, nil, "biker trips"); err != nil {
		t.Fatal(err)
	}
	if emb.calls != 2 {
		t.Fatalf("embedder m1 called %d times, want 2", emb.calls)
	}
}

func TestEmbedQueryWithoutCacheOrEmbedder(t *testing.T) {
	g := NewGateway(nil, nil, nil)
	if _, err := g.EmbedQuery(context.Background(), nil, "x"); err != ErrNotConfigured {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
	emb := &countingEmbedder{model: "m1"}
	g.SetEmbedder(emb)
	for range 2 {
		if _, err := g.EmbedQuery(context.Background(), nil, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if emb.calls != 2 {
		t.Fatalf("without a cache every call embeds; got %d calls", emb.calls)
	}
}
