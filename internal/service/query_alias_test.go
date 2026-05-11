package service

import (
	"strings"
	"testing"
)

func TestMatchedAliasesForBackendProjectQuestion(t *testing.T) {
	aliases := matchedAliases("这个后端项目的整体架构和主流程是什么？")

	for _, want := range []string{"project", "architecture", "module", "flow"} {
		if !containsString(aliases, want) {
			t.Fatalf("matchedAliases() = %v, want %q", aliases, want)
		}
	}
}

func TestMatchedAliasesForAPIQuestion(t *testing.T) {
	aliases := matchedAliases("这个项目主要接口有哪些？")

	for _, want := range []string{"api", "router", "route", "handler", "json"} {
		if !containsString(aliases, want) {
			t.Fatalf("matchedAliases() = %v, want %q", aliases, want)
		}
	}
}

func TestMatchedAliasesForCommonBackendDomains(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "database",
			query: "数据库表结构和事务是怎么处理的？",
			want:  []string{"database", "table", "transaction", "gorm"},
		},
		{
			name:  "cache",
			query: "缓存穿透和热点 key 怎么处理？",
			want:  []string{"redis", "cache", "key", "bloom"},
		},
		{
			name:  "message queue",
			query: "消息队列消费失败后怎么重试？",
			want:  []string{"message", "queue", "consumer", "retry"},
		},
		{
			name:  "auth",
			query: "登录鉴权和 token 校验在哪做？",
			want:  []string{"auth", "token", "jwt", "middleware"},
		},
		{
			name:  "frontend",
			query: "前端页面组件状态是怎么更新的？",
			want:  []string{"frontend", "component", "state", "render"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aliases := matchedAliases(tc.query)
			for _, want := range tc.want {
				if !containsString(aliases, want) {
					t.Fatalf("matchedAliases() = %v, want %q", aliases, want)
				}
			}
		})
	}
}

func TestMatchedAliasesDoNotContainProjectSpecificSymbols(t *testing.T) {
	aliases := matchedAliases("仓库项目分片检索问答")
	for _, forbidden := range []string{"CreateAndIndex", "ChunkSourceFile", "vectorSearch", "pending", "ready", "failed"} {
		if containsString(aliases, forbidden) {
			t.Fatalf("matchedAliases() = %v, should not contain project-specific %q", aliases, forbidden)
		}
	}
}

func TestExpandQueryTextKeepsHintsAndAddsAliases(t *testing.T) {
	expanded := expandQueryText("代码分片和 AST 解析逻辑", []string{"internal/service/chunker.go"})

	for _, want := range []string{"代码分片和 AST 解析逻辑", "internal/service/chunker.go", "chunk", "parser", "function"} {
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
