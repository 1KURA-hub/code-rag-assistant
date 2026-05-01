package service

import (
	"context"
	"sort"
	"strings"
	"unicode"

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
	boost(rows, query, hints)
	if len(rows) > r.cfg.TopK {
		rows = rows[:r.cfg.TopK]
	}
	return rows, nil
}

func boost(rows []Citation, query string, hints []string) {
	terms := rerankTerms(query, hints)
	queryLower := strings.ToLower(query)

	for i := range rows {
		var bonus float64
		filePath := strings.ToLower(rows[i].FilePath)
		symbolName := strings.ToLower(rows[i].SymbolName)
		symbolType := strings.ToLower(rows[i].SymbolType)
		content := strings.ToLower(rows[i].Content)

		if symbolName != "" && strings.Contains(queryLower, symbolName) {
			bonus += 0.18
		}
		if symbolType == "function" || symbolType == "method" {
			bonus += 0.02
		}
		for _, term := range terms {
			switch {
			case symbolName != "" && strings.Contains(symbolName, term):
				bonus += 0.12
			case strings.Contains(filePath, term):
				bonus += 0.08
			case strings.Contains(content, term):
				bonus += 0.03
			}
			if bonus >= 0.35 {
				bonus = 0.35
				break
			}
		}
		rows[i].Score += bonus
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Score > rows[j].Score
	})
}

func rerankTerms(query string, hints []string) []string {
	seen := map[string]bool{}
	var terms []string
	add := func(term string) {
		term = strings.ToLower(strings.TrimSpace(term))
		if len([]rune(term)) < 2 || seen[term] {
			return
		}
		seen[term] = true
		terms = append(terms, term)
	}

	for _, term := range splitSearchTerms(query) {
		add(term)
	}
	for _, hint := range hints {
		add(hint)
		for _, term := range splitSearchTerms(hint) {
			add(term)
		}
	}
	for _, alias := range matchedAliases(query + "\n" + strings.Join(hints, "\n")) {
		add(alias)
	}
	return terms
}

func splitSearchTerms(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '/' || r == '-')
	})
}
