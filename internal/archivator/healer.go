package archivator

import (
	"FurLib/internal/fetcher"
	"context"
	"fmt"

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
	Total      int    `json:"total"`
	Healthy    int    `json:"healthy"`
	Missing    int    `json:"missing"`
	MissingIDs []uint `json:"missing_ids,omitempty"`
}

type HealReport struct {
	Missing int `json:"missing"`
	Healed  int `json:"healed"`
	Failed  int `json:"failed"`
}

func (h *Healer) Check() (HealthReport, error) {
	missing, err := h.repo.FindMissing()
	if err != nil {
		return HealthReport{}, fmt.Errorf("find missing: %w", err)
	}

	all, err := h.repo.FindAll()
	if err != nil {
		return HealthReport{}, fmt.Errorf("find all: %w", err)
	}

	ids := make([]uint, 0, len(missing))
	for _, p := range missing {
		ids = append(ids, p.ID)
	}

	return HealthReport{
		Total:      len(all),
		Healthy:    len(all) - len(missing),
		Missing:    len(missing),
		MissingIDs: ids,
	}, nil
}

func (h *Healer) Heal(ctx context.Context) (HealReport, error) {
	missing, err := h.repo.FindMissing()
	if err != nil {
		return HealReport{}, fmt.Errorf("find missing: %w", err)
	}

	report := HealReport{Missing: len(missing)}

	for _, post := range missing {
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

			// verify MD5 against the original hash from the source
			if meta.Hash != "" && localHash != meta.Hash {
				h.log.Warn("heal: md5 mismatch",
					zap.String("post_id", post.PostID),
					zap.String("expected", meta.Hash),
					zap.String("got", localHash),
				)
				report.Failed++
				continue
			}

			if err := h.repo.UpdateFileInfo(post.ID, result.Media.Path, localHash); err != nil {
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
