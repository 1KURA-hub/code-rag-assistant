package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"code-rag-assistant/internal/config"
	"code-rag-assistant/internal/model"
	"code-rag-assistant/internal/util"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type IngestService struct {
	db      *gorm.DB
	rdb     *redis.Client
	fetcher *GitHubFetcher
	indexer *CodeIndexer
	cfg     config.Config
}

const staleIndexTaskAfter = 10 * time.Minute

func NewIngestService(db *gorm.DB, rdb *redis.Client, fetcher *GitHubFetcher, indexer *CodeIndexer, cfg config.Config) *IngestService {
	return &IngestService{db: db, rdb: rdb, fetcher: fetcher, indexer: indexer, cfg: cfg}
}

func (s *IngestService) CreateAndIndex(ctx context.Context, repoURL string) (*model.Repository, error) {
	ref, err := s.fetcher.Parse(repoURL)
	if err != nil {
		return nil, err
	}

	var repo model.Repository
	err = s.db.WithContext(ctx).Where("owner = ? AND name = ?", ref.Owner, ref.Name).First(&repo).Error
	if err == nil {
		staleBefore := time.Now().Add(-staleIndexTaskAfter)
		result := s.db.WithContext(ctx).Model(&model.Repository{}).
			Where("id = ?", repo.ID).
			Where("status NOT IN ? OR updated_at < ?", []string{"pending", "indexing"}, staleBefore).
			Updates(map[string]interface{}{
				"repo_url":      repoURL,
				"status":        "pending",
				"error_message": "",
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			if err := s.db.WithContext(ctx).First(&repo, repo.ID).Error; err != nil {
				return nil, err
			}
			s.setRepoCache(ctx, &repo)
			return &repo, nil
		}
		if err := s.db.WithContext(ctx).First(&repo, repo.ID).Error; err != nil {
			return nil, err
		}
		s.deleteRepoCache(ctx, repo.ID)
		go s.index(context.Background(), repo.ID, ref)
		return &repo, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	repo = model.Repository{
		RepoURL: repoURL,
		Owner:   ref.Owner,
		Name:    ref.Name,
		Status:  "pending",
	}
	if err := s.db.WithContext(ctx).Create(&repo).Error; err != nil {
		return nil, err
	}
	s.deleteRepoCache(ctx, repo.ID)
	go s.index(context.Background(), repo.ID, ref)
	return &repo, nil
}

func (s *IngestService) Ensure(ctx context.Context, repoURL string) (*model.Repository, error) {
	ref, err := s.fetcher.Parse(repoURL)
	if err != nil {
		return nil, err
	}

	var repo model.Repository
	err = s.db.WithContext(ctx).Where("owner = ? AND name = ?", ref.Owner, ref.Name).First(&repo).Error
	if err == nil {
		s.setRepoCache(ctx, &repo)
		return &repo, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	repo = model.Repository{
		RepoURL: repoURL,
		Owner:   ref.Owner,
		Name:    ref.Name,
		Status:  "pending",
	}
	if err := s.db.WithContext(ctx).Create(&repo).Error; err != nil {
		return nil, err
	}
	s.deleteRepoCache(ctx, repo.ID)
	go s.index(context.Background(), repo.ID, ref)
	return &repo, nil
}

func (s *IngestService) Get(ctx context.Context, id uint) (*model.Repository, error) {
	if repo, ok := s.getRepoCache(ctx, id); ok {
		return repo, nil
	}

	var repo model.Repository
	if err := s.db.WithContext(ctx).First(&repo, id).Error; err != nil {
		return nil, err
	}
	s.setRepoCache(ctx, &repo)
	return &repo, nil
}

func (s *IngestService) index(ctx context.Context, repoID uint, ref GitHubRepoRef) {
	var repo model.Repository
	if err := s.db.WithContext(ctx).First(&repo, repoID).Error; err != nil {
		return
	}
	s.updateStatus(ctx, repoID, "indexing", "")
	workDir := filepath.Join(s.cfg.WorkDir, ref.Owner+"-"+ref.Name)
	zipPath, err := s.fetcher.DownloadZip(ctx, ref, workDir)
	if err != nil {
		s.markFailedOrKeepReady(ctx, repoID, err.Error())
		return
	}
	sourceDir := filepath.Join(workDir, "source")
	if err := util.Unzip(zipPath, sourceDir); err != nil {
		s.markFailedOrKeepReady(ctx, repoID, err.Error())
		return
	}
	stats, err := s.indexer.IndexRepository(ctx, &repo, sourceDir)
	if err != nil {
		s.markFailedOrKeepReady(ctx, repoID, err.Error())
		return
	}
	s.updateReady(ctx, repoID, stats)
}

func (s *IngestService) updateStatus(ctx context.Context, repoID uint, status, message string) {
	if err := s.db.WithContext(ctx).Model(&model.Repository{}).
		Where("id = ?", repoID).
		Updates(map[string]interface{}{"status": status, "error_message": message}).Error; err != nil {
		log.Printf("update repository status failed: %v", err)
		return
	}
	s.deleteRepoCache(ctx, repoID)
}

func (s *IngestService) markFailedOrKeepReady(ctx context.Context, repoID uint, message string) {
	var chunkCount int64
	_ = s.db.WithContext(ctx).Model(&model.CodeChunk{}).
		Where("repository_id = ?", repoID).
		Count(&chunkCount).Error
	if chunkCount == 0 {
		s.updateStatus(ctx, repoID, "failed", message)
		return
	}

	var fileCount int64
	_ = s.db.WithContext(ctx).Model(&model.CodeChunk{}).
		Where("repository_id = ?", repoID).
		Distinct("file_path").
		Count(&fileCount).Error
	if err := s.db.WithContext(ctx).Model(&model.Repository{}).
		Where("id = ?", repoID).
		Updates(map[string]interface{}{
			"status":        "ready",
			"error_message": "上次重新索引失败，继续使用已有索引：" + message,
			"file_count":    fileCount,
			"chunk_count":   chunkCount,
		}).Error; err != nil {
		log.Printf("keep repository ready after failed reindex failed: %v", err)
		return
	}
	s.deleteRepoCache(ctx, repoID)
}

func (s *IngestService) updateReady(ctx context.Context, repoID uint, stats IndexStats) {
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&model.Repository{}).
		Where("id = ?", repoID).
		Updates(map[string]interface{}{
			"status":            "ready",
			"error_message":     "",
			"file_count":        stats.FileCount,
			"chunk_count":       stats.ChunkCount,
			"index_duration_ms": stats.IndexDurationMS,
			"indexed_at":        &now,
		}).Error; err != nil {
		log.Printf("update repository ready status failed: %v", err)
		return
	}
	s.deleteRepoCache(ctx, repoID)
}

func repoCacheKey(id uint) string {
	return fmt.Sprintf("rag:repo:%d", id)
}

func (s *IngestService) getRepoCache(ctx context.Context, id uint) (*model.Repository, bool) {
	if s.rdb == nil || s.cfg.RepoCacheTTL <= 0 {
		return nil, false
	}

	data, err := s.rdb.Get(ctx, repoCacheKey(id)).Bytes()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		log.Printf("get repository cache failed: %v", err)
		return nil, false
	}

	var repo model.Repository
	if err := json.Unmarshal(data, &repo); err != nil {
		log.Printf("decode repository cache failed: %v", err)
		s.deleteRepoCache(ctx, id)
		return nil, false
	}
	return &repo, true
}

func (s *IngestService) setRepoCache(ctx context.Context, repo *model.Repository) {
	if s.rdb == nil || s.cfg.RepoCacheTTL <= 0 {
		return
	}

	data, err := json.Marshal(repo)
	if err != nil {
		log.Printf("encode repository cache failed: %v", err)
		return
	}
	if err := s.rdb.Set(ctx, repoCacheKey(repo.ID), data, s.cfg.RepoCacheTTL).Err(); err != nil {
		log.Printf("set repository cache failed: %v", err)
	}
}

func (s *IngestService) deleteRepoCache(ctx context.Context, id uint) {
	if s.rdb == nil {
		return
	}
	if err := s.rdb.Del(ctx, repoCacheKey(id)).Err(); err != nil {
		log.Printf("delete repository cache failed: %v", err)
	}
}
