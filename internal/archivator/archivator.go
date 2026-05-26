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

	post := &Post{
		PostID:          media.Meta.ID,
		Source:          media.Meta.Source,
		OriginalSources: media.Meta.OriginalSources,
		Tags:            media.Meta.Tags,
		PostCreatedAt:   media.Meta.CreatedDate,
		DownloadedAt:    time.Now(),
		Size:            media.Meta.Size,
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
	}

	if err := a.repo.Save(post); err != nil {
		return fmt.Errorf("save failed: %w", err)
	}

	a.log.Info("archived", zap.String("post_id", media.Meta.ID), zap.String("source", media.Meta.Source))
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
