package service

import (
	"context"
	"sort"
	"strings"

	"code-rag-assistant/internal/config"

	"gorm.io/gorm"
)

type Citation struct {
	FilePath   string  `json:"file_path"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Language   string  `json:"language"`
	SymbolName string  `json:"symbol_name"`
	SymbolType string  `json:"symbol_type"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
}

type Retriever struct {
	db       *gorm.DB
	embedder *Embedder
	cfg      config.Config
}

func NewRetriever(db *gorm.DB, embedder *Embedder, cfg config.Config) *Retriever {
	return &Retriever{db: db, embedder: embedder, cfg: cfg}
}

func (r *Retriever) Search(ctx context.Context, repositoryID uint, query string, hints []string) ([]Citation, error) {
	expandedQuery := expandQueryText(query, hints)
	embedding, err := r.embedder.Embed(ctx, expandedQuery)
	if err != nil {
		return nil, err
	}
	vector := VectorLiteral(embedding)
	limit := r.cfg.TopK * 3
	if limit < r.cfg.TopK {
		limit = r.cfg.TopK
	}
	var rows []Citation
	err = r.db.WithContext(ctx).Raw(`
		SELECT file_path, start_line, end_line, language, symbol_name, symbol_type, content,
		       1 - (embedding_vector <=> ?::vector) AS score
		FROM code_chunks
		WHERE repository_id = ?
		ORDER BY embedding_vector <=> ?::vector
		LIMIT ?
	`, vector, repositoryID, vector, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	boost(rows, append(hints, matchedAliases(expandedQuery)...))
	if len(rows) > r.cfg.TopK {
		rows = rows[:r.cfg.TopK]
	}
	return rows, nil
}

func boost(rows []Citation, hints []string) {
	normalized := make([]string, 0, len(hints))
	for _, hint := range hints {
		hint = strings.ToLower(strings.TrimSpace(hint))
		if hint != "" {
			normalized = append(normalized, hint)
		}
	}
	for i := range rows {
		target := strings.ToLower(rows[i].FilePath + "\n" + rows[i].Content)
		for _, hint := range normalized {
			if strings.Contains(target, hint) {
				rows[i].Score += 0.08
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Score > rows[j].Score
	})
}
