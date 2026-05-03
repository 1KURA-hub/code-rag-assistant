package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBoostPrioritizesSymbolNameMentionedInQuery(t *testing.T) {
	rows := []Citation{
		{
			FilePath:   "internal/service/other.go",
			SymbolName: "Other",
			SymbolType: "function",
			Content:    "func Other() {}",
			Score:      0.80,
		},
		{
			FilePath:   "internal/service/ingest.go",
			SymbolName: "CreateAndIndex",
			SymbolType: "function",
			Content:    "func (s *IngestService) CreateAndIndex() {}",
			Score:      0.70,
		},
	}

	boost(rows, "CreateAndIndex 是怎么实现的", nil)

	if rows[0].SymbolName != "CreateAndIndex" {
		t.Fatalf("boost() ranked %q first, want CreateAndIndex", rows[0].SymbolName)
	}
}

func TestBoostPrioritizesFilePathMentionedInQuery(t *testing.T) {
	rows := []Citation{
		{
			FilePath:   "internal/service/answer.go",
			SymbolName: "Ask",
			SymbolType: "method",
			Content:    "func (s *AnswerService) Ask() {}",
			Score:      0.82,
		},
		{
			FilePath:   "mq/consumer.go",
			SymbolName: "handleRetryOrDLQ",
			SymbolType: "function",
			Content:    "func handleRetryOrDLQ() {}",
			Score:      0.70,
		},
	}

	boost(rows, "mq/consumer.go 里的重试逻辑是什么", nil)

	if rows[0].FilePath != "mq/consumer.go" {
		t.Fatalf("boost() ranked %q first, want mq/consumer.go", rows[0].FilePath)
	}
}

func TestBoostPrioritizesLanguageMentionedInQuery(t *testing.T) {
	rows := []Citation{
		{
			FilePath:   ".github/workflows/ci.yml",
			Language:   "yaml",
			SymbolType: "chunk",
			Content:    "go test ./...",
			Score:      0.80,
		},
		{
			FilePath:   "internal/service/chunker.go",
			Language:   "go",
			SymbolName: "ChunkSourceFile",
			SymbolType: "function",
			Content:    "func ChunkSourceFile() {}",
			Score:      0.76,
		},
	}

	boost(rows, "Go 文件是怎么分片的", nil)

	if rows[0].Language != "go" {
		t.Fatalf("boost() ranked language %q first, want go", rows[0].Language)
	}
}

func TestKeywordSearchTermsIncludePathsAndSymbols(t *testing.T) {
	features := analyzeSearchFeatures("mq/consumer.go 里的 handleRetryOrDLQ 怎么处理死信", nil)
	terms := keywordSearchTerms(features)

	for _, want := range []string{"mq/consumer.go", "handleretryordlq"} {
		if !containsString(terms, want) {
			t.Fatalf("keywordSearchTerms() = %v, want %q", terms, want)
		}
	}
}

func TestMergeCitationsDeduplicatesByID(t *testing.T) {
	rows := mergeCitations(
		[]Citation{{ID: 1, FilePath: "a.go", Score: 0.3}},
		[]Citation{{ID: 1, FilePath: "a.go", Score: 0.8}, {ID: 2, FilePath: "b.go", Score: 0.5}},
	)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Score != 0.8 {
		t.Fatalf("deduped score = %.2f, want 0.80", rows[0].Score)
	}
}

func TestRetrievalEvalSet(t *testing.T) {
	cases := loadRetrievalEvalCases(t)
	var hits int
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			rows := append([]Citation(nil), tc.Candidates...)
			boost(rows, tc.Question, tc.Hints)

			topK := tc.TopK
			if topK <= 0 || topK > len(rows) {
				topK = len(rows)
			}
			if !containsAnyExpectedSymbol(rows[:topK], tc.ExpectedSymbols) {
				t.Fatalf("top %d symbols = %v, want one of %v", topK, citationSymbols(rows[:topK]), tc.ExpectedSymbols)
			}
			hits++
		})
	}
	hitRate := float64(hits) / float64(len(cases))
	t.Logf("retrieval eval topK hit rate: %.2f (%d/%d)", hitRate, hits, len(cases))
	if hitRate < 0.90 {
		t.Fatalf("retrieval eval hit rate = %.2f, want >= 0.90", hitRate)
	}
}

type retrievalEvalCase struct {
	Name            string     `json:"name"`
	Question        string     `json:"question"`
	Hints           []string   `json:"hints"`
	TopK            int        `json:"top_k"`
	ExpectedSymbols []string   `json:"expected_symbols"`
	Candidates      []Citation `json:"candidates"`
}

func loadRetrievalEvalCases(t *testing.T) []retrievalEvalCase {
	t.Helper()

	path := filepath.Join("testdata", "retrieval_eval.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read retrieval eval set: %v", err)
	}
	var cases []retrievalEvalCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode retrieval eval set: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("retrieval eval set is empty")
	}
	return cases
}

func containsAnyExpectedSymbol(rows []Citation, expected []string) bool {
	for _, row := range rows {
		for _, symbol := range expected {
			if row.SymbolName == symbol {
				return true
			}
		}
	}
	return false
}

func citationSymbols(rows []Citation) []string {
	symbols := make([]string, 0, len(rows))
	for _, row := range rows {
		symbols = append(symbols, row.SymbolName)
	}
	return symbols
}
