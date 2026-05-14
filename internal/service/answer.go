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

type AskStreamEvent struct {
	Type      string     `json:"type"`
	Delta     string     `json:"delta,omitempty"`
	Answer    string     `json:"answer,omitempty"`
	Citations []Citation `json:"citations,omitempty"`
	Error     string     `json:"error,omitempty"`
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

func (s *AnswerService) AskStream(ctx context.Context, repositoryID uint, question string, emit func(AskStreamEvent) error) error {
	if err := s.ensureReady(ctx, repositoryID); err != nil {
		return err
	}
	citations, err := s.retriever.Search(ctx, repositoryID, question, nil)
	if err != nil {
		return err
	}
	if err := emit(AskStreamEvent{Type: "citations", Citations: citations}); err != nil {
		return err
	}
	if len(citations) == 0 {
		answer := "没有检索到相关代码片段。"
		if err := emit(AskStreamEvent{Type: "answer", Answer: answer}); err != nil {
			return err
		}
		return emit(AskStreamEvent{Type: "done", Answer: answer})
	}

	local := s.localAnswer(question, citations)
	if s.cfg.OpenAIAPIKey == "" {
		if err := emit(AskStreamEvent{Type: "answer", Answer: local}); err != nil {
			return err
		}
		return emit(AskStreamEvent{Type: "done", Answer: local})
	}

	answer, err := streamLLM(ctx, s.cfg, codeAnswerSystemPrompt(), codeAnswerUserPrompt(s.cfg, question, citations), func(delta string) error {
		return emit(AskStreamEvent{Type: "delta", Delta: delta})
	})
	if err != nil {
		if strings.TrimSpace(answer) == "" {
			if emitErr := emit(AskStreamEvent{Type: "answer", Answer: local}); emitErr != nil {
				return emitErr
			}
			return emit(AskStreamEvent{Type: "done", Answer: local})
		}
		return err
	}
	return emit(AskStreamEvent{Type: "done", Answer: answer})
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
	return "你是一名代码仓库 RAG 分析助手。必须使用中文回答。只能依据提供的代码片段回答，不要编造不存在的函数、模块、字段或调用链。回答要中等长度，不能只给一两句话，也不要写成长篇论文。先直接回答问题，再说明主要流程、关键设计或注意点，最后列出代码依据。如果问题是“有哪些接口、路由、API”，必须优先依据路由注册、handler 函数和请求结构体回答；证据不足时要说明还缺少路由注册文件。文件名、函数名、字段名直接用普通文本写，不要频繁使用反引号。"
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
	b.WriteString("\n请用中文回答。回答要中等长度，先直接回答问题，再说明主要流程、关键设计或注意点，最后给代码依据。不要只给一两句话，也不要写成长篇论文。文件名、函数名、字段名直接用普通文本写。")
	return b.String()
}

func promptCitations(cfg config.Config, citations []Citation) []Citation {
	limit := cfg.PromptCitationLimit
	if limit <= 0 {
		limit = 5
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
