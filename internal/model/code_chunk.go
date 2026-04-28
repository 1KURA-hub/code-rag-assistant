package model

import "time"

type CodeChunk struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	RepositoryID    uint       `gorm:"not null;index" json:"repository_id"`
	Repository      Repository `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	FilePath        string     `gorm:"type:text;not null" json:"file_path"`
	StartLine       int        `gorm:"not null" json:"start_line"`
	EndLine         int        `gorm:"not null" json:"end_line"`
	ChunkIndex      int        `gorm:"not null" json:"chunk_index"`
	Language        string     `gorm:"size:32;not null;default:''" json:"language"`
	SymbolName      string     `gorm:"type:text;not null;default:''" json:"symbol_name"`
	SymbolType      string     `gorm:"size:32;not null;default:''" json:"symbol_type"`
	Content         string     `gorm:"type:text;not null" json:"content"`
	EmbeddingVector string     `gorm:"type:vector(128)" json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
}
