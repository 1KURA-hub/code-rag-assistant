package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"code-rag-assistant/internal/config"
)

type Embedder struct {
	cfg    config.Config
	dim    int
	client *http.Client
}

var tokenPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*|\d+`)

func NewEmbedder(cfg config.Config) *Embedder {
	return &Embedder{cfg: cfg, dim: cfg.EmbeddingDim, client: http.DefaultClient}
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float64, error) {
	vectors, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("embedding response count mismatch: got %d want 1", len(vectors))
	}
	return vectors[0], nil
}

func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if e.cfg.EmbeddingProvider == "remote" {
		if e.cfg.OpenAIAPIKey == "" {
			return nil, errors.New("OPENAI_API_KEY is not configured")
		}
		return e.embedRemoteBatch(ctx, texts)
	}
	vectors := make([][]float64, len(texts))
	for idx, text := range texts {
		vectors[idx] = e.embedLocal(text)
	}
	return vectors, nil
}

func (e *Embedder) embedLocal(text string) []float64 {
	vector := make([]float64, e.dim)
	tokens := tokenizeForEmbedding(text)
	for _, token := range tokens {
		h := fnv.New32a()
		_, _ = h.Write([]byte(token))
		idx := int(h.Sum32() % uint32(e.dim))
		vector[idx] += 1
	}
	var norm float64
	for _, value := range vector {
		norm += value * value
	}
	if norm == 0 {
		return vector
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] = vector[i] / norm
	}
	return vector
}

type embeddingRequest struct {
	Model      string `json:"model"`
	Input      any    `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (e *Embedder) embedRemoteBatch(ctx context.Context, texts []string) ([][]float64, error) {
	body, err := json.Marshal(embeddingRequest{
		Model:      e.cfg.EmbeddingModel,
		Input:      texts,
		Dimensions: e.dim,
	})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(e.cfg.OpenAIBaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.cfg.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedding request failed: status %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var decoded embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) == 0 {
		return nil, errors.New("embedding response is empty")
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("embedding response count mismatch: got %d want %d", len(decoded.Data), len(texts))
	}
	useResponseOrder := false
	seen := make([]bool, len(texts))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(texts) || seen[item.Index] {
			useResponseOrder = true
			break
		}
		seen[item.Index] = true
	}
	vectors := make([][]float64, len(texts))
	for pos, item := range decoded.Data {
		target := item.Index
		if useResponseOrder {
			target = pos
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("embedding response item %d is empty", target)
		}
		if len(item.Embedding) != e.dim {
			return nil, fmt.Errorf("embedding dimension mismatch at item %d: got %d want %d", target, len(item.Embedding), e.dim)
		}
		vectors[target] = normalizeVector(item.Embedding)
	}
	return vectors, nil
}

func normalizeVector(vector []float64) []float64 {
	var norm float64
	for _, value := range vector {
		norm += value * value
	}
	if norm == 0 {
		return vector
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] = vector[i] / norm
	}
	return vector
}

func tokenizeForEmbedding(text string) []string {
	raw := tokenPattern.FindAllString(text, -1)
	tokens := make([]string, 0, len(raw)*2)
	for _, token := range raw {
		for _, part := range splitIdentifier(token) {
			if part != "" {
				tokens = append(tokens, part)
			}
		}
	}
	return tokens
}

func splitIdentifier(token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	var parts []string
	for _, piece := range strings.FieldsFunc(token, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	}) {
		pieceParts := splitCamel(piece)
		parts = append(parts, pieceParts...)
		if len(pieceParts) > 1 {
			parts = append(parts, strings.ToLower(piece))
		}
	}
	return parts
}

func splitCamel(value string) []string {
	if value == "" {
		return nil
	}
	runes := []rune(value)
	var parts []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev := runes[i-1]
		curr := runes[i]
		nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
		if unicode.IsLower(prev) && unicode.IsUpper(curr) || unicode.IsUpper(prev) && unicode.IsUpper(curr) && nextLower {
			parts = append(parts, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}
	parts = append(parts, strings.ToLower(string(runes[start:])))
	return parts
}

func VectorLiteral(vector []float64) string {
	parts := make([]string, len(vector))
	for i, value := range vector {
		parts[i] = fmt.Sprintf("%.6f", value)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
