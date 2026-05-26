package archivator

import (
	"FurLib/internal/fetcher"
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
)

type Healer struct {
	repo    *Repository
	fetcher *fetcher.Fetcher
	log     *zap.Logger
}

func NewHealer(repo *Repository, f *fetcher.Fetcher, log *zap.Logger) *Healer {
	return &Healer{repo: repo, fetcher: f, log: log}
}

type HealthReport struct {
	Total        int    `json:"total"`
	Healthy      int    `json:"healthy"`
	Missing      int    `json:"missing"`
	MissingIDs   []uint `json:"missing_ids,omitempty"`
	Corrupted    int    `json:"corrupted"`
	CorruptedIDs []uint `json:"corrupted_ids,omitempty"`
}

type HealReport struct {
	Missing   int `json:"missing"`
	Corrupted int `json:"corrupted"`
	Healed    int `json:"healed"`
	Failed    int `json:"failed"`
}

func (h *Healer) Check() (HealthReport, error) {
	all, err := h.repo.FindAll()
	if err != nil {
		return HealthReport{}, fmt.Errorf("find all: %w", err)
	}

	var missingIDs, corruptedIDs []uint

	for _, p := range all {
		if _, err := os.Stat(p.FilePath); os.IsNotExist(err) {
			missingIDs = append(missingIDs, p.ID)
			continue
		}
		// Original download was bad (e.g. hotlink HTML page instead of image)
		if p.SourceHash != "" && p.LocalHash != p.SourceHash {
			corruptedIDs = append(corruptedIDs, p.ID)
			continue
		}
		// File modified after download
		if p.LocalHash != "" {
			hash, err := computeMD5(p.FilePath)
			if err != nil {
				h.log.Warn("check: md5 failed", zap.Uint("id", p.ID), zap.Error(err))
				continue
			}
			if hash != p.LocalHash {
				corruptedIDs = append(corruptedIDs, p.ID)
			}
		}
	}

	return HealthReport{
		Total:        len(all),
		Healthy:      len(all) - len(missingIDs) - len(corruptedIDs),
		Missing:      len(missingIDs),
		MissingIDs:   missingIDs,
		Corrupted:    len(corruptedIDs),
		CorruptedIDs: corruptedIDs,
	}, nil
}

func (h *Healer) Heal(ctx context.Context) (HealReport, error) {
	all, err := h.repo.FindAll()
	if err != nil {
		return HealReport{}, fmt.Errorf("find all: %w", err)
	}

	var toHeal []*Post
	var report HealReport

	for _, p := range all {
		if _, err := os.Stat(p.FilePath); os.IsNotExist(err) {
			report.Missing++
			toHeal = append(toHeal, p)
			continue
		}
		// Original download was bad (e.g. hotlink HTML page instead of image)
		if p.SourceHash != "" && p.LocalHash != p.SourceHash {
			h.log.Info("heal: bad original download detected",
				zap.Uint("id", p.ID),
				zap.String("post_id", p.PostID),
				zap.String("source_hash", p.SourceHash),
				zap.String("local_hash", p.LocalHash),
			)
			report.Corrupted++
			toHeal = append(toHeal, p)
			continue
		}
		// File modified after download
		if p.LocalHash != "" {
			hash, err := computeMD5(p.FilePath)
			if err != nil {
				h.log.Warn("heal: md5 check failed", zap.Uint("id", p.ID), zap.Error(err))
				continue
			}
			if hash != p.LocalHash {
				h.log.Info("heal: corrupted file detected",
					zap.Uint("id", p.ID),
					zap.String("post_id", p.PostID),
					zap.String("expected", p.LocalHash),
					zap.String("got", hash),
				)
				report.Corrupted++
				toHeal = append(toHeal, p)
			}
		}
	}

	for _, post := range toHeal {
		if ctx.Err() != nil {
			break
		}

		meta, err := h.fetcher.PostByID(post.Source, post.PostID)
		if err != nil {
			h.log.Warn("heal: fetch metadata failed",
				zap.Uint("id", post.ID),
				zap.String("post_id", post.PostID),
				zap.Error(err),
			)
			report.Failed++
			continue
		}

		results := h.fetcher.DownloadAll(ctx, []fetcher.MetaData{meta})
		for result := range results {
			if result.Err != nil {
				h.log.Warn("heal: download failed",
					zap.String("post_id", post.PostID),
					zap.Error(result.Err),
				)
				report.Failed++
				continue
			}

			localHash, err := computeMD5(result.Media.Path)
			if err != nil {
				h.log.Warn("heal: md5 failed", zap.String("post_id", post.PostID), zap.Error(err))
				report.Failed++
				continue
			}

			if meta.Hash != "" && localHash != meta.Hash {
				h.log.Warn("heal: md5 mismatch",
					zap.String("post_id", post.PostID),
					zap.String("expected", meta.Hash),
					zap.String("got", localHash),
				)
				report.Failed++
				continue
			}

			var fileSize uint64
			if info, err := os.Stat(result.Media.Path); err == nil {
				fileSize = uint64(info.Size())
			}

			if err := h.repo.UpdateFileInfo(post.ID, result.Media.Path, localHash, fileSize); err != nil {
				h.log.Warn("heal: db update failed", zap.String("post_id", post.PostID), zap.Error(err))
				report.Failed++
				continue
			}

			h.log.Info("heal: restored", zap.String("post_id", post.PostID), zap.String("path", result.Media.Path))
			report.Healed++
		}
	}

	return report, nil
}
