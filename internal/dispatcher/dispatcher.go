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
	mu         sync.RWMutex
	jobs       map[string]*Job
}

func NewDispatcher(log *zap.Logger, f *fetcher.Fetcher, a *archivator.Archivator) *Dispatcher {
	return &Dispatcher{
		log:        log,
		fetcher:    f,
		archivator: a,
		jobs:       make(map[string]*Job),
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

	d.mu.Lock()
	d.jobs[job.ID] = job
	d.mu.Unlock()

	go d.run(ctx, job)

	return job.ID, nil
}

func (d *Dispatcher) run(ctx context.Context, job *Job) {
	defer job.cancel()

	d.mu.Lock()
	job.Status = StatusRunning
	d.mu.Unlock()

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

		d.mu.Lock()
		job.Total += len(metas)
		d.mu.Unlock()

		for result := range d.fetcher.DownloadAll(ctx, metas) {
			d.mu.Lock()
			if result.Err != nil {
				job.Failed++
			} else {
				job.Done++
			}
			d.mu.Unlock()

			if result.Err == nil {
				if err := d.archivator.Archive(result.Media); err != nil {
					d.log.Warn("archive failed", zap.String("post_id", result.Media.Meta.ID), zap.Error(err))
				}
			}
		}
	}

	d.mu.Lock()
	if ctx.Err() != nil {
		job.Status = StatusCancelled
	} else {
		job.Status = StatusDone
	}
	d.mu.Unlock()
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
				} else {
					totalDone++
					if err := d.archivator.Archive(result.Media); err != nil {
						d.log.Warn("archive failed", zap.String("post_id", result.Media.Meta.ID), zap.Error(err))
					}
					events <- Event{Type: EventDownloaded, Source: source, PostID: result.Media.Meta.ID}
				}
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

func (d *Dispatcher) GetJob(id string) (*Job, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	job, ok := d.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", id)
	}
	return job, nil
}

func (d *Dispatcher) ListJobs() []*Job {
	d.mu.RLock()
	defer d.mu.RUnlock()

	jobs := make([]*Job, 0, len(d.jobs))
	for _, j := range d.jobs {
		jobs = append(jobs, j)
	}
	return jobs
}

func (d *Dispatcher) CancelJob(id string) error {
	d.mu.RLock()
	job, ok := d.jobs[id]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	job.cancel()
	return nil
}
