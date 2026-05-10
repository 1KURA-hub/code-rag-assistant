package service

import (
	"context"
	"fmt"
	"strings"

	"code-rag-assistant/internal/config"
	"code-rag-assistant/internal/model"
	"code-rag-assistant/internal/util"

	"gorm.io/gorm"
)

type ImpactService struct {
	db        *gorm.DB
	retriever *Retriever
	cfg       config.Config
}

type ImpactResponse struct {
	Summary         string     `json:"summary"`
	ImpactedModules []string   `json:"impacted_modules"`
	Risks           []string   `json:"risks"`
	SuggestedTests  []string   `json:"suggested_tests"`
	MatchedPaths    []string   `json:"matched_paths"`
	UnmatchedPaths  []string   `json:"unmatched_paths"`
	Citations       []Citation `json:"citations"`
}

func NewImpactService(db *gorm.DB, retriever *Retriever, cfg config.Config) *ImpactService {
	return &ImpactService{db: db, retriever: retriever, cfg: cfg}
}

func (s *ImpactService) Analyze(ctx context.Context, repositoryID uint, diffText string) (*ImpactResponse, error) {
	if err := s.ensureReady(ctx, repositoryID); err != nil {
		return nil, err
	}
	hints := util.ExtractDiffHints(diffText)
	citations, err := s.retriever.Search(ctx, repositoryID, diffText, hints)
	if err != nil {
		return nil, err
	}
	resp := localImpact(diffText, citations)
	if generated, err := callLLM(ctx, s.cfg, impactSystemPrompt(), impactUserPrompt(diffText, citations, resp.MatchedPaths, resp.UnmatchedPaths)); err == nil {
		resp.Summary = generated
	}
	return resp, nil
}

func (s *ImpactService) ensureReady(ctx context.Context, repositoryID uint) error {
	var repo model.Repository
	if err := s.db.WithContext(ctx).First(&repo, repositoryID).Error; err != nil {
		return err
	}
	if repo.Status != "ready" {
		return fmt.Errorf("repository is %s", repo.Status)
	}
	return nil
}

func localImpact(diffText string, citations []Citation) *ImpactResponse {
	changedPaths := util.ExtractDiffPaths(diffText)
	matchedPaths, unmatchedPaths := matchDiffPaths(changedPaths, citations)
	matchedCitations := filterCitationsByPaths(citations, matchedPaths)
	modules := uniquePaths(matchedCitations, 5)
	if len(modules) == 0 && len(unmatchedPaths) == 0 {
		modules = uniquePaths(citations, 5)
	}
	risks := inferRisks(diffText, matchedPaths, matchedCitations)
	tests := inferTests(diffText, matchedPaths, matchedCitations)
	summary := buildImpactSummary(changedPaths, matchedPaths, unmatchedPaths, citations)
	if strings.TrimSpace(diffText) == "" {
		summary = "没有提供 diff 内容。"
	}
	return &ImpactResponse{
		Summary:         summary,
		ImpactedModules: modules,
		Risks:           risks,
		SuggestedTests:  tests,
		MatchedPaths:    matchedPaths,
		UnmatchedPaths:  unmatchedPaths,
		Citations:       citations,
	}
}

func buildImpactSummary(changedPaths, matchedPaths, unmatchedPaths []string, citations []Citation) string {
	if len(changedPaths) == 0 {
		return "没有从 diff 中解析到明确文件路径，当前结果主要基于 diff 文本和向量检索召回的代码片段。"
	}
	if len(matchedPaths) == 0 && len(unmatchedPaths) > 0 {
		return fmt.Sprintf("本次 diff 涉及 %s，但这些变更文件未在当前索引仓库的代码依据中命中。当前分析主要基于 diff 文本，可信度较低，请确认 diff 是否属于当前仓库。", strings.Join(unmatchedPaths, "、"))
	}
	var b strings.Builder
	b.WriteString("本次变更涉及 ")
	b.WriteString(strings.Join(changedPaths, "、"))
	b.WriteString("。系统已优先结合变更路径和相关关键词检索代码证据")
	if len(citations) > 0 {
		b.WriteString(fmt.Sprintf("，召回 %d 个相关代码片段。", len(citations)))
	} else {
		b.WriteString("，但没有召回明显相关的代码片段。")
	}
	if len(unmatchedPaths) > 0 {
		b.WriteString("未命中的变更文件：")
		b.WriteString(strings.Join(unmatchedPaths, "、"))
		b.WriteString("。")
	}
	return b.String()
}

