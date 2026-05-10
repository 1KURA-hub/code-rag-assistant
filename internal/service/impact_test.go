package service

import (
	"strings"
	"testing"
)

func TestLocalImpactMarksUnmatchedDiffPath(t *testing.T) {
	diff := `diff --git a/mq/consumer.go b/mq/consumer.go
++ b/mq/consumer.go
+func handleRetryMessage() error {
+    return retryDeadLetter()
+}`
	citations := []Citation{
		{
			FilePath:   "internal/service/impact.go",
			StartLine:  1,
			EndLine:    20,
			SymbolName: "Analyze",
			SymbolType: "function",
			Content:    "func (s *ImpactService) Analyze() {}",
			Score:      0.7,
		},
	}

	resp := localImpact(diff, citations)

	if len(resp.MatchedPaths) != 0 {
		t.Fatalf("matched paths = %v, want empty", resp.MatchedPaths)
	}
	if !containsString(resp.UnmatchedPaths, "mq/consumer.go") {
		t.Fatalf("unmatched paths = %v, want mq/consumer.go", resp.UnmatchedPaths)
	}
	if !strings.Contains(resp.Summary, "可信度较低") {
		t.Fatalf("summary = %q, want low confidence notice", resp.Summary)
	}
	assertNoCourseSpecificText(t, resp.Risks)
	assertNoCourseSpecificText(t, resp.SuggestedTests)
}

func TestLocalImpactUsesMatchedDiffPath(t *testing.T) {
	diff := `diff --git a/internal/service/impact.go b/internal/service/impact.go
+++ b/internal/service/impact.go
+func localImpact() {}`
	citations := []Citation{
		{
			FilePath:   "internal/service/impact.go",
			StartLine:  60,
			EndLine:    75,
			SymbolName: "localImpact",
			SymbolType: "function",
			Content:    "func localImpact(diffText string, citations []Citation) *ImpactResponse {}",
			Score:      0.9,
		},
	}

	resp := localImpact(diff, citations)

	if !containsString(resp.MatchedPaths, "internal/service/impact.go") {
		t.Fatalf("matched paths = %v, want internal/service/impact.go", resp.MatchedPaths)
	}
	if len(resp.UnmatchedPaths) != 0 {
		t.Fatalf("unmatched paths = %v, want empty", resp.UnmatchedPaths)
	}
	if !containsString(resp.ImpactedModules, "internal/service/impact.go") {
		t.Fatalf("impacted modules = %v, want matched citation path", resp.ImpactedModules)
	}
}

func TestLocalImpactUsesRetrievalSpecificSuggestions(t *testing.T) {
	diff := `diff --git a/internal/service/retriever.go b/internal/service/retriever.go
+++ b/internal/service/retriever.go
+	if len(vectorResults) == 0 {
+		return r.keywordSearch(ctx, repositoryID, hints)
+	}
+	return fuseCitationsRRF(vectorResults, keywordResults, r.cfg.TopK), nil`
	citations := []Citation{
		{
			FilePath:   "internal/service/retriever.go",
			StartLine:  35,
			EndLine:    80,
			SymbolName: "Search",
			SymbolType: "function",
			Content:    "func (r *Retriever) Search() { vectorSearch(); keywordSearch(); fuseCitationsRRF() }",
			Score:      0.92,
		},
	}

	resp := localImpact(diff, citations)

	if !containsJoined(resp.Risks, "RRF") {
		t.Fatalf("risks = %v, want retrieval-specific RRF risk", resp.Risks)
	}
	if !containsJoined(resp.SuggestedTests, "TopK") {
		t.Fatalf("tests = %v, want TopK retrieval test", resp.SuggestedTests)
	}
	if containsJoined(resp.Risks, "事务一致性") || containsJoined(resp.SuggestedTests, "事务一致性") {
		t.Fatalf("impact suggestions should not mention unrelated transactions: risks=%v tests=%v", resp.Risks, resp.SuggestedTests)
	}
}

func assertNoCourseSpecificText(t *testing.T, values []string) {
	t.Helper()
	joined := strings.Join(values, "\n")
	for _, forbidden := range []string{"选课", "库存", "重复选课"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("values contain course-specific text %q: %v", forbidden, values)
		}
	}
}

func containsJoined(values []string, target string) bool {
	return strings.Contains(strings.Join(values, "\n"), target)
}
