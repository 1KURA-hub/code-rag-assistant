package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"code-rag-assistant/internal/config"
	dbrepo "code-rag-assistant/internal/repository"
	"code-rag-assistant/internal/service"
)

type evalCase struct {
	Name     string          `json:"name"`
	Question string          `json:"question"`
	Hints    []string        `json:"hints"`
	Relevant []relevantChunk `json:"relevant"`
}

type relevantChunk struct {
	FilePath   string `json:"file_path"`
	SymbolName string `json:"symbol_name"`
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
	for _, tc := range cases {
		citations, err := retriever.Search(ctx, uint(*repoID), tc.Question, tc.Hints)
		if err != nil {
			fmt.Printf("[ERROR] %s: %v\n", tc.Name, err)
			continue
		}
		fmt.Printf("[CASE] %s top%d\n", tc.Name, len(citations))
		for idx, citation := range citations {
			fmt.Printf("  #%d %s %s:%d-%d score=%.6f\n",
				idx+1, citation.SymbolName, citation.FilePath, citation.StartLine, citation.EndLine, citation.Score)
		}
	}
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