func inferRisks(diffText string, matchedPaths []string, citations []Citation) []string {
	seen := map[string]bool{}
	var risks []string
	add := func(value string) {
		if !seen[value] {
			seen[value] = true
			risks = append(risks, value)
		}
	}
	if len(matchedPaths) == 0 && len(citations) == 0 {
		add("当前 diff 文件未命中当前仓库的代码依据，需要先确认变更是否属于当前索引仓库。")
		return risks
	}
	evidence := impactEvidenceText(diffText, matchedPaths, citations)
	switch {
	case hasAny(evidence, "retriever", "search", "vector", "embedding", "pgvector", "keyword", "rrf", "topk", "citation"):
		add("检索召回链路可能受影响，需要关注向量召回、关键词召回、RRF 融合和 TopK 截断是否仍符合预期。")
		if hasAny(evidence, "return", "keywordsearch", "fusecitationsrrf") {
			add("新增提前返回可能绕过融合排序逻辑，需要确认 fallback 结果的排序、去重和数量限制。")
		}
	case hasAny(evidence, "prompt", "llm", "answer", "impact"):
		add("模型生成链路可能受影响，需要关注 prompt 上下文、代码依据引用和无证据回答控制。")
	}
	for _, path := range append(matchedPaths, uniquePaths(citations, 8)...) {
		lower := strings.ToLower(path)
		switch {
		case strings.Contains(lower, "mq") || strings.Contains(lower, "queue") || strings.Contains(lower, "consumer") || strings.Contains(lower, "publisher"):
			add("异步消息链路可能受影响，需要关注重复处理、确认时机、失败重试和消息丢失。")
		case strings.Contains(lower, "redis") || strings.Contains(lower, "cache") || strings.Contains(lower, "stream"):
			add("缓存或 Redis 相关逻辑可能受影响，需要关注缓存一致性、过期时间、重复请求和并发更新。")
		case strings.Contains(lower, "service"):
			add("核心服务逻辑可能受影响，需要关注调用链、错误处理、空结果和边界参数。")
		case strings.Contains(lower, "api") || strings.Contains(lower, "router") || strings.Contains(lower, "handler") || strings.Contains(lower, "middleware"):
			add("接口入口或中间件行为可能受影响，需要关注鉴权、参数校验和错误返回。")
		case strings.Contains(lower, "model") || strings.Contains(lower, "repository"):
			add("数据模型或持久化访问可能受影响，需要关注字段兼容性和事务一致性。")
		case strings.Contains(lower, "config") || strings.Contains(lower, "env"):
			add("配置读取或运行环境可能受影响，需要关注默认值、环境变量和部署配置兼容性。")
		}
	}
	if len(risks) == 0 {
		add("需要结合引用代码片段人工确认变更是否影响调用方、错误处理和边界条件。")
	}
	return risks
}

func inferTests(diffText string, matchedPaths []string, citations []Citation) []string {
	seen := map[string]bool{}
	var tests []string
	add := func(value string) {
		if !seen[value] {
			seen[value] = true
			tests = append(tests, value)
		}
	}
	if len(matchedPaths) == 0 && len(citations) == 0 {
		add("先确认 diff 是否属于当前仓库；确认后再补充变更文件对应的单元测试或接口回归测试。")
		return tests
	}
	evidence := impactEvidenceText(diffText, matchedPaths, citations)
	switch {
	case hasAny(evidence, "retriever", "search", "vector", "embedding", "pgvector", "keyword", "rrf", "topk", "citation"):
		add("补充向量召回为空、关键词召回非空、混合召回融合和 TopK 截断场景测试。")
		if hasAny(evidence, "return", "keywordsearch", "fusecitationsrrf") {
			add("补充 keywordSearch fallback 场景，验证返回结果不会绕过必要的去重和数量限制。")
		}
	case hasAny(evidence, "prompt", "llm", "answer", "impact"):
		add("补充模型调用失败、本地 fallback、无代码依据和有代码依据的回答生成测试。")
	}
	for _, path := range append(matchedPaths, uniquePaths(citations, 8)...) {
		lower := strings.ToLower(path)
		switch {
		case strings.Contains(lower, "mq") || strings.Contains(lower, "queue") || strings.Contains(lower, "consumer"):
			add("补充异步消息处理成功、业务失败、系统异常、重复投递和重试耗尽场景测试。")
		case strings.Contains(lower, "redis") || strings.Contains(lower, "cache") || strings.Contains(lower, "stream"):
			add("补充缓存命中、缓存失效、并发更新、重复请求和 Redis 异常场景测试。")
		case strings.Contains(lower, "service"):
			add("补充核心服务成功路径、异常路径、空结果和边界参数测试。")
		case strings.Contains(lower, "api") || strings.Contains(lower, "router") || strings.Contains(lower, "handler") || strings.Contains(lower, "middleware"):
			add("补充接口参数校验、未授权访问、正常响应和错误响应测试。")
		case strings.Contains(lower, "model") || strings.Contains(lower, "repository"):
			add("补充数据字段兼容、持久化读写和事务失败回滚测试。")
		}
	}
	if len(tests) == 0 {
		add("至少补充变更文件的单元测试，并执行相关 API 或集成链路回归。")
	}
	return tests
}

