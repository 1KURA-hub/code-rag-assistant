package service

import (
	"strings"
	"testing"
)

func TestLocalQueryPlanKeepsAliasesAndFeatures(t *testing.T) {
	plan := localQueryPlan("这个后端项目的接口路由是怎么处理请求的？", []string{"internal/handler/routes.go"})

	for _, want := range []string{"api", "router", "handler", "request"} {
		if !strings.Contains(plan.EmbeddingText, want) {
			t.Fatalf("EmbeddingText = %q, want alias %q", plan.EmbeddingText, want)
		}
	}
	if !containsString(plan.Features.Paths, "internal/handler/routes.go") {
		t.Fatalf("Features.Paths = %v, want internal/handler/routes.go", plan.Features.Paths)
	}
}

func TestModelDrivenQueryPlanUsesRewriteAsPrimaryPlan(t *testing.T) {
	plan := modelDrivenQueryPlan("这个后端项目的接口路由是怎么处理请求的？", []string{"internal/handler/routes.go"}, modelQueryPlan{
		RewrittenQuery: "request success but mysql record missing async persistence rabbitmq consumer",
		Terms:          []string{"mysql", "async persistence", "rabbitmq"},
		Paths:          []string{"internal/service/consumer.go"},
		Symbols:        []string{"HandleMessage"},
		SymbolTypes:    []string{"function", "invalid"},
		Languages:      []string{"go"},
	}, 20)

	if plan.EmbeddingText != "request success but mysql record missing async persistence rabbitmq consumer" {
		t.Fatalf("EmbeddingText = %q, want only rewritten query", plan.EmbeddingText)
	}
	for _, want := range []string{"mysql", "async persistence", "rabbitmq"} {
		if !containsString(plan.Features.Terms, want) {
			t.Fatalf("Features.Terms = %v, want %q", plan.Features.Terms, want)
		}
	}
	if !containsString(plan.Features.Paths, "internal/service/consumer.go") {
		t.Fatalf("Features.Paths = %v, want model path", plan.Features.Paths)
	}
	if !containsString(plan.Features.Paths, "internal/handler/routes.go") {
		t.Fatalf("Features.Paths = %v, want explicit hint path", plan.Features.Paths)
	}
	if !containsString(plan.Features.Symbols, "handlemessage") {
		t.Fatalf("Features.Symbols = %v, want handlemessage", plan.Features.Symbols)
	}
	if !containsString(plan.Features.SymbolTypes, "function") {
		t.Fatalf("Features.SymbolTypes = %v, want function", plan.Features.SymbolTypes)
	}
	if containsString(plan.Features.SymbolTypes, "invalid") {
		t.Fatalf("Features.SymbolTypes = %v, should reject invalid symbol type", plan.Features.SymbolTypes)
	}
	if !containsString(plan.Features.Languages, "go") {
		t.Fatalf("Features.Languages = %v, want go", plan.Features.Languages)
	}
	for _, unwantedAlias := range []string{"api", "router", "handler", "request"} {
		if containsString(plan.Features.Terms, unwantedAlias) {
			t.Fatalf("Features.Terms = %v, should not keep fallback alias %q after model rewrite", plan.Features.Terms, unwantedAlias)
		}
	}
}

func TestModelDrivenQueryPlanFallsBackToExplicitTextWhenRewriteEmpty(t *testing.T) {
	plan := modelDrivenQueryPlan("解释 BuildQueryPlan", []string{"internal/service/query_plan.go"}, modelQueryPlan{
		Terms: []string{"query plan"},
	}, 20)

	want := "解释 BuildQueryPlan\ninternal/service/query_plan.go"
	if plan.EmbeddingText != want {
		t.Fatalf("EmbeddingText = %q, want %q", plan.EmbeddingText, want)
	}
}

func TestExtractJSONObjectFromMarkdownFence(t *testing.T) {
	input := "```json\n{\"rewritten_query\":\"redis stream\",\"terms\":[\"redis\"]}\n```"
	got := extractJSONObject(input)

	if got != "{\"rewritten_query\":\"redis stream\",\"terms\":[\"redis\"]}" {
		t.Fatalf("extractJSONObject() = %q", got)
	}
}
