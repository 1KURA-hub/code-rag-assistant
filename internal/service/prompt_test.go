package service

import (
	"strings"
	"testing"

	"code-rag-assistant/internal/config"
)

func TestPromptCitationsLimitsCountAndTruncatesContent(t *testing.T) {
	citations := []Citation{
		{FilePath: "a.go", Content: "1234567890"},
		{FilePath: "b.go", Content: "abcdefghij"},
		{FilePath: "c.go", Content: "unused"},
	}

	got := promptCitations(config.Config{
		PromptCitationLimit: 2,
		PromptChunkMaxChars: 4,
	}, citations)

	if len(got) != 2 {
		t.Fatalf("promptCitations() length = %d, want 2", len(got))
	}
	if !strings.Contains(got[0].Content, "1234") || !strings.Contains(got[0].Content, "已截断") {
		t.Fatalf("first prompt content = %q, want truncated content", got[0].Content)
	}
	if got[1].FilePath != "b.go" {
		t.Fatalf("second citation path = %q, want b.go", got[1].FilePath)
	}
}

func TestPromptCitationsDoesNotMutateOriginalCitations(t *testing.T) {
	citations := []Citation{{FilePath: "a.go", Content: "1234567890"}}

	got := promptCitations(config.Config{
		PromptCitationLimit: 1,
		PromptChunkMaxChars: 4,
	}, citations)

	if got[0].Content == citations[0].Content {
		t.Fatalf("prompt content was not truncated")
	}
	if citations[0].Content != "1234567890" {
		t.Fatalf("original citation content = %q, want unchanged", citations[0].Content)
	}
}
