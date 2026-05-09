package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"unicode"
)

// EmbedText returns a 384-dimension float64 vector for the given text.
// It tries the configured embedding API first; on failure it falls back
// to a TF-IDF bag-of-words vector (also normalised to 384 dims).
func EmbedText(text string) ([]float64, error) {
	vec, err := apiEmbed(text)
	if err == nil {
		return vec, nil
	}
	// Fallback: TF-IDF bag-of-words hashed into 384 buckets.
	return tfidfEmbed(text), nil
}

// CosineSimilarity returns the cosine similarity between two equal-length vectors.
// Returns 0 if either vector has zero magnitude.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

// EncodeEmbedding serialises a float64 slice to a compact JSON string.
func EncodeEmbedding(v []float64) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeEmbedding deserialises a JSON string into a float64 slice.
func DecodeEmbedding(s string) ([]float64, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var v []float64
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// ──────────────────────────────────────────────────────────────
// API embedding (OpenAI-compatible endpoint)
// ──────────────────────────────────────────────────────────────

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func apiEmbed(text string) ([]float64, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	baseURL := os.Getenv("WLLR_EMBED_URL")
	if baseURL == "" {
		if apiKey == "" {
			return nil, fmt.Errorf("no embedding API key set")
		}
		baseURL = "https://api.openai.com/v1/embeddings"
	}

	model := os.Getenv("WLLR_EMBED_MODEL")
	if model == "" {
		model = "text-embedding-3-small"
	}

	body, _ := json.Marshal(embedRequest{Model: model, Input: text})
	req, err := http.NewRequest("POST", baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed API %d: %s", resp.StatusCode, b)
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Data) == 0 || len(er.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embed API: empty response")
	}
	return er.Data[0].Embedding, nil
}

// ──────────────────────────────────────────────────────────────
// TF-IDF bag-of-words fallback (384-bucket hash projection)
// ──────────────────────────────────────────────────────────────

const tfidfDims = 384

func tfidfEmbed(text string) []float64 {
	tokens := tokenise(text)
	if len(tokens) == 0 {
		return make([]float64, tfidfDims)
	}

	// Count term frequencies.
	tf := make(map[string]float64)
	for _, t := range tokens {
		tf[t]++
	}
	// Normalise by document length.
	n := float64(len(tokens))
	for k := range tf {
		tf[k] /= n
	}

	// Sort terms for determinism.
	terms := make([]string, 0, len(tf))
	for k := range tf {
		terms = append(terms, k)
	}
	sort.Strings(terms)

	// Project into tfidfDims buckets using FNV-style hash.
	vec := make([]float64, tfidfDims)
	for _, term := range terms {
		bucket := fnvHash(term) % tfidfDims
		vec[bucket] += tf[term]
	}

	// L2-normalise.
	var mag float64
	for _, v := range vec {
		mag += v * v
	}
	if mag > 0 {
		mag = math.Sqrt(mag)
		for i := range vec {
			vec[i] /= mag
		}
	}
	return vec
}

func tokenise(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var buf strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(r)
		} else {
			if buf.Len() > 1 {
				tokens = append(tokens, buf.String())
			}
			buf.Reset()
		}
	}
	if buf.Len() > 1 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

func fnvHash(s string) int {
	const (
		prime  = 16777619
		offset = 2166136261
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	// Make positive.
	return int(h & 0x7fffffff)
}
