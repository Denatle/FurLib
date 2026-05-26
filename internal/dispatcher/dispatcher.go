package dispatcher

import (
	"FurLib/internal/archivator"
	"FurLib/internal/fetcher"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Dispatcher struct {
	log        *zap.Logger
	fetcher    *fetcher.Fetcher
	archivator *archivator.Archivator
	jobs       sync.Map
}

func NewDispatcher(log *zap.Logger, f *fetcher.Fetcher, a *archivator.Archivator) *Dispatcher {
	return &Dispatcher{
		log:        log,
		fetcher:    f,
		archivator: a,
	}
}

func (d *Dispatcher) resolveSources(opts Options) []string {
	if len(opts.Sources) > 0 {
		return opts.Sources
	}
	return d.fetcher.Sources()
}

// buildSearchTags assembles the final tag list for a specific source:
// author (if set) + base tags + per-source extra tags.
// For tag-based sources (e621, gelbooru) author is just another tag.
// Future sources like kemono will handle it differently at the client level.
func buildSearchTags(source, author string, baseTags []string, sourceTags map[string][]string) []string {
	var tags []string
	if author != "" {
		tags = append(tags, author)
	}
	for _, t := range baseTags {
		if t != "" {
			tags = append(tags, t)
		}
	}
	tags = append(tags, sourceTags[source]...)
	return tags
}

func filterByDate(metas []fetcher.MetaData, opts Options) []fetcher.MetaData {
	if opts.NewerThan == nil && opts.OlderThan == nil {
		return metas
	}
	out := make([]fetcher.MetaData, 0, len(metas))
	for _, m := range metas {
		if opts.NewerThan != nil && !m.CreatedDate.After(*opts.NewerThan) {
			continue
		}
		if opts.OlderThan != nil && !m.CreatedDate.Before(*opts.OlderThan) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (d *Dispatcher) Submit(tags []string, limit int, opts Options) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())

	job := &Job{
		ID:         uuid.New().String(),
		Status:     StatusPending,
		Sources:    d.resolveSources(opts),
		Author:     opts.Author,
		Tags:       tags,
		SourceTags: opts.SourceTags,
		Limit:      limit,
		NewerThan:  opts.NewerThan,
		OlderThan:  opts.OlderThan,
		CreatedAt:  time.Now(),
		cancel:     cancel,
	}

	d.jobs.Store(job.ID, job)

	go d.run(ctx, job)

	return job.ID, nil
}

func (d *Dispatcher) run(ctx context.Context, job *Job) {
	defer job.cancel()

	job.setStatus(StatusRunning)

	opts := Options{NewerThan: job.NewerThan, OlderThan: job.OlderThan}

	for _, source := range job.Sources {
		if ctx.Err() != nil {
			break
		}

		metas, err := d.fetcher.SearchByTags(source, buildSearchTags(source, job.Author, job.Tags, job.SourceTags), job.Limit)
		if err != nil {
			d.log.Warn("search failed", zap.String("job_id", job.ID), zap.String("source", source), zap.Error(err))
			continue
		}

		if job.Author != "" {
			for i := range metas {
				if metas[i].Author == "" {
					metas[i].Author = job.Author
				}
			}
		}

		metas = filterByDate(metas, opts)
		job.addTotal(len(metas))

		for result := range d.fetcher.DownloadAll(ctx, metas) {
			if result.Err != nil {
				job.incFailed()
			} else {
				job.incDone()
			}

			if result.Err == nil {
				if err := d.archivator.Archive(result.Media); err != nil {
					d.log.Warn("archive failed", zap.String("post_id", result.Media.Meta.ID), zap.Error(err))
				}
			}
		}
	}

	if ctx.Err() != nil {
		job.setStatus(StatusCancelled)
	} else {
		job.setStatus(StatusDone)
	}
}

func (d *Dispatcher) Stream(ctx context.Context, tags []string, limit int, opts Options) <-chan Event {
	events := make(chan Event, 512)

	go func() {
		defer close(events)

		// Phase 1: search all sources to know total count before any download starts.
		var allMetas []fetcher.MetaData
		for _, source := range d.resolveSources(opts) {
			if ctx.Err() != nil {
				return
			}
			metas, err := d.fetcher.SearchByTags(source, buildSearchTags(source, opts.Author, tags, opts.SourceTags), limit)
			if err != nil {
				d.log.Warn("stream search failed", zap.String("source", source), zap.Error(err))
				continue
			}
			if opts.Author != "" {
				for i := range metas {
					if metas[i].Author == "" {
						metas[i].Author = opts.Author
					}
				}
			}
			allMetas = append(allMetas, filterByDate(metas, opts)...)
		}

		events <- Event{Type: EventFound, Count: len(allMetas)}

		// Phase 2: download all in parallel (limited by Workers semaphore).
		var totalDone, totalFailed int
		for result := range d.fetcher.DownloadAll(ctx, allMetas) {
			if result.Err != nil {
				totalFailed++
				events <- Event{Type: EventFailed, Source: result.Media.Meta.Source, PostID: result.Media.Meta.ID, Error: result.Err.Error()}
				continue
			}
			totalDone++
			if err := d.archivator.Archive(result.Media); err != nil {
				d.log.Warn("archive failed", zap.String("post_id", result.Media.Meta.ID), zap.Error(err))
			}
			events <- Event{Type: EventDownloaded, Source: result.Media.Meta.Source, PostID: result.Media.Meta.ID}
		}

		events <- Event{Type: EventDone, Done: totalDone, Failed: totalFailed}
	}()

	return events
}

func (d *Dispatcher) Search(tags []string, limit int) ([]fetcher.MetaData, error) {
	var all []fetcher.MetaData
	for _, source := range d.fetcher.Sources() {
		metas, err := d.fetcher.SearchByTags(source, tags, limit)
		if err != nil {
			d.log.Warn("search failed", zap.String("source", source), zap.Error(err))
			continue
		}
		all = append(all, metas...)
	}
	return all, nil
}

func (d *Dispatcher) GetJob(id string) (JobSnapshot, error) {
	v, ok := d.jobs.Load(id)
	if !ok {
		return JobSnapshot{}, fmt.Errorf("job not found: %s", id)
	}
	return v.(*Job).Snapshot(), nil
}

func (d *Dispatcher) ListJobs() []JobSnapshot {
	jobs := make([]JobSnapshot, 0)
	d.jobs.Range(func(_, v any) bool {
		jobs = append(jobs, v.(*Job).Snapshot())
		return true
	})
	return jobs
}

func (d *Dispatcher) CancelJob(id string) error {
	v, ok := d.jobs.Load(id)
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}
	v.(*Job).cancel()
	return nil
}
