package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"code-rag-assistant/internal/config"

	"gorm.io/gorm"
)

type Citation struct {
	ID         uint    `json:"-"`
	FilePath   string  `json:"file_path"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Language   string  `json:"language"`
	SymbolName string  `json:"symbol_name"`
	SymbolType string  `json:"symbol_type"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
}

type searchFeatures struct {
	Terms     []string
	Paths     []string
	Symbols   []string
	Languages []string
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
	features := analyzeSearchFeatures(query, hints)
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
	rows, err := r.vectorSearch(ctx, repositoryID, vector, limit)
	if err != nil {
		return nil, err
	}
	keywordRows, err := r.keywordSearch(ctx, repositoryID, features, r.cfg.TopK*2)
	if err != nil {
		return nil, err
	}
	rows = mergeCitations(rows, keywordRows)
	boost(rows, query, features)
	if len(rows) > r.cfg.TopK {
		rows = rows[:r.cfg.TopK]
	}
	return rows, nil
}

func (r *Retriever) vectorSearch(ctx context.Context, repositoryID uint, vector string, limit int) ([]Citation, error) {
	var rows []Citation
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, file_path, start_line, end_line, language, symbol_name, symbol_type, content,
		       1 - (embedding_vector <=> ?::vector) AS score
		FROM code_chunks
		WHERE repository_id = ?
		ORDER BY embedding_vector <=> ?::vector
		LIMIT ?
	`, vector, repositoryID, vector, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *Retriever) keywordSearch(ctx context.Context, repositoryID uint, features searchFeatures, limit int) ([]Citation, error) {
	terms := keywordSearchTerms(features)
	if (len(terms) == 0 && len(features.Languages) == 0) || limit <= 0 {
		return nil, nil
	}

	var clauses []string
	args := []any{repositoryID}
	for _, term := range terms {
		pattern := "%" + strings.ToLower(term) + "%"
		clauses = append(clauses, "lower(file_path) LIKE ?")
		args = append(args, pattern)
		clauses = append(clauses, "lower(symbol_name) LIKE ?")
		args = append(args, pattern)
		clauses = append(clauses, "lower(content) LIKE ?")
		args = append(args, pattern)
	}
	for _, language := range features.Languages {
		clauses = append(clauses, "lower(language) = ?")
		args = append(args, strings.ToLower(language))
	}
	args = append(args, limit)

	var rows []Citation
	err := r.db.WithContext(ctx).Raw(fmt.Sprintf(`
		SELECT id, file_path, start_line, end_line, language, symbol_name, symbol_type, content,
		       0.45 AS score
		FROM code_chunks
		WHERE repository_id = ? AND (%s)
		ORDER BY id
		LIMIT ?
	`, strings.Join(clauses, " OR ")), args...).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func boost(rows []Citation, query string, features searchFeatures) {
	terms := features.Terms
	queryLower := strings.ToLower(query)

	for i := range rows {
		var bonus float64
		filePath := strings.ToLower(rows[i].FilePath)
		symbolName := strings.ToLower(rows[i].SymbolName)
		symbolType := strings.ToLower(rows[i].SymbolType)
		content := strings.ToLower(rows[i].Content)
		language := strings.ToLower(rows[i].Language)

		if symbolName != "" && strings.Contains(queryLower, symbolName) {
			bonus += 0.30
		}
		for _, path := range features.Paths {
			if filePath == path || strings.HasSuffix(filePath, "/"+path) || strings.HasSuffix(filePath, path) {
				bonus += 0.24
				break
			}
		}
		for _, symbol := range features.Symbols {
			if symbolName == symbol {
				bonus += 0.25
				break
			}
		}
		for _, lang := range features.Languages {
			if language == lang {
				bonus += 0.06
				break
			}
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
			if bonus >= 0.60 {
				bonus = 0.60
				break
			}
		}
		rows[i].Score += bonus
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Score > rows[j].Score
	})
}

func analyzeSearchFeatures(query string, hints []string) searchFeatures {
	text := query + "\n" + strings.Join(hints, "\n")
	rawTerms := splitSearchTerms(text)
	features := searchFeatures{}
	seenTerms := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	seenSymbols := map[string]struct{}{}
	seenLanguages := map[string]struct{}{}

	addTerm := func(term string) {
		term = strings.ToLower(strings.TrimSpace(term))
		if _, ok := seenTerms[term]; len([]rune(term)) < 2 || ok {
			return
		}
		seenTerms[term] = struct{}{}
		features.Terms = append(features.Terms, term)
	}
	addPath := func(path string) {
		path = strings.ToLower(strings.Trim(path, "`'\"，。；,;()[]{}<>"))
		if _, ok := seenPaths[path]; path == "" || ok {
			return
		}
		seenPaths[path] = struct{}{}
		features.Paths = append(features.Paths, path)
	}
	addSymbol := func(symbol string) {
		symbol = strings.ToLower(strings.Trim(symbol, "`'\"，。；,;()[]{}<>"))
		if _, ok := seenSymbols[symbol]; len(symbol) < 3 || ok {
			return
		}
		seenSymbols[symbol] = struct{}{}
		features.Symbols = append(features.Symbols, symbol)
	}
	addLanguage := func(language string) {
		if _, ok := seenLanguages[language]; language == "" || ok {
			return
		}
		seenLanguages[language] = struct{}{}
		features.Languages = append(features.Languages, language)
	}

	for _, term := range rawTerms {
		addTerm(term)
		lower := strings.ToLower(term)
		if isPathLike(lower) {
			addPath(lower)
		}
		if isSymbolLike(term) {
			addSymbol(term)
		}
	}
	for _, alias := range matchedAliases(text) {
		addTerm(alias)
	}

	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "golang") || strings.Contains(lowerText, "go 文件") || strings.Contains(lowerText, "go文件") {
		addLanguage("go")
	}
	if strings.Contains(lowerText, "dockerfile") {
		addLanguage("dockerfile")
	}
	if strings.Contains(lowerText, "yaml") || strings.Contains(lowerText, "yml") {
		addLanguage("yaml")
	}
	if strings.Contains(lowerText, "json") {
		addLanguage("json")
	}
	if strings.Contains(lowerText, "sql") {
		addLanguage("sql")
	}

	return features
}

