package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"code-rag-assistant/internal/config"
	dbrepo "code-rag-assistant/internal/repository"
	"code-rag-assistant/internal/service"
)

type evalCase struct {
	Category string          `json:"category"`
	Name     string          `json:"name"`
	Question string          `json:"question"`
	Hints    []string        `json:"hints"`
	Relevant []relevantChunk `json:"relevant"`
}

type relevantChunk struct {
	FilePath   string `json:"file_path"`
	SymbolName string `json:"symbol_name"`
}

type evalSummary struct {
	Total      int
	Errors     int
	HitAt1     int
	HitAt3     int
	HitAt5     int
	RecallAt5  float64
	Reciprocal float64
}

func main() {
	repoID := flag.Uint("repo-id", 1, "repository id to evaluate")
	evalPath := flag.String("eval", "internal/service/testdata/retrieval_eval_cases.json", "retrieval eval cases path")
	flag.Parse()

	cases, err := loadEvalCases(*evalPath)
	if err != nil {
		log.Fatalf("load eval cases: %v", err)
	}
	cfg := config.Load()
	db, err := dbrepo.OpenPostgres(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}

	retriever := service.NewRetriever(db, service.NewEmbedder(cfg), cfg)
	ctx := context.Background()
	summary := evalSummary{}
	grouped := map[string]*evalSummary{}
	for _, tc := range cases {
		category := evalCategory(tc)
		group := groupedSummary(grouped, category)
		summary.Total++
		group.Total++
		citations, err := retriever.Search(ctx, uint(*repoID), tc.Question, tc.Hints)
		if err != nil {
			summary.Errors++
			group.Errors++
			fmt.Printf("[ERROR] %s: %v\n", tc.Name, err)
			continue
		}
		result := evaluateCase(tc, citations)
		addResult(&summary, result)
		addResult(group, result)

		if result.FirstHitRank > 0 {
			fmt.Printf("[PASS] %s/%s first_hit=%d recall@5=%.2f\n", category, tc.Name, result.FirstHitRank, result.RecallAt5)
		} else {
			fmt.Printf("[FAIL] %s/%s no_hit@5 expected=%s\n", category, tc.Name, formatRelevant(tc.Relevant))
		}
	}
	printSummary("Retrieval Eval Summary", summary)
	printGroupedSummary(grouped)
}

func loadEvalCases(path string) ([]evalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []evalCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func evalCategory(tc evalCase) string {
	category := strings.TrimSpace(tc.Category)
	if category == "" {
		return "general"
	}
	return category
}

func groupedSummary(groups map[string]*evalSummary, category string) *evalSummary {
	if groups[category] == nil {
		groups[category] = &evalSummary{}
	}
	return groups[category]
}

func addResult(summary *evalSummary, result caseResult) {
	if result.HitAt1 {
		summary.HitAt1++
	}
	if result.HitAt3 {
		summary.HitAt3++
	}
	if result.HitAt5 {
		summary.HitAt5++
	}
	summary.RecallAt5 += result.RecallAt5
	summary.Reciprocal += result.ReciprocalRank
}

type caseResult struct {
	HitAt1         bool
	HitAt3         bool
	HitAt5         bool
	RecallAt5      float64
	ReciprocalRank float64
	FirstHitRank   int
}

func evaluateCase(tc evalCase, citations []service.Citation) caseResult {
	firstHit := firstHitRank(citations, tc.Relevant)
	matched := matchedRelevantCount(limitCitations(citations, 5), tc.Relevant)
	recall := 0.0
	if len(tc.Relevant) > 0 {
		recall = float64(matched) / float64(len(tc.Relevant))
	}
	reciprocal := 0.0
	if firstHit > 0 {
		reciprocal = 1.0 / float64(firstHit)
	}
	return caseResult{
		HitAt1:         firstHit > 0 && firstHit <= 1,
		HitAt3:         firstHit > 0 && firstHit <= 3,
		HitAt5:         firstHit > 0 && firstHit <= 5,
		RecallAt5:      recall,
		ReciprocalRank: reciprocal,
		FirstHitRank:   firstHit,
	}
}

func firstHitRank(citations []service.Citation, relevant []relevantChunk) int {
	for idx, citation := range citations {
		if citationHits(citation, relevant) {
			return idx + 1
		}
	}
	return 0
}

func matchedRelevantCount(citations []service.Citation, relevant []relevantChunk) int {
	matched := map[int]struct{}{}
	for idx, rel := range relevant {
		for _, citation := range citations {
			if matchesRelevant(citation, rel) {
				matched[idx] = struct{}{}
				break
			}
		}
	}
	return len(matched)
}

func citationHits(citation service.Citation, relevant []relevantChunk) bool {
	for _, rel := range relevant {
		if matchesRelevant(citation, rel) {
			return true
		}
	}
	return false
}

func matchesRelevant(citation service.Citation, rel relevantChunk) bool {
	fileOK := rel.FilePath == "" || strings.Contains(normalize(citation.FilePath), normalize(rel.FilePath))
	symbolOK := rel.SymbolName == "" || strings.Contains(normalize(citation.SymbolName), normalize(rel.SymbolName))
	return fileOK && symbolOK
}

func normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "a/")
	value = strings.TrimPrefix(value, "b/")
	return strings.Trim(value, "/")
}

func limitCitations(citations []service.Citation, limit int) []service.Citation {
	if len(citations) <= limit {
		return citations
	}
	return citations[:limit]
}

func printSummary(title string, summary evalSummary) {
	total := summary.Total
	if total == 0 {
		fmt.Println("cases: 0")
		return
	}
	fmt.Println()
	fmt.Println(title)
	fmt.Printf("cases: %d errors: %d\n", summary.Total, summary.Errors)
	fmt.Printf("HitRate@1: %.1f%% (%d/%d)\n", percent(summary.HitAt1, total), summary.HitAt1, total)
	fmt.Printf("HitRate@3: %.1f%% (%d/%d)\n", percent(summary.HitAt3, total), summary.HitAt3, total)
	fmt.Printf("HitRate@5: %.1f%% (%d/%d)\n", percent(summary.HitAt5, total), summary.HitAt5, total)
	fmt.Printf("Recall@5: %.3f\n", summary.RecallAt5/float64(total))
	fmt.Printf("MRR: %.3f\n", summary.Reciprocal/float64(total))
}

func printGroupedSummary(groups map[string]*evalSummary) {
	if len(groups) == 0 {
		return
	}
	categories := make([]string, 0, len(groups))
	for category := range groups {
		categories = append(categories, category)
	}
	sort.Strings(categories)

	fmt.Println()
	fmt.Println("Category Summary")
	for _, category := range categories {
		summary := groups[category]
		fmt.Printf("- %s: cases=%d HitRate@5=%.1f%% Recall@5=%.3f MRR=%.3f errors=%d\n",
			category,
			summary.Total,
			percent(summary.HitAt5, summary.Total),
			summary.RecallAt5/float64(summary.Total),
			summary.Reciprocal/float64(summary.Total),
			summary.Errors,
		)
	}
}

func percent(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(count) * 100 / float64(total)
}

func formatRelevant(relevant []relevantChunk) string {
	parts := make([]string, 0, len(relevant))
	for _, rel := range relevant {
		parts = append(parts, rel.FilePath+"#"+rel.SymbolName)
	}
	return strings.Join(parts, ",")
}
