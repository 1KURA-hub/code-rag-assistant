package service

import "strings"

var queryAliases = []struct {
	Terms   []string
	Aliases []string
}{
	{
		Terms: []string{"仓库", "导入", "索引", "重新索引", "下载", "解压", "扫描", "状态", "github", "repo", "repository"},
		Aliases: []string{
			"repository", "repo", "ingest", "index", "indexing", "github", "download",
			"zip", "unzip", "scan", "status", "pending", "ready", "failed", "CreateAndIndex",
		},
	},
	{
		Terms: []string{"分片", "切片", "ast", "语法树", "函数", "方法", "结构体", "类型", "常量", "变量", "行号", "声明"},
		Aliases: []string{
			"chunk", "chunking", "ast", "parser", "parse", "declaration", "decl",
			"function", "method", "type", "const", "var", "symbol", "line", "ChunkSourceFile",
		},
	},
	{
		Terms: []string{"embedding", "向量", "向量化", "批量", "归一化", "pgvector", "相似度", "余弦"},
		Aliases: []string{
			"embedding", "embed", "Embed", "EmbedBatch", "vector", "pgvector",
			"cosine", "normalize", "normalizeVector", "embedding_vector", "VectorLiteral",
		},
	},
	{
		Terms: []string{"检索", "召回", "混合检索", "关键词", "全文检索", "重排", "融合", "rrf", "topk", "依据", "引用"},
		Aliases: []string{
			"retriever", "retrieve", "Search", "keyword", "full-text", "search_vector",
			"plainto_tsquery", "rank", "rerank", "RRF", "TopK", "citation", "evidence",
			"BuildQueryPlan", "query rewrite", "alias",
		},
	},
	{
		Terms: []string{"问答", "回答", "大模型", "模型", "prompt", "提示词", "上下文", "幻觉", "本地兜底"},
		Aliases: []string{
			"answer", "Ask", "LLM", "model", "prompt", "context", "citation",
			"callLLM", "localAnswer", "promptCitations", "OpenAI",
		},
	},
	{
		Terms: []string{"diff", "变更", "影响分析", "风险", "测试建议", "修改影响", "git diff"},
		Aliases: []string{
			"diff", "impact", "Analyze", "ExtractDiffHints", "risk", "risks",
			"suggested_tests", "changed", "module",
		},
	},
	{
		Terms: []string{"接口", "api", "路由", "handler", "endpoint", "参数", "请求", "响应", "main", "启动"},
		Aliases: []string{
			"api", "router", "route", "routes", "handler", "endpoint", "request",
			"response", "ShouldBindJSON", "RegisterRoutes", "main", "server",
		},
	},
	{
		Terms: []string{"配置", "环境变量", "数据库", "缓存", "redis", "postgres", "gorm", "docker", "部署"},
		Aliases: []string{
			"config", "env", "environment", "postgres", "postgresql", "gorm",
			"redis", "cache", "cache aside", "docker", "compose", "OpenPostgres",
		},
	},
}

func expandQueryText(query string, hints []string) string {
	var b strings.Builder
	b.WriteString(query)
	for _, hint := range hints {
		b.WriteByte('\n')
		b.WriteString(hint)
	}
	for _, alias := range matchedAliases(query + "\n" + strings.Join(hints, "\n")) {
		b.WriteByte('\n')
		b.WriteString(alias)
	}
	return b.String()
}

func matchedAliases(text string) []string {
	lower := strings.ToLower(text)
	seen := map[string]struct{}{}
	var aliases []string
	for _, group := range queryAliases {
		matched := false
		for _, term := range group.Terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, alias := range group.Aliases {
			if _, ok := seen[alias]; !ok {
				seen[alias] = struct{}{}
				aliases = append(aliases, alias)
			}
		}
	}
	return aliases
}
