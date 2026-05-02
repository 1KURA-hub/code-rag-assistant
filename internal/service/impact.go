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
	risks := inferRisks(matchedPaths, matchedCitations)
	tests := inferTests(matchedPaths, matchedCitations)
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

func inferRisks(matchedPaths []string, citations []Citation) []string {
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
	for _, path := range append(matchedPaths, uniquePaths(citations, 8)...) {
		lower := strings.ToLower(path)
		switch {
		case strings.Contains(lower, "mq") || strings.Contains(lower, "queue") || strings.Contains(lower, "consumer") || strings.Contains(lower, "publisher"):
			add("异步消息链路可能受影响，需要关注重复处理、确认时机、失败重试和消息丢失。")
		case strings.Contains(lower, "redis") || strings.Contains(lower, "cache") || strings.Contains(lower, "stream"):
			add("缓存或 Redis 相关逻辑可能受影响，需要关注缓存一致性、过期时间、重复请求和并发更新。")
		case strings.Contains(lower, "service"):
			add("核心服务逻辑可能受影响，需要关注调用链、错误处理、边界条件和事务一致性。")
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

func inferTests(matchedPaths []string, citations []Citation) []string {
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
	for _, path := range append(matchedPaths, uniquePaths(citations, 8)...) {
		lower := strings.ToLower(path)
		switch {
		case strings.Contains(lower, "mq") || strings.Contains(lower, "queue") || strings.Contains(lower, "consumer"):
			add("补充异步消息处理成功、业务失败、系统异常、重复投递和重试耗尽场景测试。")
		case strings.Contains(lower, "redis") || strings.Contains(lower, "cache") || strings.Contains(lower, "stream"):
			add("补充缓存命中、缓存失效、并发更新、重复请求和 Redis 异常场景测试。")
		case strings.Contains(lower, "service"):
			add("补充核心服务成功路径、异常路径、边界参数和事务一致性测试。")
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
	return "你是一名后端代码审查讲解助手。必须使用中文回答。语言要简单、直接，不要写长篇 Markdown，不要使用大量加粗、标题、分割线或项目符号。只能依据 diff 和提供的代码片段分析，不要编造不存在的调用链。回答格式固定为四段：这次变更大概影响什么；可能的风险；建议怎么测试；代码依据。"
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
	b.WriteString("\n请用中文输出。不要使用复杂 Markdown。每段尽量短，用简单语言说明。如果变更文件未在当前仓库命中，必须明确说明可信度较低，不要编造当前仓库不存在的模块或调用链。")
	return b.String()
}
