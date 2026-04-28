package service

import (
	"math"
	"testing"

	"code-rag-assistant/internal/config"
)

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
