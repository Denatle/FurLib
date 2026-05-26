package archivator

import (
	"time"

	"gorm.io/gorm"
)

type Post struct {
	gorm.Model
	PostID          string   `gorm:"uniqueIndex:idx_post_source;not null"`
	Source          string   `gorm:"uniqueIndex:idx_post_source;not null;index"`
	OriginalSources []string `gorm:"serializer:json"`
	Tags            []string `gorm:"serializer:json"`
	PostCreatedAt   time.Time
	DownloadedAt    time.Time
	Size            uint64
	Width           uint16
	Height          uint16
	Filetype        string
	Animated        bool
	Duration        int64
	Sound           bool
	SourceHash      string
	LocalHash       string
	FilePath        string
	Score           int
	Rating          string `gorm:"index"`
	Author          string `gorm:"index"`
}
