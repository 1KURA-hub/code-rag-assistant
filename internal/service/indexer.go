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

type chunkEmbeddingItem struct {
	filePath   string
	chunk      Chunk
	chunkIndex int
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
	chunkItems := make([]chunkEmbeddingItem, 0, len(files)*2)
	for _, file := range files {
		chunks := ChunkSourceFile(file, i.cfg.ChunkMaxLines, i.cfg.ChunkOverlapLines)
		for idx, chunk := range chunks {
			chunkItems = append(chunkItems, chunkEmbeddingItem{filePath: file.Path, chunk: chunk, chunkIndex: idx})
		}
	}

	records := make([]*model.CodeChunk, 0, len(chunkItems))
	batchSize := i.cfg.EmbeddingBatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	for start := 0; start < len(chunkItems); start += batchSize {
		end := start + batchSize
		if end > len(chunkItems) {
			end = len(chunkItems)
		}
		chunkBatch := chunkItems[start:end]
		embeddingTexts := make([]string, len(chunkBatch))
		for idx, chunkItem := range chunkBatch {
			embeddingTexts[idx] = chunkEmbeddingText(chunkItem.filePath, chunkItem.chunk)
		}
		embeddingVectors, err := i.embedder.EmbedBatch(ctx, embeddingTexts)
		if err != nil {
			first := chunkBatch[0]
			last := chunkBatch[len(chunkBatch)-1]
			return IndexStats{}, fmt.Errorf("embed batch %s:%d-%d to %s:%d-%d: %w",
				first.filePath, first.chunk.StartLine, first.chunk.EndLine,
				last.filePath, last.chunk.StartLine, last.chunk.EndLine, err)
		}
		if len(embeddingVectors) != len(chunkBatch) {
			return IndexStats{}, fmt.Errorf("embed batch count mismatch: got %d want %d", len(embeddingVectors), len(chunkBatch))
		}
		for idx, chunkItem := range chunkBatch {
			chunk := chunkItem.chunk
			record := &model.CodeChunk{
				RepositoryID:    repo.ID,
				FilePath:        chunkItem.filePath,
				StartLine:       chunk.StartLine,
				EndLine:         chunk.EndLine,
				ChunkIndex:      chunkItem.chunkIndex,
				Language:        chunkLanguage(chunkItem.filePath, chunk),
				SymbolName:      chunk.SymbolName,
				SymbolType:      chunk.SymbolType,
				Content:         chunk.Content,
				EmbeddingVector: VectorLiteral(embeddingVectors[idx]),
			}
			records = append(records, record)
		}
	}
	if err := i.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("repository_id = ?", repo.ID).Delete(&model.CodeChunk{}).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		if err := tx.Create(&records).Error; err != nil {
			return err
		}
		return updateChunkSearchVectors(tx, repo.ID)
	}); err != nil {
		return IndexStats{}, err
	}
	return IndexStats{
		FileCount:       len(files),
		ChunkCount:      len(records),
		IndexDurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func updateChunkSearchVectors(tx *gorm.DB, repositoryID uint) error {
	return tx.Exec("UPDATE code_chunks SET search_vector = "+model.CodeChunkSearchVectorExpression+" WHERE repository_id = ?", repositoryID).Error
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
