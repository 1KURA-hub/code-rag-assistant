package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"code-rag-assistant/internal/config"
)

type QueryPlan struct {
	Original      string
	EmbeddingText string
	Features      searchFeatures
	RewriteUsed   bool
	RewriteError  string
}

type QueryPlanSnapshot struct {
	Original      string   `json:"original"`
	EmbeddingText string   `json:"embedding_text"`
	RewriteUsed   bool     `json:"rewrite_used"`
	RewriteError  string   `json:"rewrite_error"`
	Terms         []string `json:"terms"`
	Paths         []string `json:"paths"`
	Symbols       []string `json:"symbols"`
	SymbolTypes   []string `json:"symbol_types"`
	Languages     []string `json:"languages"`
}

type modelQueryPlan struct {
	RewrittenQuery string   `json:"rewritten_query"`
	Terms          []string `json:"terms"`
	Paths          []string `json:"paths"`
	Symbols        []string `json:"symbols"`
	SymbolTypes    []string `json:"symbol_types"`
	Languages      []string `json:"languages"`
}

func buildQueryPlan(ctx context.Context, cfg config.Config, query string, hints []string) QueryPlan {
	return BuildQueryPlan(ctx, cfg, query, hints)
}

func BuildQueryPlan(ctx context.Context, cfg config.Config, query string, hints []string) QueryPlan {
	plan := localQueryPlan(query, hints)
	if !cfg.QueryRewriteEnabled || cfg.OpenAIAPIKey == "" {
		return plan
	}
	timeout := cfg.QueryRewriteTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	rewriteCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	modelPlan, err := callModelQueryPlan(rewriteCtx, cfg, query, hints)
	if err != nil {
		plan.RewriteError = err.Error()
		return plan
	}
	mergeModelQueryPlan(&plan, modelPlan, cfg.QueryRewriteMaxTerms)
	plan.RewriteUsed = true
	return plan
}

func BuildQueryPlanSnapshot(ctx context.Context, cfg config.Config, query string, hints []string) QueryPlanSnapshot {
	plan := BuildQueryPlan(ctx, cfg, query, hints)
	return QueryPlanSnapshot{
		Original:      plan.Original,
		EmbeddingText: plan.EmbeddingText,
		RewriteUsed:   plan.RewriteUsed,
		RewriteError:  plan.RewriteError,
		Terms:         append([]string{}, plan.Features.Terms...),
		Paths:         append([]string{}, plan.Features.Paths...),
		Symbols:       append([]string{}, plan.Features.Symbols...),
		SymbolTypes:   append([]string{}, plan.Features.SymbolTypes...),
		Languages:     append([]string{}, plan.Features.Languages...),
	}
}

func localQueryPlan(query string, hints []string) QueryPlan {
	return QueryPlan{
		Original:      query,
		EmbeddingText: expandQueryText(query, hints),
		Features:      analyzeSearchFeatures(query, hints),
	}
}

func callModelQueryPlan(ctx context.Context, cfg config.Config, query string, hints []string) (modelQueryPlan, error) {
	content, err := callLLMWithModel(ctx, cfg, cfg.QueryRewriteModel, queryRewriteSystemPrompt(), queryRewriteUserPrompt(query, hints))
	if err != nil {
		return modelQueryPlan{}, err
	}
	var plan modelQueryPlan
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &plan); err != nil {
		return modelQueryPlan{}, err
	}
	return plan, nil
}

