package service

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"code-rag-assistant/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestTokenizeForEmbeddingSplitsIdentifiers(t *testing.T) {
	tokens := tokenizeForEmbedding("RedisStreamRelay handle_retry_message")

	for _, want := range []string{"redis", "stream", "relay", "redisstreamrelay", "handle", "retry", "message"} {
		if !containsString(tokens, want) {
			t.Fatalf("tokenizeForEmbedding() = %v, want %q", tokens, want)
		}
	}
}

func TestLocalEmbeddingIsNormalized(t *testing.T) {
	embedder := NewEmbedder(config.Config{EmbeddingDim: 8})

	vector := embedder.embedLocal("consumer retry message")

	if len(vector) != 8 {
		t.Fatalf("len(vector) = %d, want 8", len(vector))
	}
	var norm float64
	for _, value := range vector {
		norm += value * value
	}
	if math.Abs(math.Sqrt(norm)-1) > 0.000001 {
		t.Fatalf("vector norm = %.8f, want 1", math.Sqrt(norm))
	}
}

func TestRemoteEmbedBatchUsesResponseIndex(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		inputs, ok := req.Input.([]any)
		if !ok || len(inputs) != 2 {
			t.Fatalf("request input = %#v, want two-item array", req.Input)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"index":1,"embedding":[0,2]},{"index":0,"embedding":[3,0]}]}`)),
			Header:     make(http.Header),
		}, nil
	})}

	embedder := NewEmbedder(config.Config{
		EmbeddingProvider: "remote",
		EmbeddingModel:    "test-embedding",
		EmbeddingDim:      2,
		OpenAIAPIKey:      "test-key",
		OpenAIBaseURL:     "https://embedding.example/v1",
	})
	embedder.client = client

	vectors, err := embedder.EmbedBatch(t.Context(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("EmbedBatch() failed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("len(vectors) = %d, want 2", len(vectors))
	}
	if vectors[0][0] != 1 || vectors[0][1] != 0 {
		t.Fatalf("vectors[0] = %v, want [1 0]", vectors[0])
	}
	if vectors[1][0] != 0 || vectors[1][1] != 1 {
		t.Fatalf("vectors[1] = %v, want [0 1]", vectors[1])
	}
}
