package service

import "testing"

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
