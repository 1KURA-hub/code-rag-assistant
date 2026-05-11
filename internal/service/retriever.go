package service

import (
	"context"
	"fmt"
	"math"
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
	Terms       []string
	Paths       []string
	Symbols     []string
	SymbolTypes []string
	Languages   []string
}

const defaultRRFK = 40.0

type Retriever struct {
	db       *gorm.DB
	embedder *Embedder
	cfg      config.Config
}

func NewRetriever(db *gorm.DB, embedder *Embedder, cfg config.Config) *Retriever {
	return &Retriever{db: db, embedder: embedder, cfg: cfg}
}

func (r *Retriever) Search(ctx context.Context, repositoryID uint, query string, hints []string) ([]Citation, error) {
	plan := buildQueryPlan(ctx, r.cfg, query, hints)
	embedding, err := r.embedder.Embed(ctx, plan.EmbeddingText)
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
	keywordRows, err := r.keywordSearch(ctx, repositoryID, plan.Features, r.cfg.TopK*2)
	if err != nil {
		return nil, err
	}
	rows = fuseCitationsRRF(r.cfg.RRFK, rows, keywordRows)
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
	query, args := buildKeywordSearchQuery(repositoryID, features, limit)
	if query == "" {
		return nil, nil
	}
	var rows []Citation
	err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func buildKeywordSearchQuery(repositoryID uint, features searchFeatures, limit int) (string, []any) {
	terms := keywordContentTerms(features)
	if (len(features.Paths) == 0 && len(features.Symbols) == 0 && len(features.SymbolTypes) == 0 && len(terms) == 0 && len(features.Languages) == 0) || limit <= 0 {
		return "", nil
	}

	var clauses []string
	var rankParts []string
	var rankArgs []any
	whereArgs := []any{repositoryID}
	addFullText := func(term string) {
		clauses = append(clauses, "search_vector @@ plainto_tsquery('simple', ?)")
		whereArgs = append(whereArgs, term)
		rankParts = append(rankParts, "ts_rank(search_vector, plainto_tsquery('simple', ?))")
		rankArgs = append(rankArgs, term)
	}
	addLike := func(field string, term string, boost float64) {
		pattern := "%" + strings.ToLower(term) + "%"
		clauses = append(clauses, fmt.Sprintf("lower(%s) LIKE ?", field))
		whereArgs = append(whereArgs, pattern)
		if boost > 0 {
			rankParts = append(rankParts, fmt.Sprintf("CASE WHEN lower(%s) LIKE ? THEN %.2f ELSE 0 END", field, boost))
			rankArgs = append(rankArgs, pattern)
		}
	}
	for _, path := range features.Paths {
		addLike("file_path", path, 2.0)
	}
	for _, symbol := range features.Symbols {
		addLike("symbol_name", symbol, 2.5)
	}
	for _, symbolType := range features.SymbolTypes {
		clauses = append(clauses, "lower(symbol_type) = ?")
		whereArgs = append(whereArgs, strings.ToLower(symbolType))
		rankParts = append(rankParts, "CASE WHEN lower(symbol_type) = ? THEN 0.40 ELSE 0 END")
		rankArgs = append(rankArgs, strings.ToLower(symbolType))
	}
	for _, term := range terms {
		addLike("file_path", term, 0.8)
		addLike("symbol_name", term, 0.8)
		addFullText(term)
	}
	for _, language := range features.Languages {
		clauses = append(clauses, "lower(language) = ?")
		whereArgs = append(whereArgs, strings.ToLower(language))
		rankParts = append(rankParts, "CASE WHEN lower(language) = ? THEN 0.40 ELSE 0 END")
		rankArgs = append(rankArgs, strings.ToLower(language))
	}
	scoreSQL := "0.45"
	if len(rankParts) > 0 {
		scoreSQL += " + " + strings.Join(rankParts, " + ")
	}
	args := append([]any{}, rankArgs...)
	args = append(args, whereArgs...)
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT id, file_path, start_line, end_line, language, symbol_name, symbol_type, content,
		       %s AS score
		FROM code_chunks
		WHERE repository_id = ? AND (%s)
		ORDER BY score DESC, id
		LIMIT ?
	`, scoreSQL, strings.Join(clauses, " OR "))
	return query, args
}

func analyzeSearchFeatures(query string, hints []string) searchFeatures {
	text := query + "\n" + strings.Join(hints, "\n")
	rawTerms := splitSearchTerms(text)
	features := searchFeatures{}
	seenTerms := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	seenSymbols := map[string]struct{}{}
	seenSymbolTypes := map[string]struct{}{}
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
	addSymbolType := func(symbolType string) {
		if _, ok := seenSymbolTypes[symbolType]; symbolType == "" || ok {
			return
		}
		seenSymbolTypes[symbolType] = struct{}{}
		features.SymbolTypes = append(features.SymbolTypes, symbolType)
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
	for _, symbolType := range detectSymbolTypes(text) {
		addSymbolType(symbolType)
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

func detectSymbolTypes(text string) []string {
	lowerText := strings.ToLower(text)
	types := make([]string, 0, 4)
	add := func(symbolType string) {
		for _, existing := range types {
			if existing == symbolType {
				return
			}
		}
		types = append(types, symbolType)
	}
	if strings.Contains(lowerText, "func") ||
		strings.Contains(text, "函数") ||
		strings.Contains(text, "方法") ||
		strings.Contains(text, "接口入口") ||
		strings.Contains(text, "调用链") {
		add("function")
		add("method")
	}
	if strings.Contains(lowerText, "struct") ||
		strings.Contains(lowerText, "interface") ||
		strings.Contains(text, "结构体") ||
		strings.Contains(text, "类型") ||
		strings.Contains(text, "模型") {
		add("type")
	}
	if strings.Contains(lowerText, "const") ||
		strings.Contains(text, "常量") ||
		strings.Contains(text, "状态码") {
		add("const")
	}
	if strings.Contains(lowerText, "var") ||
		strings.Contains(text, "变量") ||
		strings.Contains(text, "全局变量") {
		add("var")
	}
	return types
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

func keywordContentTerms(features searchFeatures) []string {
	seen := map[string]struct{}{}
	for _, path := range features.Paths {
		seen[path] = struct{}{}
	}
	for _, symbol := range features.Symbols {
		seen[symbol] = struct{}{}
	}

	var terms []string
	for _, term := range features.Terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if _, ok := seen[term]; len([]rune(term)) < 2 || len([]rune(term)) > 60 || ok || isWeakKeyword(term) {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
		if len(terms) >= 16 {
			break
		}
	}
	return terms
}

func fuseCitationsRRF(k float64, groups ...[]Citation) []Citation {
	if k <= 0 {
		k = defaultRRFK
	}
	seen := map[string]int{}
	var merged []Citation
	for _, group := range groups {
		for rank, row := range group {
			key := citationKey(row)
			rrfScore := 1 / (k + float64(rank+1))
			if idx, ok := seen[key]; ok {
				merged[idx].Score += rrfScore
				continue
			}
			row.Score = rrfScore
			seen[key] = len(merged)
			merged = append(merged, row)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if math.Abs(merged[i].Score-merged[j].Score) < 1e-12 {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].Score > merged[j].Score
	})
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
	case "go", "golang", "yaml", "yml", "json", "sql", "dockerfile",
		"redis", "rabbitmq", "mysql", "postgres", "postgresql", "pgvector",
		"jwt", "lua", "stream", "cache", "http", "api", "docker", "gorm", "gin":
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
