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
		ID:        uuid.New().String(),
		Status:    StatusPending,
		Sources:   d.resolveSources(opts),
		Tags:      tags,
		Limit:     limit,
		NewerThan: opts.NewerThan,
		OlderThan: opts.OlderThan,
		CreatedAt: time.Now(),
		cancel:    cancel,
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

		metas, err := d.fetcher.SearchByTags(source, job.Tags, job.Limit)
		if err != nil {
			d.log.Warn("search failed", zap.String("job_id", job.ID), zap.String("source", source), zap.Error(err))
			continue
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
	events := make(chan Event, 32)

	go func() {
		defer close(events)

		var totalDone, totalFailed int

		for _, source := range d.resolveSources(opts) {
			if ctx.Err() != nil {
				break
			}

			metas, err := d.fetcher.SearchByTags(source, tags, limit)
			if err != nil {
				d.log.Warn("stream search failed", zap.String("source", source), zap.Error(err))
				continue
			}

			metas = filterByDate(metas, opts)

			events <- Event{Type: EventFound, Source: source, Count: len(metas)}

			for result := range d.fetcher.DownloadAll(ctx, metas) {
				if result.Err != nil {
					totalFailed++
					events <- Event{Type: EventFailed, Source: source, PostID: result.Media.Meta.ID, Error: result.Err.Error()}
					continue
				}

				totalDone++
				if err := d.archivator.Archive(result.Media); err != nil {
					d.log.Warn("archive failed", zap.String("post_id", result.Media.Meta.ID), zap.Error(err))
				}
				events <- Event{Type: EventDownloaded, Source: source, PostID: result.Media.Meta.ID}

			}
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
