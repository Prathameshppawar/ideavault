package providers

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
)

// Identity of the local hashing embedder.
const (
	HashEmbedderProviderID = "local"
	HashEmbedderModel      = "hash-768-v1"
)

// Feature group weights for the hashing embedder.
const (
	hashWeightUnigram = 1.0
	hashWeightBigram  = 0.5
	hashWeightTrigram = 0.3
)

// HashEmbedder is a fully offline, zero-cost models.Embedder that produces
// deterministic 768-dim vectors by feature hashing.
//
// Pipeline: lower-case → Unicode-aware tokenization (letters/digits; each
// CJK ideograph/kana is its own token) → English stopword removal → light
// stemming (plural "s"/"ies", "ing", "ed") → features = word unigrams + word
// bigrams + character trigrams of each token → sublinear tf weighting
// (1 + ln tf) → signed FNV-1a hashing into 768 buckets → L2 normalization.
//
// It is lexical, not semantic: texts sharing words or word fragments score as
// similar, but synonyms and paraphrases with no lexical overlap ("car" vs
// "automobile") do not. Use it as an offline fallback or for near-duplicate
// detection, not as a substitute for a neural embedding model.
//
// Vectors from different embedders live in different spaces; never compare a
// hash-768-v1 vector with one from another model. Empty text yields the zero
// vector. HashEmbedder is stateless and safe for concurrent use.
type HashEmbedder struct{}

var _ models.Embedder = (*HashEmbedder)(nil)

// NewHashEmbedder returns the local hashing embedder ("local" / "hash-768-v1").
func NewHashEmbedder() *HashEmbedder { return &HashEmbedder{} }

// ProviderID returns "local".
func (*HashEmbedder) ProviderID() string { return HashEmbedderProviderID }

// Model returns "hash-768-v1".
func (*HashEmbedder) Model() string { return HashEmbedderModel }

// Embed returns one deterministic, L2-normalized vector per text. Usage is a
// local estimate (no cost is incurred).
func (*HashEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, models.Usage, error) {
	out := make([][]float32, len(texts))
	usage := models.Usage{Estimated: true}
	for i, t := range texts {
		if err := ctx.Err(); err != nil {
			return nil, models.Usage{}, fmt.Errorf("%s: %w", HashEmbedderProviderID, err)
		}
		out[i] = hashEmbed(t)
		usage.InputTokens += estimateTokens(t)
	}
	return out, usage, nil
}

// hashEmbed computes the embedding of a single text.
func hashEmbed(text string) []float32 {
	vec := make([]float32, models.EmbeddingDims)
	tokens := hashTokens(text)
	if len(tokens) == 0 {
		return vec
	}

	counts := map[string]float64{}
	for _, t := range tokens {
		counts["w\x00"+t]++
	}
	for i := 0; i+1 < len(tokens); i++ {
		counts["b\x00"+tokens[i]+"\x00"+tokens[i+1]]++
	}
	for _, t := range tokens {
		r := []rune("^" + t + "$")
		for i := 0; i+3 <= len(r); i++ {
			counts["c\x00"+string(r[i:i+3])]++
		}
	}

	// Accumulate in sorted order so float summation is deterministic.
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	acc := make([]float64, models.EmbeddingDims)
	for _, k := range keys {
		w := 1 + math.Log(counts[k])
		switch k[0] {
		case 'w':
			w *= hashWeightUnigram
		case 'b':
			w *= hashWeightBigram
		default:
			w *= hashWeightTrigram
		}
		h := fnv.New64a()
		h.Write([]byte(k))
		sum := h.Sum64()
		idx := sum % uint64(models.EmbeddingDims)
		if sum>>63 == 1 {
			w = -w
		}
		acc[idx] += w
	}

	var norm float64
	for _, x := range acc {
		norm += x * x
	}
	if norm == 0 {
		return vec
	}
	inv := 1 / math.Sqrt(norm)
	for i, x := range acc {
		vec[i] = float32(x * inv)
	}
	return vec
}

// hashTokens lower-cases, tokenizes, removes stopwords (unless that would
// leave nothing) and stems.
func hashTokens(text string) []string {
	raw := tokenizeUnicode(strings.ToLower(text))
	kept := make([]string, 0, len(raw))
	for _, t := range raw {
		if !hashStopwords[t] {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		kept = raw
	}
	for i, t := range kept {
		kept[i] = lightStem(t)
	}
	return kept
}

// tokenizeUnicode splits on anything that is not a letter, digit or combining
// mark. Han, Hiragana and Katakana runes become single-rune tokens (those
// scripts are written without spaces). Single-letter non-CJK tokens are dropped.
func tokenizeUnicode(s string) []string {
	var (
		tokens []string
		cur    []rune
	)
	emit := func() {
		if len(cur) == 0 {
			return
		}
		if len(cur) > 1 || unicode.IsDigit(cur[0]) {
			tokens = append(tokens, string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana):
			emit()
			tokens = append(tokens, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			cur = append(cur, r)
		default:
			emit()
		}
	}
	emit()
	return tokens
}

// lightStem strips a few common English suffixes conservatively.
func lightStem(t string) string {
	n := len(t)
	switch {
	case n > 4 && strings.HasSuffix(t, "ies"):
		return t[:n-3] + "y"
	case n > 4 && strings.HasSuffix(t, "sses"):
		return t[:n-2]
	case n >= 7 && strings.HasSuffix(t, "ing"):
		return undouble(t[:n-3])
	case n > 4 && strings.HasSuffix(t, "ed") && !strings.HasSuffix(t, "eed"):
		return undouble(t[:n-2])
	case n > 3 && strings.HasSuffix(t, "s") &&
		!strings.HasSuffix(t, "ss") && !strings.HasSuffix(t, "us") && !strings.HasSuffix(t, "is"):
		return t[:n-1]
	}
	return t
}

// undouble turns "runn" → "run", "stopp" → "stop" (but leaves "add", "fall").
func undouble(s string) string {
	n := len(s)
	if n >= 4 && s[n-1] == s[n-2] && strings.IndexByte("bdgmnprt", s[n-1]) >= 0 {
		return s[:n-1]
	}
	return s
}

// hashStopwords is a compact English stopword list.
var hashStopwords = func() map[string]bool {
	words := strings.Fields(`a about above after again against all am an and any are as at be because been
		before being below between both but by can could did do does doing down during each few for from
		further had has have having he her here hers herself him himself his how i if in into is it its
		itself just me more most my myself no nor not now of off on once only or other our ours ourselves
		out over own same she should so some such than that the their theirs them themselves then there
		these they this those through to too under until up very was we were what when where which while
		who whom why will with would you your yours yourself yourselves also its it's i'm i've don't`)
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}()
