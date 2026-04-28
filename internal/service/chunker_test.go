package service

import (
	"testing"

	"code-rag-assistant/internal/util"
)

func TestChunkSourceFileSplitsGoSymbols(t *testing.T) {
	file := util.SourceFile{
		Path: "mq/consumer.go",
		Content: `package mq

type Consumer struct {
	name string
}

func NewConsumer() *Consumer {
	return &Consumer{}
}

func (c *Consumer) HandleMessage() error {
	return nil
}
`,
	}

	chunks := ChunkSourceFile(file, 80, 0)

	want := map[string]string{
		"Consumer":               "type",
		"NewConsumer":            "function",
		"Consumer.HandleMessage": "function",
	}
	for symbol, symbolType := range want {
		chunk := findChunk(chunks, symbol)
		if chunk == nil {
			t.Fatalf("ChunkSourceFile() symbols = %#v, want symbol %q", chunks, symbol)
		}
		if chunk.SymbolType != symbolType {
			t.Fatalf("chunk %q type = %q, want %q", symbol, chunk.SymbolType, symbolType)
		}
		if chunk.Language != "go" {
			t.Fatalf("chunk %q language = %q, want go", symbol, chunk.Language)
		}
	}
}

func TestChunkContentUsesOverlap(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5"

	chunks := ChunkContent(content, 3, 1)

	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 3 {
		t.Fatalf("first chunk range = %d-%d, want 1-3", chunks[0].StartLine, chunks[0].EndLine)
	}
	if chunks[1].StartLine != 3 || chunks[1].EndLine != 5 {
		t.Fatalf("second chunk range = %d-%d, want 3-5", chunks[1].StartLine, chunks[1].EndLine)
	}
}

func findChunk(chunks []Chunk, symbol string) *Chunk {
	for i := range chunks {
		if chunks[i].SymbolName == symbol {
			return &chunks[i]
		}
	}
	return nil
}
