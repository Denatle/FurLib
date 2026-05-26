package archivator

import (
	"FurLib/internal/fetcher"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"go.uber.org/zap"
)

type Archivator struct {
	repo *Repository
	log  *zap.Logger
}

func NewArchivator(repo *Repository, log *zap.Logger) *Archivator {
	return &Archivator{repo: repo, log: log}
}

func (a *Archivator) Archive(media fetcher.Media) error {
	localHash, err := computeMD5(media.Path)
	if err != nil {
		return fmt.Errorf("md5 failed: %w", err)
	}

	size := media.Meta.Size
	if size == 0 {
		if info, err := os.Stat(media.Path); err == nil {
			size = uint64(info.Size())
		}
	}

	post := &Post{
		PostID:          media.Meta.ID,
		Source:          media.Meta.Source,
		OriginalSources: media.Meta.OriginalSources,
		Tags:            media.Meta.Tags,
		PostCreatedAt:   media.Meta.CreatedDate,
		DownloadedAt:    time.Now(),
		Size:            size,
		Width:           media.Meta.Width,
		Height:          media.Meta.Height,
		Filetype:        media.Meta.Filetype,
		Animated:        media.Meta.Animated,
		Duration:        int64(media.Meta.Duration),
		Sound:           media.Meta.Sound,
		SourceHash:      media.Meta.Hash,
		LocalHash:       localHash,
		FilePath:        media.Path,
		Score:           media.Meta.Score,
		Rating:          media.Meta.Rating,
	}

	if err := a.repo.Save(post); err != nil {
		return fmt.Errorf("save failed: %w", err)
	}

	a.log.Info("archived", zap.String("post_id", media.Meta.ID), zap.String("source", media.Meta.Source))
	return nil
}

// SoftDelete removes the file from disk and marks the post as deleted in the DB.
func (a *Archivator) SoftDelete(id uint) error {
	post, err := a.repo.SoftDelete(id)
	if err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}
	if post.FilePath != "" {
		if err := os.Remove(post.FilePath); err != nil && !os.IsNotExist(err) {
			a.log.Warn("delete file failed", zap.String("path", post.FilePath), zap.Error(err))
		}
	}
	a.log.Info("deleted post", zap.Uint("id", id), zap.String("path", post.FilePath))
	return nil
}

func computeMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
