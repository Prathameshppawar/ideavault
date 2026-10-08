package providers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// maxEmbedBatch is the maximum number of inputs sent per embeddings request.
const maxEmbedBatch = 96

// OpenAIEmbedConfig configures an OpenAI-compatible embeddings backend
// (OpenAI text-embedding-3-*, Ollama nomic-embed-text, LM Studio, ...).
type OpenAIEmbedConfig struct {
	// ID is the registry provider key ("openai", "ollama", ...).
	ID string
	// BaseURL is the API root including the version segment. Empty selects
	// the default for ID.
	BaseURL string
	// APIKey is sent as a bearer token when non-empty. Required for hosted IDs.
	APIKey string
	// Model is the embedding model, e.g. "text-embedding-3-small" or
	// "nomic-embed-text".
	Model string
	// Dimensions, when > 0, is sent as the "dimensions" request parameter
	// (supported by OpenAI text-embedding-3-*). Leave 0 for models that are
	// natively models.EmbeddingDims wide (e.g. nomic-embed-text) or that
	// reject the parameter. Returned vectors must be EmbeddingDims wide either way.
	Dimensions int
	// BatchSize caps inputs per request (default and maximum 96).
	BatchSize int
	// HTTPClient overrides the HTTP client.
	HTTPClient *http.Client
	// ExtraHeaders are added to every request.
	ExtraHeaders map[string]string
}

// OpenAIEmbedder is a models.Embedder for OpenAI-compatible /embeddings APIs.
// It is safe for concurrent use.
type OpenAIEmbedder struct {
	id         string
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	batch      int
	headers    map[string]string
	http       httpCaller
	requireKey bool
}

var _ models.Embedder = (*OpenAIEmbedder)(nil)

// NewOpenAIEmbedder returns an embedder for an OpenAI-compatible API.
func NewOpenAIEmbedder(cfg OpenAIEmbedConfig) *OpenAIEmbedder {
	id := cfg.ID
	if id == "" {
		id = "openai"
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultOpenAICompatBaseURL(id)
	}
	batch := cfg.BatchSize
	if batch <= 0 || batch > maxEmbedBatch {
		batch = maxEmbedBatch
	}
	headers := map[string]string{"Accept": "application/json"}
	for k, v := range cfg.ExtraHeaders {
		headers[k] = v
	}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	return &OpenAIEmbedder{
		id:         id,
		baseURL:    strings.TrimRight(base, "/"),
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		dimensions: cfg.Dimensions,
		batch:      batch,
		headers:    headers,
		http:       httpCaller{client: httpClientOrDefault(cfg.HTTPClient), provider: id, secrets: []string{cfg.APIKey}},
		requireKey: hostedProviderIDs[id],
	}
}

// ProviderID returns the registry provider key.
func (e *OpenAIEmbedder) ProviderID() string { return e.id }

// Model returns the embedding model name.
func (e *OpenAIEmbedder) Model() string { return e.model }

type oaEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type oaEmbedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage *struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
}

// Embed returns one models.EmbeddingDims-wide vector per text, batching
// requests. Blank texts get a zero vector without a network call (the API
// rejects empty input).
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, models.Usage, error) {
	var usage models.Usage
	if len(texts) == 0 {
		return nil, usage, nil
	}
	if e.requireKey && e.apiKey == "" {
		return nil, usage, fmt.Errorf("%s: %w", e.id, models.ErrNotConfigured)
	}
	if e.baseURL == "" || e.model == "" {
		return nil, usage, fmt.Errorf("%s: base URL and model required: %w", e.id, models.ErrNotConfigured)
	}

	out := make([][]float32, len(texts))
	var pending []int // indices of non-blank texts
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			out[i] = make([]float32, models.EmbeddingDims)
			continue
		}
		pending = append(pending, i)
	}

	for start := 0; start < len(pending); start += e.batch {
		idx := pending[start:min(start+e.batch, len(pending))]
		inputs := make([]string, len(idx))
		for j, i := range idx {
			inputs[j] = texts[i]
		}
		vecs, u, err := e.embedBatch(ctx, inputs)
		if err != nil {
			return nil, models.Usage{}, err
		}
		for j, i := range idx {
			out[i] = vecs[j]
		}
		usage.InputTokens += u.InputTokens
		usage.Estimated = usage.Estimated || u.Estimated
	}
	return out, usage, nil
}

func (e *OpenAIEmbedder) embedBatch(ctx context.Context, inputs []string) ([][]float32, models.Usage, error) {
	body := oaEmbedRequest{Model: e.model, Input: inputs, Dimensions: e.dimensions}
	resp, err := e.http.do(ctx, http.MethodPost, e.baseURL+"/embeddings", e.headers, body)
	if err != nil {
		return nil, models.Usage{}, err
	}
	var er oaEmbedResponse
	if err := e.http.decodeJSON(ctx, resp, &er); err != nil {
		return nil, models.Usage{}, err
	}
	if len(er.Data) != len(inputs) {
		return nil, models.Usage{}, e.http.errorf(0, false, "embeddings: got %d vectors for %d inputs", len(er.Data), len(inputs))
	}
	sort.SliceStable(er.Data, func(i, j int) bool { return er.Data[i].Index < er.Data[j].Index })
	vecs := make([][]float32, len(inputs))
	for i, d := range er.Data {
		if len(d.Embedding) != models.EmbeddingDims {
			return nil, models.Usage{}, e.http.errorf(0, false,
				"embeddings: model %q returned %d dimensions, want %d (set Dimensions or choose a %d-dim model)",
				e.model, len(d.Embedding), models.EmbeddingDims, models.EmbeddingDims)
		}
		vecs[i] = d.Embedding
	}
	var u models.Usage
	if er.Usage != nil && er.Usage.PromptTokens > 0 {
		u.InputTokens = er.Usage.PromptTokens
	} else {
		for _, in := range inputs {
			u.InputTokens += estimateTokens(in)
		}
		u.Estimated = true
	}
	return vecs, u, nil
}
