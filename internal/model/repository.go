package model

import "time"

type Repository struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	RepoURL         string     `gorm:"not null" json:"repo_url"`
	Owner           string     `gorm:"size:100;not null;index" json:"owner"`
	Name            string     `gorm:"size:150;not null;index" json:"name"`
	Status          string     `gorm:"size:32;not null;index" json:"status"`
	ErrorMessage    string     `gorm:"type:text" json:"error_message"`
	FileCount       int        `gorm:"not null;default:0" json:"file_count"`
	ChunkCount      int        `gorm:"not null;default:0" json:"chunk_count"`
	IndexDurationMS int64      `gorm:"not null;default:0" json:"index_duration_ms"`
	IndexedAt       *time.Time `json:"indexed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
