package service

import (
	"strings"
	"testing"
)

func TestMatchedAliasesForChineseMessageQuestion(t *testing.T) {
	aliases := matchedAliases("这个项目的消息消费和重试流程是什么？")

	for _, want := range []string{"message", "consumer", "retry", "queue"} {
		if !containsString(aliases, want) {
			t.Fatalf("matchedAliases() = %v, want %q", aliases, want)
		}
	}
}

func TestExpandQueryTextKeepsHintsAndAddsAliases(t *testing.T) {
	expanded := expandQueryText("Redis Stream 转发逻辑", []string{"mq/relay.go"})

	for _, want := range []string{"Redis Stream 转发逻辑", "mq/relay.go", "xreadgroup", "xautoclaim", "relay"} {
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
