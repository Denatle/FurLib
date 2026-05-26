package e621

import (
	"FurLib/internal/config"
	"FurLib/internal/fetcher"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const maxPageSize = 320

type Client struct {
	log      *zap.Logger
	e6Client *HTTPClient
}

func (c *Client) SourceName() string {
	return "e621"
}

func NewClient(log *zap.Logger, cfg config.E621Config) *Client {
	return &Client{log: log, e6Client: NewHTTPClient(Config{
		AppName:           "FurLib",
		AppVersion:        "0.0.1",
		Username:          cfg.Username,
		APIKey:            cfg.APIKey,
		RequestsPerSecond: cfg.RPS,
		Burst:             int(cfg.RPS),
		BaseURL:           cfg.BaseURL,
	})}
}

func (c *Client) Search(tags []string, limit int) ([]fetcher.MetaData, error) {
	var all []fetcher.MetaData

	for page := 1; len(all) < limit; page++ {
		pageSize := limit - len(all)
		if pageSize > maxPageSize {
			pageSize = maxPageSize
		}

		query := url.Values{}
		query.Add("tags", strings.Join(tags, " "))
		query.Add("limit", strconv.Itoa(pageSize))
		query.Add("page", strconv.Itoa(page))

		var resp PostsResponse
		if err := c.e6Client.Do(context.Background(), http.MethodGet, "/posts.json", query, nil, &resp); err != nil {
			return nil, err
		}

		posts := FlattenPosts(resp)
		all = append(all, posts...)

		if len(posts) < pageSize {
			break
		}
	}

	c.log.Info("search done", zap.Int("count", len(all)), zap.Strings("tags", tags))
	return all, nil
}

func (c *Client) PostByID(id string) (fetcher.MetaData, error) {
	panic("implement me")
}

func FlattenPosts(resp PostsResponse) []fetcher.MetaData {
	out := make([]fetcher.MetaData, 0, len(resp.Posts))

	for _, post := range resp.Posts {
		var tags []string
		tags = append(tags, post.Tags.General...)
		tags = append(tags, post.Tags.Artist...)
		tags = append(tags, post.Tags.Copyright...)
		tags = append(tags, post.Tags.Character...)
		tags = append(tags, post.Tags.Species...)
		tags = append(tags, post.Tags.Meta...)

		var duration time.Duration
		if post.Duration != nil {
			duration = 0
		}

		animated := false
		switch strings.ToLower(post.File.Ext) {
		case "gif", "webm", "mp4":
			animated = true
		}

		sound := false
		switch strings.ToLower(post.File.Ext) {
		case "webm", "mp4":
			sound = true
		}

		out = append(out, fetcher.MetaData{
			ID:              strconv.Itoa(post.ID),
			Source:          "e621",
			OriginalSources: post.Sources,
			Tags:            tags,
			CreatedDate:     post.CreatedAt,
			Size:            uint64(post.File.Size),
			Width:           uint16(post.File.Width),
			Height:          uint16(post.File.Height),
			Filetype:        post.File.Ext,
			Animated:        animated,
			Duration:        duration,
			Sound:           sound,
			Hash:            post.File.MD5,
			Link:            post.File.URL,
		})
	}

	return out
}
