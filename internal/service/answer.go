package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"code-rag-assistant/internal/config"
	"code-rag-assistant/internal/model"

	"gorm.io/gorm"
)

type AnswerService struct {
	db        *gorm.DB
	retriever *Retriever
	cfg       config.Config
}

type AskResponse struct {
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations"`
}

func NewAnswerService(db *gorm.DB, retriever *Retriever, cfg config.Config) *AnswerService {
	return &AnswerService{db: db, retriever: retriever, cfg: cfg}
}

func (s *AnswerService) Ask(ctx context.Context, repositoryID uint, question string) (*AskResponse, error) {
	if err := s.ensureReady(ctx, repositoryID); err != nil {
		return nil, err
	}
	citations, err := s.retriever.Search(ctx, repositoryID, question, nil)
	if err != nil {
		return nil, err
	}
	if len(citations) == 0 {
		return &AskResponse{Answer: "没有检索到相关代码片段。", Citations: citations}, nil
	}
	answer := s.localAnswer(question, citations)
	if generated, err := callLLM(ctx, s.cfg, codeAnswerSystemPrompt(), codeAnswerUserPrompt(s.cfg, question, citations)); err == nil {
		answer = generated
	}
	return &AskResponse{Answer: answer, Citations: citations}, nil
}

func (s *AnswerService) ensureReady(ctx context.Context, repositoryID uint) error {
	var repo model.Repository
	if err := s.db.WithContext(ctx).First(&repo, repositoryID).Error; err != nil {
		return err
	}
	if repo.Status != "ready" {
		return fmt.Errorf("repository is %s", repo.Status)
	}
	return nil
}

func (s *AnswerService) localAnswer(question string, citations []Citation) string {
	var b strings.Builder
	b.WriteString("已为问题检索到相关代码片段：")
	b.WriteString(question)
	b.WriteString("\n\n")
	for i, c := range citations {
		if i >= 3 {
			break
		}
		label := c.FilePath
		if c.SymbolName != "" {
			label += " " + c.SymbolName
		}
		b.WriteString(fmt.Sprintf("- %s:%d-%d 可能相关。\n", label, c.StartLine, c.EndLine))
	}
	b.WriteString("\n当前使用本地 fallback。配置模型 API Key 后，可以生成更完整的中文分析。")
	return b.String()
}

func codeAnswerSystemPrompt() string {
	return "你是一名代码仓库分析助手。必须使用中文回答。只能依据提供的代码片段回答，不要编造不存在的函数、模块、字段或调用链。回答保持简洁，不要写长篇分析。格式固定为三段：第一段直接回答问题；第二段说明主要执行流程或设计原因；第三段列少量代码依据，包含文件路径、函数名或行号。证据不足时直接说明证据不足。"
}

func codeAnswerUserPrompt(cfg config.Config, question string, citations []Citation) string {
	var b strings.Builder
	b.WriteString("用户问题：\n")
	b.WriteString(question)
	b.WriteString("\n\n相关代码片段：\n")
	for i, c := range promptCitations(cfg, citations) {
		b.WriteString(fmt.Sprintf("\n[%d] %s:%d-%d", i+1, c.FilePath, c.StartLine, c.EndLine))
		if c.SymbolName != "" {
			b.WriteString(fmt.Sprintf(" 符号=%s 类型=%s", c.SymbolName, c.SymbolType))
		}
		b.WriteString("\n")
		b.WriteString(c.Content)
		b.WriteByte('\n')
	}
	b.WriteString("\n请用中文回答。回答保持简洁，不要写长篇分析。先直接回答问题，再说明主要流程或设计原因，最后给少量代码依据。")
	return b.String()
}

func promptCitations(cfg config.Config, citations []Citation) []Citation {
	limit := cfg.PromptCitationLimit
	if limit <= 0 {
		limit = 4
	}
	if len(citations) < limit {
		limit = len(citations)
	}
	maxChars := cfg.PromptChunkMaxChars
	if maxChars <= 0 {
		maxChars = 1200
	}
	out := make([]Citation, 0, limit)
	for _, citation := range citations[:limit] {
		citation.Content = truncatePromptContent(citation.Content, maxChars)
		out = append(out, citation)
	}
	return out
}

func truncatePromptContent(content string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	return string(runes[:maxChars]) + "\n...（内容已截断）"
}

var ErrRepositoryNotReady = errors.New("repository is not ready")
