package dispatcher

import (
	"context"
	"sync"
	"time"
)

type EventType string

const (
	EventFound      EventType = "found"
	EventDownloaded EventType = "downloaded"
	EventFailed     EventType = "failed"
	EventDone       EventType = "done"
)

type Event struct {
	Type   EventType `json:"type"`
	Source string    `json:"source,omitempty"`
	PostID string    `json:"post_id,omitempty"`
	Count  int       `json:"count,omitempty"`
	Done   int       `json:"done,omitempty"`
	Failed int       `json:"failed,omitempty"`
	Error  string    `json:"error,omitempty"`
}

type JobStatus string

const (
	StatusPending   JobStatus = "pending"
	StatusRunning   JobStatus = "running"
	StatusDone      JobStatus = "done"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

type Options struct {
	Sources    []string            `json:"sources,omitempty"`
	Author     string              `json:"author,omitempty"`
	SourceTags map[string][]string `json:"source_tags,omitempty"`
	NewerThan  *time.Time          `json:"newer_than,omitempty"`
	OlderThan  *time.Time          `json:"older_than,omitempty"`
}

type Job struct {
	// immutable after creation
	ID         string
	Sources    []string
	Author     string
	Tags       []string
	SourceTags map[string][]string
	Limit      int
	NewerThan  *time.Time
	OlderThan  *time.Time
	CreatedAt  time.Time
	cancel     context.CancelFunc

	// mutable, protected by mu
	mu     sync.Mutex
	Status JobStatus
	Total  int
	Done   int
	Failed int
	Err    string
}

func (j *Job) setStatus(s JobStatus) {
	j.mu.Lock()
	j.Status = s
	j.mu.Unlock()
}

func (j *Job) incDone() {
	j.mu.Lock()
	j.Done++
	j.mu.Unlock()
}

func (j *Job) incFailed() {
	j.mu.Lock()
	j.Failed++
	j.mu.Unlock()
}

func (j *Job) addTotal(n int) {
	j.mu.Lock()
	j.Total += n
	j.mu.Unlock()
}

// JobSnapshot is a consistent read-only copy of a Job, safe for JSON serialization.
type JobSnapshot struct {
	ID         string              `json:"id"`
	Status     JobStatus           `json:"status"`
	Sources    []string            `json:"sources"`
	Author     string              `json:"author,omitempty"`
	Tags       []string            `json:"tags"`
	SourceTags map[string][]string `json:"source_tags,omitempty"`
	Limit      int                 `json:"limit"`
	NewerThan  *time.Time          `json:"newer_than,omitempty"`
	OlderThan  *time.Time          `json:"older_than,omitempty"`
	Total      int                 `json:"total"`
	Done       int                 `json:"done"`
	Failed     int                 `json:"failed"`
	Err        string              `json:"error,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
}

func (j *Job) Snapshot() JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobSnapshot{
		ID:         j.ID,
		Status:     j.Status,
		Sources:    j.Sources,
		Author:     j.Author,
		Tags:       j.Tags,
		SourceTags: j.SourceTags,
		Limit:      j.Limit,
		NewerThan:  j.NewerThan,
		OlderThan:  j.OlderThan,
		Total:      j.Total,
		Done:       j.Done,
		Failed:     j.Failed,
		Err:        j.Err,
		CreatedAt:  j.CreatedAt,
	}
}
