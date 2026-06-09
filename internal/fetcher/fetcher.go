package fetcher

import (
	"FurLib/internal/config"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Params struct {
	fx.In

	Logger  *zap.Logger
	Cfg     config.FetcherConfig
	Clients []Client `group:"clients"`
}

type Fetcher struct {
	log     *zap.Logger
	cfg     config.FetcherConfig
	clients map[string]Client
	http    *http.Client
}

func NewFetcher(p Params) *Fetcher {
	m := make(map[string]Client, len(p.Clients))
	for _, c := range p.Clients {
		m[c.SourceName()] = c
	}
	return &Fetcher{
		log:     p.Logger,
		cfg:     p.Cfg,
		clients: m,
		http: &http.Client{
			Timeout: 2 * time.Minute,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return (&net.Dialer{
						Timeout:   30 * time.Second,
						KeepAlive: 30 * time.Second,
					}).DialContext(ctx, "tcp", addr)
				},
			},
		},
	}
}

func (f *Fetcher) Sources() []string {
	names := make([]string, 0, len(f.clients))
	for name := range f.clients {
		names = append(names, name)
	}
	return names
}

func (f *Fetcher) PostByID(source, id string) (MetaData, error) {
	client, ok := f.clients[source]
	if !ok {
		return MetaData{}, fmt.Errorf("unknown source: %s", source)
	}
	return client.PostByID(id)
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

func (f *Fetcher) DownloadAll(ctx context.Context, metas []MetaData) <-chan Result {
	results := make(chan Result, len(metas))
	sem := make(chan struct{}, f.cfg.Workers)

	go func() {
		var wg sync.WaitGroup
		for _, meta := range metas {
			if ctx.Err() != nil {
				break
			}

			wg.Add(1)
			go func(m MetaData) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					results <- Result{Err: ctx.Err(), Media: Media{Meta: m}}
					return
				}
				defer func() { <-sem }()

				path, err := f.download(ctx, m)
				if err != nil {
					f.log.Error("download failed",
						zap.String("id", m.ID),
						zap.String("source", m.Source),
						zap.Error(err),
					)
					results <- Result{Err: err, Media: Media{Meta: m}}
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

func (f *Fetcher) download(ctx context.Context, meta MetaData) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.Link, nil)
	if err != nil {
		return "", err
	}
	for k, v := range meta.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := f.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			f.log.Error("failed to close response body", zap.Error(err))
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	path := filepath.Join(f.cfg.DownloadDir, meta.Source, meta.ID+"."+meta.Filetype)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(file, resp.Body)
	if closeErr := file.Close(); closeErr != nil {
		f.log.Warn("failed to close file", zap.Error(closeErr))
	}
	if copyErr != nil {
		_ = os.Remove(path)
		return "", copyErr
	}
	return path, nil
}