func keywordSearchTerms(features searchFeatures) []string {
	seen := map[string]struct{}{}
	var terms []string
	add := func(term string) {
		term = strings.ToLower(strings.TrimSpace(term))
		if _, ok := seen[term]; len([]rune(term)) < 2 || len([]rune(term)) > 60 || ok || isWeakKeyword(term) {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	for _, path := range features.Paths {
		add(path)
	}
	for _, symbol := range features.Symbols {
		add(symbol)
	}
	for _, term := range features.Terms {
		add(term)
		if len(terms) >= 16 {
			break
		}
	}
	return terms
}

func mergeCitations(groups ...[]Citation) []Citation {
	seen := map[string]int{}
	var merged []Citation
	for _, group := range groups {
		for _, row := range group {
			key := citationKey(row)
			if idx, ok := seen[key]; ok {
				if row.Score > merged[idx].Score {
					merged[idx].Score = row.Score
				}
				continue
			}
			seen[key] = len(merged)
			merged = append(merged, row)
		}
	}
	return merged
}

func citationKey(row Citation) string {
	if row.ID != 0 {
		return fmt.Sprintf("id:%d", row.ID)
	}
	return fmt.Sprintf("%s:%d:%d:%s", row.FilePath, row.StartLine, row.EndLine, row.SymbolName)
}

func splitSearchTerms(text string) []string {
	text = normalizeSearchText(text)
	return strings.FieldsFunc(text, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '/' || r == '-')
	})
}

func normalizeSearchText(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 8)
	for i, r := range runes {
		if i > 0 && shouldInsertSearchSpace(runes[i-1], r) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func shouldInsertSearchSpace(prev, cur rune) bool {
	return (isCodeSearchRune(prev) && isCJKRune(cur)) ||
		(isCJKRune(prev) && isCodeSearchRune(cur))
}

func isCodeSearchRune(r rune) bool {
	return (unicode.IsLetter(r) && !isCJKRune(r)) ||
		unicode.IsDigit(r) ||
		r == '_' ||
		r == '.' ||
		r == '/' ||
		r == '-'
}

func isCJKRune(r rune) bool {
	return unicode.In(r, unicode.Han)
}

func isPathLike(term string) bool {
	return strings.Contains(term, "/") ||
		strings.HasSuffix(term, ".go") ||
		strings.HasSuffix(term, ".yaml") ||
		strings.HasSuffix(term, ".yml") ||
		strings.HasSuffix(term, ".json") ||
		strings.HasSuffix(term, ".sql") ||
		strings.HasSuffix(term, ".md") ||
		strings.HasSuffix(term, "dockerfile")
}

func isSymbolLike(term string) bool {
	if isReservedSearchWord(term) {
		return false
	}
	if strings.Contains(term, "/") || strings.Contains(term, ".go") {
		return false
	}
	for _, r := range term {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.') {
			return false
		}
	}
	return strings.Contains(term, "_") ||
		strings.Contains(term, ".") ||
		hasUppercase(term) ||
		isLowercaseIdentifier(term)
}

func hasUppercase(term string) bool {
	for _, r := range term {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func isLowercaseIdentifier(term string) bool {
	if len([]rune(term)) < 4 {
		return false
	}
	for _, r := range term {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isReservedSearchWord(term string) bool {
	switch strings.ToLower(term) {
	case "go", "golang", "yaml", "yml", "json", "sql", "dockerfile":
		return true
	default:
		return false
	}
}

func isWeakKeyword(term string) bool {
	switch term {
	case "go", "golang", "yaml", "yml", "json", "sql", "dockerfile", "func", "type", "var", "const", "这个", "项目", "代码", "逻辑", "什么", "怎么", "如何":
		return true
	default:
		return false
	}
}
