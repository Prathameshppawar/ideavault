package providers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// DefaultGeminiEmbedModel is the default Gemini embedding model.
const DefaultGeminiEmbedModel = "gemini-embedding-001"

// GeminiEmbedConfig configures the Gemini embeddings backend.
type GeminiEmbedConfig struct {
	// ID is the registry provider key (default "gemini").
	ID string
	// BaseURL overrides the API root (default DefaultGeminiBaseURL).
	BaseURL string
	// APIKey is sent in the x-goog-api-key header (required).
	APIKey string
	// Model defaults to DefaultGeminiEmbedModel.
	Model string
	// TaskType defaults to "RETRIEVAL_DOCUMENT".
	TaskType string
	// BatchSize caps inputs per request (default and maximum 96).
	BatchSize int
	// HTTPClient overrides the HTTP client.
	HTTPClient *http.Client
	// ExtraHeaders are added to every request.
	ExtraHeaders map[string]string
}

// GeminiEmbedder is a models.Embedder using Gemini batchEmbedContents with
// outputDimensionality = models.EmbeddingDims. Vectors are L2-normalized
// (Google recommends normalizing truncated gemini-embedding-001 outputs).
// It is safe for concurrent use.
type GeminiEmbedder struct {
	id       string
	baseURL  string
	apiKey   string
	model    string
	taskType string
	batch    int
	headers  map[string]string
	http     httpCaller
}

var _ models.Embedder = (*GeminiEmbedder)(nil)

// NewGeminiEmbedder returns a Gemini embedder.
func NewGeminiEmbedder(cfg GeminiEmbedConfig) *GeminiEmbedder {
	id := cfg.ID
	if id == "" {
		id = "gemini"
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultGeminiBaseURL
	}
	model := strings.TrimPrefix(cfg.Model, "models/")
	if model == "" {
		model = DefaultGeminiEmbedModel
	}
	task := cfg.TaskType
	if task == "" {
		task = "RETRIEVAL_DOCUMENT"
	}
	batch := cfg.BatchSize
	if batch <= 0 || batch > maxEmbedBatch {
		batch = maxEmbedBatch
	}
	return &GeminiEmbedder{
		id:       id,
		baseURL:  strings.TrimRight(base, "/"),
		apiKey:   cfg.APIKey,
		model:    model,
		taskType: task,
		batch:    batch,
		headers:  geminiHeaders(cfg.APIKey, cfg.ExtraHeaders),
		http:     httpCaller{client: httpClientOrDefault(cfg.HTTPClient), provider: id, secrets: []string{cfg.APIKey}},
	}
}

// ProviderID returns the registry provider key.
func (e *GeminiEmbedder) ProviderID() string { return e.id }

// Model returns the embedding model name.
func (e *GeminiEmbedder) Model() string { return e.model }

type gemEmbedRequest struct {
	Requests []gemEmbedItem `json:"requests"`
}

type gemEmbedItem struct {
	Model                string     `json:"model"`
	Content              gemContent `json:"content"`
	TaskType             string     `json:"taskType,omitempty"`
	OutputDimensionality int        `json:"outputDimensionality"`
}

type gemEmbedResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}

// Embed returns one models.EmbeddingDims-wide vector per text. Blank texts
// get a zero vector without a network call. Gemini does not report token
// usage for embeddings, so usage is estimated.
func (e *GeminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, models.Usage, error) {
	usage := models.Usage{Estimated: true}
	if len(texts) == 0 {
		return nil, models.Usage{}, nil
	}
	if e.apiKey == "" {
		return nil, models.Usage{}, fmt.Errorf("%s: %w", e.id, models.ErrNotConfigured)
	}
	out := make([][]float32, len(texts))
	var pending []int
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			out[i] = make([]float32, models.EmbeddingDims)
			continue
		}
		pending = append(pending, i)
		usage.InputTokens += estimateTokens(t)
	}
	endpoint := geminiModelURL(e.baseURL, e.model, "batchEmbedContents")
	for start := 0; start < len(pending); start += e.batch {
		idx := pending[start:min(start+e.batch, len(pending))]
		body := gemEmbedRequest{Requests: make([]gemEmbedItem, len(idx))}
		for j, i := range idx {
			body.Requests[j] = gemEmbedItem{
				Model:                "models/" + e.model,
				Content:              gemContent{Parts: []gemPart{{Text: texts[i]}}},
				TaskType:             e.taskType,
				OutputDimensionality: models.EmbeddingDims,
			}
		}
		resp, err := e.http.do(ctx, http.MethodPost, endpoint, e.headers, body)
		if err != nil {
			return nil, models.Usage{}, err
		}
		var er gemEmbedResponse
		if err := e.http.decodeJSON(ctx, resp, &er); err != nil {
			return nil, models.Usage{}, err
		}
		if len(er.Embeddings) != len(idx) {
			return nil, models.Usage{}, e.http.errorf(0, false, "embeddings: got %d vectors for %d inputs", len(er.Embeddings), len(idx))
		}
		for j, i := range idx {
			v := er.Embeddings[j].Values
			if len(v) != models.EmbeddingDims {
				return nil, models.Usage{}, e.http.errorf(0, false,
					"embeddings: model %q returned %d dimensions, want %d", e.model, len(v), models.EmbeddingDims)
			}
			out[i] = l2Normalize(v)
		}
	}
	return out, usage, nil
}

// l2Normalize scales v to unit length in place (zero vectors are unchanged).
func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
		return v
	}
	inv := 1 / math.Sqrt(sum)
	for i, x := range v {
		v[i] = float32(float64(x) * inv)
	}
	return v
}