func queryRewriteSystemPrompt() string {
	return strings.Join([]string{
		"你是代码仓库检索查询改写器，不要回答用户问题。",
		"你的任务是把中文或中英文混合问题改写成适合检索英文源码的查询。",
		"只输出 JSON，不要输出解释、Markdown 或代码块。",
		"rewritten_query 用于 embedding，必须包含英文技术词、后端常见源码词和必要的中文原意。",
		"terms 用于代码内容全文检索，必须输出 5 到 12 个短词或短语；paths 用于文件路径；symbols 用于可能的函数、方法、变量、类型名；symbol_types 只能包含 function、method、type、const、var；languages 用于语言名。",
		"不要输出自然语言解释。symbols 可以给出可能出现在源码里的命名线索，最终系统会用真实代码命中结果校验，不会把你的猜测当事实。",
		"通用后端映射：项目结构 -> project module package architecture layer dependency flow；接口路由 -> api router route handler controller endpoint request response param bind json；数据库 -> database db table model schema repository dao query transaction commit rollback migration；缓存 -> redis cache key ttl bloom filter singleflight；消息队列 -> message queue producer consumer async retry ack dead letter dlq；鉴权 -> auth authentication authorization jwt token session middleware claims permission；配置启动 -> config env startup bootstrap init main dependency docker compose yaml json；并发 -> goroutine channel context timeout lock mutex worker pool；一致性 -> consistency idempotency duplicate unique index compensation；错误测试 -> error fallback log metrics trace test mock coverage。",
	}, "\n")
}

func queryRewriteUserPrompt(query string, hints []string) string {
	var b strings.Builder
	b.WriteString("用户问题或 diff：\n")
	b.WriteString(query)
	if len(hints) > 0 {
		b.WriteString("\n\n已有本地提示词：\n")
		for _, hint := range hints {
			b.WriteString("- ")
			b.WriteString(hint)
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n请输出如下 JSON：\n")
	b.WriteString(`{"rewritten_query":"","terms":[],"paths":[],"symbols":[],"symbol_types":[],"languages":[]}`)
	return b.String()
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start >= 0 && end >= start {
		return text[start : end+1]
	}
	return text
}

func mergeModelQueryPlan(plan *QueryPlan, modelPlan modelQueryPlan, maxTerms int) {
	if strings.TrimSpace(modelPlan.RewrittenQuery) != "" {
		plan.EmbeddingText = strings.TrimSpace(modelPlan.RewrittenQuery) + "\n" + plan.EmbeddingText
	}
	if maxTerms <= 0 {
		maxTerms = 20
	}
	addLimitedStrings(modelPlan.Terms, maxTerms, func(value string) {
		addSearchTerm(&plan.Features, value)
	})
	for _, value := range modelPlan.Paths {
		addSearchPath(&plan.Features, value)
	}
	for _, value := range modelPlan.Symbols {
		addSearchSymbol(&plan.Features, value)
	}
	for _, value := range modelPlan.SymbolTypes {
		addSearchSymbolType(&plan.Features, value)
	}
	for _, value := range modelPlan.Languages {
		addSearchLanguage(&plan.Features, value)
	}
}

func addLimitedStrings(values []string, max int, add func(string)) {
	for i, value := range values {
		if i >= max {
			return
		}
		add(value)
	}
}

func addSearchTerm(features *searchFeatures, term string) {
	term = strings.ToLower(strings.TrimSpace(term))
	if len([]rune(term)) < 2 || containsSearchString(features.Terms, term) {
		return
	}
	features.Terms = append(features.Terms, term)
}

func addSearchPath(features *searchFeatures, path string) {
	path = strings.ToLower(strings.Trim(path, "`'\"，。；,;()[]{}<> \n\t"))
	if path == "" || containsSearchString(features.Paths, path) {
		return
	}
	features.Paths = append(features.Paths, path)
}

func addSearchSymbol(features *searchFeatures, symbol string) {
	symbol = strings.ToLower(strings.Trim(symbol, "`'\"，。；,;()[]{}<> \n\t"))
	if len(symbol) < 3 || containsSearchString(features.Symbols, symbol) {
		return
	}
	features.Symbols = append(features.Symbols, symbol)
}

func addSearchSymbolType(features *searchFeatures, symbolType string) {
	symbolType = strings.ToLower(strings.TrimSpace(symbolType))
	switch symbolType {
	case "function", "method", "type", "const", "var":
	default:
		return
	}
	if containsSearchString(features.SymbolTypes, symbolType) {
		return
	}
	features.SymbolTypes = append(features.SymbolTypes, symbolType)
}

func addSearchLanguage(features *searchFeatures, language string) {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" || containsSearchString(features.Languages, language) {
		return
	}
	features.Languages = append(features.Languages, language)
}

func containsSearchString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
