package dispatcher

import (
	"context"
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
	Sources   []string   `json:"sources,omitempty"`
	NewerThan *time.Time `json:"newer_than,omitempty"`
	OlderThan *time.Time `json:"older_than,omitempty"`
}

type Job struct {
	ID        string     `json:"id"`
	Status    JobStatus  `json:"status"`
	Sources   []string   `json:"sources"`
	Tags      []string   `json:"tags"`
	Limit     int        `json:"limit"`
	NewerThan *time.Time `json:"newer_than,omitempty"`
	OlderThan *time.Time `json:"older_than,omitempty"`
	Total     int        `json:"total"`
	Done      int        `json:"done"`
	Failed    int        `json:"failed"`
	Err       string     `json:"error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	cancel    context.CancelFunc
}
