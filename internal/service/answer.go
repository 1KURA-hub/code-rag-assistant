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
	return "你是一名代码仓库 RAG 分析助手，擅长理解后端、前端、数据库、部署配置和测试代码。必须使用中文回答。你只能依据提供的代码片段回答，不能编造不存在的函数、模块、字段或调用链。请先直接回答用户问题，再按真实执行流程或代码结构解释核心逻辑，最后给出少量代码依据。如果用户问为什么这样设计，请结合代码结构说明设计目的、优点和限制。如果用户问函数逻辑，请按代码顺序说明输入、关键变量、调用关系和返回结果。如果涉及框架、标准库、数据库、中间件、HTTP、Docker、RAG、embedding、向量检索等技术点，请解释它们在当前代码中的具体作用。如果证据不足，请明确说明依据不足，并指出还需要查看哪些文件或函数。回答要像人正常讲解代码，不要写成论文。少用 Markdown，少用反引号，文件名、函数名、字段名一般直接写普通文本即可。不要频繁使用加粗、编号、小标题或复杂符号。列表最多 4 条，优先使用自然段。"
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
