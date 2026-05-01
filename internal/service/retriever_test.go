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

func TestRetrievalEvalSet(t *testing.T) {
	cases := loadRetrievalEvalCases(t)
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
		})
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
