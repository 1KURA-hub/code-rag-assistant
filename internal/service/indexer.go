package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"code-rag-assistant/internal/config"
	"code-rag-assistant/internal/model"
	"code-rag-assistant/internal/util"

	"gorm.io/gorm"
)

type CodeIndexer struct {
	db       *gorm.DB
	embedder *Embedder
	cfg      config.Config
}

type IndexStats struct {
	FileCount       int
	ChunkCount      int
	IndexDurationMS int64
}

func NewCodeIndexer(db *gorm.DB, embedder *Embedder, cfg config.Config) *CodeIndexer {
	return &CodeIndexer{db: db, embedder: embedder, cfg: cfg}
}

func (i *CodeIndexer) IndexRepository(ctx context.Context, repo *model.Repository, root string) (IndexStats, error) {
	started := time.Now()
	files, err := util.ScanSourceFiles(root)
	if err != nil {
		return IndexStats{}, err
	}
	if err := i.db.WithContext(ctx).Where("repository_id = ?", repo.ID).Delete(&model.CodeChunk{}).Error; err != nil {
		return IndexStats{}, err
	}
	chunkCount := 0
	for _, file := range files {
		chunks := ChunkSourceFile(file, i.cfg.ChunkMaxLines, i.cfg.ChunkOverlapLines)
		for idx, chunk := range chunks {
			embeddingText := chunkEmbeddingText(file.Path, chunk)
			vector, err := i.embedder.Embed(ctx, embeddingText)
			if err != nil {
				return IndexStats{}, fmt.Errorf("embed %s:%d-%d: %w", file.Path, chunk.StartLine, chunk.EndLine, err)
			}
			record := &model.CodeChunk{
				RepositoryID:    repo.ID,
				FilePath:        file.Path,
				StartLine:       chunk.StartLine,
				EndLine:         chunk.EndLine,
				ChunkIndex:      idx,
				Language:        chunkLanguage(file.Path, chunk),
				SymbolName:      chunk.SymbolName,
				SymbolType:      chunk.SymbolType,
				Content:         chunk.Content,
				EmbeddingVector: VectorLiteral(vector),
			}
			if err := i.db.WithContext(ctx).Create(record).Error; err != nil {
				return IndexStats{}, err
			}
			chunkCount++
		}
	}
	return IndexStats{
		FileCount:       len(files),
		ChunkCount:      chunkCount,
		IndexDurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func chunkEmbeddingText(path string, chunk Chunk) string {
	var b strings.Builder
	b.WriteString("path: ")
	b.WriteString(path)
	b.WriteByte('\n')
	if chunk.Language != "" {
		b.WriteString("language: ")
		b.WriteString(chunk.Language)
		b.WriteByte('\n')
	}
	if chunk.SymbolName != "" {
		b.WriteString("symbol: ")
		b.WriteString(chunk.SymbolName)
		b.WriteByte('\n')
	}
	if chunk.SymbolType != "" {
		b.WriteString("symbol_type: ")
		b.WriteString(chunk.SymbolType)
		b.WriteByte('\n')
	}
	b.WriteString(chunk.Content)
	return b.String()
}

func chunkLanguage(path string, chunk Chunk) string {
	if chunk.Language != "" {
		return chunk.Language
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return ext
}
