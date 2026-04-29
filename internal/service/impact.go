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
	if generated, err := callLLM(ctx, s.cfg, impactSystemPrompt(), impactUserPrompt(diffText, citations)); err == nil {
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
	modules := uniquePaths(citations, 5)
	changedPaths := util.ExtractDiffPaths(diffText)
	risks := inferRisks(changedPaths, citations)
	tests := inferTests(changedPaths, citations)
	summary := buildImpactSummary(changedPaths, citations)
	if strings.TrimSpace(diffText) == "" {
		summary = "没有提供 diff 内容。"
	}
	return &ImpactResponse{
		Summary:         summary,
		ImpactedModules: modules,
		Risks:           risks,
		SuggestedTests:  tests,
		Citations:       citations,
	}
}

func buildImpactSummary(changedPaths []string, citations []Citation) string {
	if len(changedPaths) == 0 {
		return "没有从 diff 中解析到明确文件路径，当前结果主要基于 diff 文本和向量检索召回的代码片段。"
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
	return b.String()
}

func inferRisks(changedPaths []string, citations []Citation) []string {
	seen := map[string]bool{}
	var risks []string
	add := func(value string) {
		if !seen[value] {
			seen[value] = true
			risks = append(risks, value)
		}
	}
	for _, path := range append(changedPaths, uniquePaths(citations, 8)...) {
		lower := strings.ToLower(path)
		switch {
		case strings.Contains(lower, "mq") || strings.Contains(lower, "consumer") || strings.Contains(lower, "publisher"):
			add("消息消费、ACK/NACK、重试或死信队列行为可能受影响，需要关注重复消费和消息丢失。")
		case strings.Contains(lower, "redis") || strings.Contains(lower, "relay") || strings.Contains(lower, "stream"):
			add("Redis Stream 转发、pending 消息回收或去重链路可能受影响，需要关注消息堆积和重复投递。")
		case strings.Contains(lower, "service") || strings.Contains(lower, "select"):
			add("核心业务服务逻辑可能受影响，需要关注库存扣减、重复选课和最终一致性。")
		case strings.Contains(lower, "api") || strings.Contains(lower, "router") || strings.Contains(lower, "middleware"):
			add("接口入口或中间件行为可能受影响，需要关注鉴权、参数校验和错误返回。")
		case strings.Contains(lower, "model") || strings.Contains(lower, "repository"):
			add("数据模型或持久化访问可能受影响，需要关注字段兼容性和事务一致性。")
		}
	}
	if len(risks) == 0 {
		add("需要结合引用代码片段人工确认变更是否影响调用方、错误处理和边界条件。")
	}
	return risks
}

func inferTests(changedPaths []string, citations []Citation) []string {
	seen := map[string]bool{}
	var tests []string
	add := func(value string) {
		if !seen[value] {
			seen[value] = true
			tests = append(tests, value)
		}
	}
	for _, path := range append(changedPaths, uniquePaths(citations, 8)...) {
		lower := strings.ToLower(path)
		switch {
		case strings.Contains(lower, "mq") || strings.Contains(lower, "consumer"):
			add("补充 MQ 消费成功、业务失败、系统异常、重试耗尽和死信流转测试。")
		case strings.Contains(lower, "redis") || strings.Contains(lower, "stream") || strings.Contains(lower, "relay"):
			add("补充 Redis Stream 正常转发、pending reclaim、重复消息和阻塞读取测试。")
		case strings.Contains(lower, "service") || strings.Contains(lower, "select"):
			add("补充选课成功、库存不足、重复选课和数据库异常场景测试。")
		case strings.Contains(lower, "api") || strings.Contains(lower, "router") || strings.Contains(lower, "middleware"):
			add("补充接口参数校验、未授权访问、正常响应和错误响应测试。")
		}
	}
	if len(tests) == 0 {
		add("至少补充变更文件的单元测试，并执行相关 API 或集成链路回归。")
	}
	return tests
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

func impactUserPrompt(diffText string, citations []Citation) string {
	var b strings.Builder
	b.WriteString("代码变更 diff：\n")
	b.WriteString(diffText)
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
	b.WriteString("\n请用中文输出。不要使用复杂 Markdown。每段尽量短，用简单语言说明。")
	return b.String()
}
