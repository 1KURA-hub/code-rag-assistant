package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeywordSearchTermsIncludePathsAndSymbols(t *testing.T) {
	features := analyzeSearchFeatures("mq/consumer.go 里的 handleRetryOrDLQ 怎么处理死信", nil)
	terms := keywordSearchTerms(features)

	for _, want := range []string{"mq/consumer.go", "handleretryordlq"} {
		if !containsString(terms, want) {
			t.Fatalf("keywordSearchTerms() = %v, want %q", terms, want)
		}
	}
}

func TestAnalyzeSearchFeaturesTreatsLowercaseIdentifiersAsSymbols(t *testing.T) {
	features := analyzeSearchFeatures("index函数和reclaim逻辑怎么实现", nil)

	for _, want := range []string{"index", "reclaim"} {
		if !containsString(features.Symbols, want) {
			t.Fatalf("features.Symbols = %v, want %q", features.Symbols, want)
		}
	}
	if containsString(features.Symbols, "id") {
		t.Fatalf("features.Symbols = %v, should not include short identifier %q", features.Symbols, "id")
	}
}

func TestAnalyzeSearchFeaturesTreatsLanguageWordsAsLanguages(t *testing.T) {
	features := analyzeSearchFeatures("yaml文件是干什么的", nil)

	if !containsString(features.Languages, "yaml") {
		t.Fatalf("features.Languages = %v, want yaml", features.Languages)
	}
	if containsString(features.Symbols, "yaml") {
		t.Fatalf("features.Symbols = %v, should not include language word yaml", features.Symbols)
	}

	terms := keywordSearchTerms(features)
	if containsString(terms, "yaml") {
		t.Fatalf("keywordSearchTerms() = %v, should not include language word yaml", terms)
	}
}

func TestAnalyzeSearchFeaturesKeepsTechWordsAsTerms(t *testing.T) {
	features := analyzeSearchFeatures("redis stream 和 rabbitmq 的 jwt 逻辑", nil)

	for _, word := range []string{"redis", "stream", "rabbitmq", "jwt"} {
		if containsString(features.Symbols, word) {
			t.Fatalf("features.Symbols = %v, should not include tech word %q", features.Symbols, word)
		}
		if !containsString(features.Terms, word) {
			t.Fatalf("features.Terms = %v, want tech word %q", features.Terms, word)
		}
	}
}

func TestKeywordContentTermsExcludeStrongFeatures(t *testing.T) {
	features := analyzeSearchFeatures("mq/consumer.go里的reclaim函数怎么处理死信", nil)
	terms := keywordContentTerms(features)

	for _, excluded := range []string{"mq/consumer.go", "reclaim"} {
		if containsString(terms, excluded) {
			t.Fatalf("keywordContentTerms() = %v, should not include strong feature %q", terms, excluded)
		}
	}
	for _, want := range []string{"deadletter", "dlq"} {
		if !containsString(terms, want) {
			t.Fatalf("keywordContentTerms() = %v, want alias term %q", terms, want)
		}
	}
}

func TestBuildKeywordSearchQueryUsesFullTextForContentTerms(t *testing.T) {
	features := analyzeSearchFeatures("redis stream 重试逻辑", nil)
	query, args := buildKeywordSearchQuery(7, features, 10)

	if !strings.Contains(query, "search_vector @@ plainto_tsquery('simple', ?)") {
		t.Fatalf("keyword query = %s, want full-text search condition", query)
	}
	if !strings.Contains(query, "ts_rank(search_vector, plainto_tsquery('simple', ?))") {
		t.Fatalf("keyword query = %s, want full-text rank", query)
	}
	if !strings.Contains(query, "ORDER BY score DESC, id") {
		t.Fatalf("keyword query = %s, want score ordering", query)
	}
	if len(args) == 0 {
		t.Fatal("keyword query args are empty")
	}
}

func TestAnalyzeSearchFeaturesDetectsMultipleLanguages(t *testing.T) {
	features := analyzeSearchFeatures("go文件和yaml配置怎么关联，json响应在哪里", nil)

	for _, want := range []string{"go", "yaml", "json"} {
		if !containsString(features.Languages, want) {
			t.Fatalf("features.Languages = %v, want %q", features.Languages, want)
		}
	}
}

func TestSplitSearchTermsSeparatesCodeAndChineseBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "symbol followed by chinese",
			input: "HandleMessage怎么处理",
			want:  []string{"HandleMessage", "怎么处理"},
		},
		{
			name:  "path followed by chinese",
			input: "mq/consumer.go里的逻辑",
			want:  []string{"mq/consumer.go", "里的逻辑"},
		},
		{
			name:  "chinese around symbol",
			input: "解析CreateAndIndex函数",
			want:  []string{"解析", "CreateAndIndex", "函数"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSearchTerms(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("splitSearchTerms(%q) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("splitSearchTerms(%q) = %v, want %v", tc.input, got, tc.want)
				}
			}
		})
	}
}

func TestFuseCitationsRRFBoostsChunksFoundByMultipleRetrievers(t *testing.T) {
	rows := fuseCitationsRRF(
		[]Citation{{ID: 1, FilePath: "a.go"}, {ID: 2, FilePath: "b.go"}},
		[]Citation{{ID: 2, FilePath: "b.go"}, {ID: 3, FilePath: "c.go"}},
	)

	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].ID != 2 {
		t.Fatalf("first fused row ID = %d, want duplicate chunk 2", rows[0].ID)
	}
	if rows[0].Score <= rows[1].Score {
		t.Fatalf("duplicate chunk score = %.6f, want greater than %.6f", rows[0].Score, rows[1].Score)
	}
}

func TestRetrievalEvalSet(t *testing.T) {
	cases := loadRetrievalEvalCases(t)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.Question == "" {
				t.Fatal("question is empty")
			}
			if len(tc.ExpectedSymbols) == 0 {
				t.Fatal("expected symbols are empty")
			}
			if len(tc.Candidates) == 0 {
				t.Fatal("candidates are empty")
			}
		})
	}
}

type retrievalEvalCase struct {
	Name            string     `json:"name"`
	Question        string     `json:"question"`
	Hints           []string   `json:"hints"`
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
