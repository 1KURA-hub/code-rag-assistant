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
	if generated, err := callLLM(ctx, s.cfg, codeAnswerSystemPrompt(), codeAnswerUserPrompt(question, citations)); err == nil {
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
	return "你是一名后端项目讲解助手，正在给学生解释代码。必须使用中文回答。语言要简单、直接，避免复杂术语堆叠。不要写长篇 Markdown，不要使用大量加粗、标题、分割线或项目符号。只能依据提供的代码片段回答；如果证据不足，直接说证据不足。回答格式固定为三段：第一段用自然语言直接回答问题；第二段按执行流程解释；第三段列出少量代码依据，包含文件路径、函数名或行号。"
}

func codeAnswerUserPrompt(question string, citations []Citation) string {
	var b strings.Builder
	b.WriteString("用户问题：\n")
	b.WriteString(question)
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
	b.WriteString("\n请用中文回答。不要使用复杂 Markdown。不要把回答写成论文。先用简单自然语言解释，再给执行流程，最后给少量代码依据。")
	return b.String()
}

func promptCitations(citations []Citation) []Citation {
	if len(citations) > 5 {
		return citations[:5]
	}
	return citations
}

var ErrRepositoryNotReady = errors.New("repository is not ready")
