package service

import (
	"strings"
	"testing"
)

func TestMatchedAliasesForRepositoryIndexQuestion(t *testing.T) {
	aliases := matchedAliases("这个项目的仓库导入和索引流程是什么？")

	for _, want := range []string{"repository", "ingest", "indexing", "github"} {
		if !containsString(aliases, want) {
			t.Fatalf("matchedAliases() = %v, want %q", aliases, want)
		}
	}
}

func TestMatchedAliasesForAPIQuestion(t *testing.T) {
	aliases := matchedAliases("这个项目主要接口有哪些？")

	for _, want := range []string{"api", "router", "route", "handler", "RegisterRoutes"} {
		if !containsString(aliases, want) {
			t.Fatalf("matchedAliases() = %v, want %q", aliases, want)
		}
	}
}

func TestExpandQueryTextKeepsHintsAndAddsAliases(t *testing.T) {
	expanded := expandQueryText("代码分片和 AST 解析逻辑", []string{"internal/service/chunker.go"})

	for _, want := range []string{"代码分片和 AST 解析逻辑", "internal/service/chunker.go", "chunk", "parser", "ChunkSourceFile"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expandQueryText() = %q, want to contain %q", expanded, want)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
