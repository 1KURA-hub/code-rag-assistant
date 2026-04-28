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
		return &AskResponse{Answer: "No relevant code chunks were found.", Citations: citations}, nil
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
	b.WriteString("Relevant code was found for: ")
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
		b.WriteString(fmt.Sprintf("- %s:%d-%d appears relevant.\n", label, c.StartLine, c.EndLine))
	}
	b.WriteString("\nConfigure OPENAI_API_KEY to enable full natural-language analysis.")
	return b.String()
}

func codeAnswerSystemPrompt() string {
	return "You are a senior engineer explaining a codebase. Answer only from the provided snippets. If evidence is insufficient, say so. Mention file paths when relevant."
}

func codeAnswerUserPrompt(question string, citations []Citation) string {
	var b strings.Builder
	b.WriteString("Question:\n")
	b.WriteString(question)
	b.WriteString("\n\nCode snippets:\n")
	for i, c := range citations {
		b.WriteString(fmt.Sprintf("\n[%d] %s:%d-%d", i+1, c.FilePath, c.StartLine, c.EndLine))
		if c.SymbolName != "" {
			b.WriteString(fmt.Sprintf(" symbol=%s type=%s", c.SymbolName, c.SymbolType))
		}
		b.WriteString("\n")
		b.WriteString(c.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

var ErrRepositoryNotReady = errors.New("repository is not ready")
