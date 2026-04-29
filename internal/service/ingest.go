package service

import (
	"context"
	"path/filepath"
	"time"

	"code-rag-assistant/internal/config"
	"code-rag-assistant/internal/model"
	"code-rag-assistant/internal/util"

	"gorm.io/gorm"
)

type IngestService struct {
	db      *gorm.DB
	fetcher *GitHubFetcher
	indexer *CodeIndexer
	cfg     config.Config
}

func NewIngestService(db *gorm.DB, fetcher *GitHubFetcher, indexer *CodeIndexer, cfg config.Config) *IngestService {
	return &IngestService{db: db, fetcher: fetcher, indexer: indexer, cfg: cfg}
}

func (s *IngestService) CreateAndIndex(ctx context.Context, repoURL string) (*model.Repository, error) {
	ref, err := s.fetcher.Parse(repoURL)
	if err != nil {
		return nil, err
	}

	var repo model.Repository
	err = s.db.WithContext(ctx).Where("owner = ? AND name = ?", ref.Owner, ref.Name).First(&repo).Error
	if err == nil {
		if err := s.db.WithContext(ctx).Model(&repo).Updates(map[string]interface{}{
			"repo_url":      repoURL,
			"status":        "pending",
			"error_message": "",
		}).Error; err != nil {
			return nil, err
		}
		repo.RepoURL = repoURL
		repo.Status = "pending"
		repo.ErrorMessage = ""
		go s.index(context.Background(), repo.ID, ref)
		return &repo, nil
	}
	if err != gorm.ErrRecordNotFound {
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
	go s.index(context.Background(), repo.ID, ref)
	return &repo, nil
}

func (s *IngestService) Get(ctx context.Context, id uint) (*model.Repository, error) {
	var repo model.Repository
	if err := s.db.WithContext(ctx).First(&repo, id).Error; err != nil {
		return nil, err
	}
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
	_ = s.db.WithContext(ctx).Model(&model.Repository{}).
		Where("id = ?", repoID).
		Updates(map[string]interface{}{"status": status, "error_message": message}).Error
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
	_ = s.db.WithContext(ctx).Model(&model.Repository{}).
		Where("id = ?", repoID).
		Updates(map[string]interface{}{
			"status":        "ready",
			"error_message": "上次重新索引失败，继续使用已有索引：" + message,
			"file_count":    fileCount,
			"chunk_count":   chunkCount,
		}).Error
}

func (s *IngestService) updateReady(ctx context.Context, repoID uint, stats IndexStats) {
	now := time.Now()
	_ = s.db.WithContext(ctx).Model(&model.Repository{}).
		Where("id = ?", repoID).
		Updates(map[string]interface{}{
			"status":            "ready",
			"error_message":     "",
			"file_count":        stats.FileCount,
			"chunk_count":       stats.ChunkCount,
			"index_duration_ms": stats.IndexDurationMS,
			"indexed_at":        &now,
		}).Error
}
