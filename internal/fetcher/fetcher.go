package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/viper"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

func init() {
	viper.SetDefault("fetcher.workers", 5)
	viper.SetDefault("fetcher.download_dir", "/data/tmp")
}

type Params struct {
	fx.In

	Logger  *zap.Logger
	Clients []Client `group:"clients"`
}

type Fetcher struct {
	log     *zap.Logger
	clients map[string]Client
	http    *http.Client
}

func NewFetcher(p Params) *Fetcher {
	m := make(map[string]Client, len(p.Clients))
	for _, c := range p.Clients {
		m[c.SourceName()] = c
	}
	return &Fetcher{log: p.Logger, clients: m, http: &http.Client{}}
}

func (f *Fetcher) SearchByTags(source string, tags []string, limit int) ([]MetaData, error) {
	client, ok := f.clients[source]
	if !ok {
		return nil, fmt.Errorf("unknown source: %s", source)
	}

	metas, err := client.Search(tags, limit)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	return metas, nil
}

type Result struct {
	Media Media
	Err   error
}

// TODO:
func (f *Fetcher) downloadAll(ctx context.Context, metas []MetaData) <-chan Result {
	results := make(chan Result, len(metas))
	workers := viper.GetInt("fetcher.workers")
	sem := make(chan struct{}, workers)

	go func() {
		var wg sync.WaitGroup
		for _, meta := range metas {
			select {
			case <-ctx.Done():
				break
			default:
			}

			wg.Add(1)
			go func(m MetaData) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				path, err := f.download(ctx, m)
				if err != nil {
					f.log.Error("download failed",
						zap.String("id", m.ID),
						zap.String("source", m.Source),
						zap.Error(err),
					)
					results <- Result{Err: err}
					return
				}
				results <- Result{Media: Media{Path: path, Meta: m}}
			}(meta)
		}
		wg.Wait()
		close(results)
	}()

	return results
}

// TODO:
func (f *Fetcher) download(ctx context.Context, meta MetaData) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.Link, nil)
	if err != nil {
		return "", err
	}

	resp, err := f.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			f.log.Error("failed to close response body", zap.Error(err))
		}
	}(resp.Body)

	dir := viper.GetString("fetcher.download_dir")
	path := filepath.Join(dir, meta.Source, meta.ID+"."+meta.Filetype)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			f.log.Warn("failed to close file")
		}
	}(file)

	_, err = io.Copy(file, resp.Body)
	return path, err
}
