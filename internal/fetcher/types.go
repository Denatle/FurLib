package fetcher

import "time"

type MetaData struct {
	ID          string
	Source      string
	Tags        []string
	CreatedDate time.Time
	Size        uint64
	Width       uint16
	Height      uint16
	Filetype    string
	Animated    bool
	Duration    time.Duration
	Sound       bool
	Hash        string
	Link        string
}

type Media struct {
	Path string
	Meta MetaData
}

type Client interface {
	SourceName() string
	Search(tags []string, limit int) ([]MetaData, error)
	PostByID(id string) (MetaData, error)
}