func impactEvidenceText(diffText string, paths []string, citations []Citation) string {
	var b strings.Builder
	b.WriteString(diffText)
	for _, path := range paths {
		b.WriteByte('\n')
		b.WriteString(path)
	}
	for _, citation := range citations {
		b.WriteByte('\n')
		b.WriteString(citation.FilePath)
		b.WriteByte('\n')
		b.WriteString(citation.SymbolName)
		b.WriteByte('\n')
		b.WriteString(citation.SymbolType)
		b.WriteByte('\n')
		b.WriteString(citation.Content)
	}
	return strings.ToLower(b.String())
}

func hasAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func matchDiffPaths(changedPaths []string, citations []Citation) ([]string, []string) {
	seenMatched := map[string]bool{}
	seenUnmatched := map[string]bool{}
	var matched []string
	var unmatched []string
	for _, changed := range changedPaths {
		if pathMatched(changed, citations) {
			if !seenMatched[changed] {
				seenMatched[changed] = true
				matched = append(matched, changed)
			}
			continue
		}
		if !seenUnmatched[changed] {
			seenUnmatched[changed] = true
			unmatched = append(unmatched, changed)
		}
	}
	return matched, unmatched
}

func pathMatched(changed string, citations []Citation) bool {
	changed = normalizePath(changed)
	if changed == "" {
		return false
	}
	for _, citation := range citations {
		path := normalizePath(citation.FilePath)
		if path == changed || strings.HasSuffix(path, "/"+changed) || strings.HasSuffix(changed, "/"+path) {
			return true
		}
	}
	return false
}

func filterCitationsByPaths(citations []Citation, paths []string) []Citation {
	if len(paths) == 0 {
		return nil
	}
	var out []Citation
	for _, citation := range citations {
		for _, path := range paths {
			if pathMatches(path, citation.FilePath) {
				out = append(out, citation)
				break
			}
		}
	}
	return out
}

func pathMatches(left, right string) bool {
	left = normalizePath(left)
	right = normalizePath(right)
	if left == "" || right == "" {
		return false
	}
	return left == right || strings.HasSuffix(left, "/"+right) || strings.HasSuffix(right, "/"+left)
}

func normalizePath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "\"")
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return strings.Trim(path, "/")
}

func uniquePaths(citations []Citation, limit int) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range citations {
		if seen[c.FilePath] {
			continue
		}
		seen[c.FilePath] = true
		out = append(out, c.FilePath)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func impactSystemPrompt() string {
	return "你是一名软件工程代码审查助手，擅长分析后端、前端、数据库、部署配置和测试代码的变更影响。必须使用中文回答。你只能依据用户提供的 diff 和检索到的代码片段进行分析，不能猜测没有证据的模块。请重点分析这次变更是否改变了调用链、状态流转、错误处理、数据读写、外部依赖、接口行为、构建部署或用户交互。回答控制在 600 字以内，结构为：变更总结、影响范围、风险点、建议测试、代码依据。每个风险点必须绑定一个具体文件、函数、字段、配置项或调用，并说明依据来自 diff 还是代码片段。如果没有足够代码依据，请降低结论强度；如果 diff 文件没有在当前仓库命中，要明确说明可信度较低。语言要自然，不要堆砌术语，不要输出泛化风险。"
}

func impactUserPrompt(diffText string, citations []Citation, matchedPaths, unmatchedPaths []string) string {
	var b strings.Builder
	b.WriteString("代码变更 diff：\n")
	b.WriteString(diffText)
	if len(matchedPaths) > 0 || len(unmatchedPaths) > 0 {
		b.WriteString("\n\n路径匹配情况：\n")
		if len(matchedPaths) > 0 {
			b.WriteString("已在当前仓库命中的变更文件：")
			b.WriteString(strings.Join(matchedPaths, "、"))
			b.WriteByte('\n')
		}
		if len(unmatchedPaths) > 0 {
			b.WriteString("未在当前仓库命中的变更文件：")
			b.WriteString(strings.Join(unmatchedPaths, "、"))
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n\n相关代码片段：\n")
	for i, c := range promptCitations(citations) {
		b.WriteString(fmt.Sprintf("\n[%d] %s:%d-%d", i+1, c.FilePath, c.StartLine, c.EndLine))
		if c.SymbolName != "" {
			b.WriteString(fmt.Sprintf(" 符号=%s 类型=%s", c.SymbolName, c.SymbolType))
		}
		b.WriteString("\n")
		b.WriteString(c.Content)
		b.WriteByte('\n')
	}
	b.WriteString("\n请用中文输出。不要使用复杂 Markdown。每段尽量短，用简单语言说明。风险和测试建议必须对应 diff 或引用代码里的具体函数、文件、字段或调用。不要把所有 service 文件都泛化成事务一致性问题。如果变更文件未在当前仓库命中，必须明确说明可信度较低，不要编造当前仓库不存在的模块或调用链。")
	return b.String()
}
